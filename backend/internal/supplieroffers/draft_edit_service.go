package supplieroffers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// Draft edit commands (spec §7).
//
// Every command names exactly the fields a Supplier may supply. None of them
// carries ReviewRequired, ConfirmationRequired or a line subtotal: those are
// server-owned, so a generic PATCH shape can never be used to clear a review
// gate or post an arbitrary total. The authoritative amounts are calculated
// here from the issued RFQ quantity.

// QuoteDraftLineCommand prices one line.
type QuoteDraftLineCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
	DraftLineID      string
	// UnitPriceMinor is minor units; Money is never a float.
	UnitPriceMinor       int64
	Brand                string
	SKU                  string
	ProductDescription   string
	LeadTime             string
	SupplierLineNotes    string
	CommercialExceptions string
}

// DeclineDraftLineCommand records a no-bid or unavailable response.
type DeclineDraftLineCommand struct {
	Context           SupplierOfferMutationContextInput
	DraftID           string
	ExpectedRevision  int64
	DraftLineID       string
	ResponseStatus    OfferLineResponseStatus
	SupplierLineNotes string
}

// SupplierOfferDraftProjection adds only server-derived capabilities to the
// Supplier's own commercial draft. Submission errors remain private here: the
// boolean fails closed and the submit command still returns the precise,
// bounded validation response when the Supplier attempts the mutation.
type SupplierOfferDraftProjection struct {
	Draft     SupplierOfferDraft
	CanEdit   bool
	CanSubmit bool
}

// GetActiveDraft returns the Supplier's current unfinished draft.
//
// The Company comes from the authorized session, never from caller input, so a
// Supplier cannot read another tenant's offer by naming its identifiers. A
// missing draft is a tenant-safe not-found.
func (service *Service) GetActiveDraft(
	ctx context.Context,
	input SupplierOfferReadContextInput,
) (SupplierOfferDraft, error) {
	projection, err := service.GetActiveDraftProjection(ctx, input)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	return projection.Draft, nil
}

// GetActiveDraftProjection resolves authorization and the issued RFQ once,
// then uses the same calculation kernel as SubmitOffer to derive canSubmit.
// This prevents the read model from growing a second, drifting completeness
// predicate.
func (service *Service) GetActiveDraftProjection(
	ctx context.Context,
	input SupplierOfferReadContextInput,
) (SupplierOfferDraftProjection, error) {
	if service.chains == nil || service.drafts == nil {
		return SupplierOfferDraftProjection{}, ErrSupplierOffersNotConfigured
	}
	authorized, err := service.ResolveReadContext(ctx, input)
	if err != nil {
		return SupplierOfferDraftProjection{}, err
	}

	repository, err := service.replacementRepository()
	if err != nil {
		return SupplierOfferDraftProjection{}, err
	}
	draft, found, err := repository.FindUnfinishedDraftForInvitation(
		ctx,
		authorized.Access.CompanyID,
		authorized.Access.InvitationID,
		authorized.RFQ.ID,
	)
	if err != nil {
		return SupplierOfferDraftProjection{}, err
	}
	// A barrier-only row is replacement coordination, not an offer. Surfacing
	// it would show the Supplier a draft that does not exist.
	if !found || draft.IsRecipientReplacementBarrier() {
		return SupplierOfferDraftProjection{}, ErrOfferDraftNotFound
	}
	if draft.RecipientIdentity != authorized.Access.RecipientIdentity {
		return SupplierOfferDraftProjection{}, ErrOfferDraftNotFound
	}
	canEdit := draft.Status.AllowsMutation()
	_, calculationErr := CalculateSubmission(SubmissionCalculationInput{
		Draft: draft, RFQ: authorized.RFQ, SubmittedAt: input.AccessedAt,
	})
	return SupplierOfferDraftProjection{
		Draft: draft, CanEdit: canEdit,
		CanSubmit: canEdit && calculationErr == nil,
	}, nil
}

// QuoteDraftLine prices exactly one line.
//
// The subtotal is CALCULATED from the authoritative issued quantity, so a
// client cannot post a total that disagrees with quantity times unit price.
func (service *Service) QuoteDraftLine(
	ctx context.Context,
	command QuoteDraftLineCommand,
) (SupplierOfferDraft, error) {
	return service.editDraft(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft, rfq IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			index, line, err := findDraftLine(state.Lines, command.DraftLineID)
			if err != nil {
				return SupplierOfferDraftCommercialState{}, err
			}
			issued, err := findIssuedLine(rfq, line.RFQLineID)
			if err != nil {
				return SupplierOfferDraftCommercialState{}, err
			}

			calculated, err := CalculateQuotedLine(draft.Currency, QuotedLineInput{
				RFQLineID:             line.RFQLineID,
				AuthoritativeQuantity: issued.Quantity,
				// Phase 1 quotes the full issued quantity; the Supplier does
				// not choose a partial quantity.
				QuotedQuantity: issued.Quantity,
				UnitPrice:      money.New(command.UnitPriceMinor, draft.Currency),
			})
			if err != nil {
				return SupplierOfferDraftCommercialState{}, err
			}

			quotedQuantity := calculated.QuotedQuantity
			unitPrice := calculated.UnitPrice
			subtotal := calculated.LineSubtotal

			line.ResponseStatus = OfferLineQuoted
			line.QuotedQuantity = &quotedQuantity
			line.UnitPriceExcludingTax = &unitPrice
			line.LineSubtotalExcludingTax = &subtotal
			line.Brand = strings.TrimSpace(command.Brand)
			line.SKU = strings.TrimSpace(command.SKU)
			line.ProductDescription = strings.TrimSpace(command.ProductDescription)
			line.LeadTime = strings.TrimSpace(command.LeadTime)
			line.SupplierLineNotes = strings.TrimSpace(command.SupplierLineNotes)
			line.CommercialExceptions = strings.TrimSpace(command.CommercialExceptions)
			// Explicitly re-pricing a copied line resolves its review gate: the
			// Supplier has now stated this price for THIS RFQ version.
			line.ReviewRequired = false

			state.Lines[index] = line
			return state, nil
		})
}

// DeclineDraftLine records a no-bid or unavailable response.
//
// Commercial values are cleared rather than left in place, so a stale price can
// never be read later as an active quote.
func (service *Service) DeclineDraftLine(
	ctx context.Context,
	command DeclineDraftLineCommand,
) (SupplierOfferDraft, error) {
	if command.ResponseStatus != OfferLineNoBid &&
		command.ResponseStatus != OfferLineUnavailable {
		return SupplierOfferDraft{}, ErrInvalidQuotedLine
	}

	return service.editDraft(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			index, line, err := findDraftLine(state.Lines, command.DraftLineID)
			if err != nil {
				return SupplierOfferDraftCommercialState{}, err
			}

			line.ResponseStatus = command.ResponseStatus
			line.QuotedQuantity = nil
			line.UnitPriceExcludingTax = nil
			line.LineSubtotalExcludingTax = nil
			line.LineTax = nil
			line.SupplierLineNotes = strings.TrimSpace(command.SupplierLineNotes)
			// An explicit decline answers the copied-decline confirmation gate.
			line.ConfirmationRequired = false
			line.ReviewRequired = false

			state.Lines[index] = line
			return state, nil
		})
}

// ResetDraftLineResponseCommand clears one line's response back to
// unanswered. Reset is NOT deletion (M8.1, checkpoint 4): the issued line,
// its lineage and its position in the draft are preserved untouched.
type ResetDraftLineResponseCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
	DraftLineID      string
}

// ResetDraftLineResponse clears a quoted or declined response to unanswered,
// blocking submission until the Supplier answers again. Resetting an
// already-unanswered line converges without another revision.
func (service *Service) ResetDraftLineResponse(
	ctx context.Context,
	command ResetDraftLineResponseCommand,
) (SupplierOfferDraft, error) {
	lineAlreadyUnanswered := func(draft SupplierOfferDraft) bool {
		_, line, err := findDraftLine(draft.Lines, command.DraftLineID)
		return err == nil && line.ResponseStatus == OfferLineUnanswered
	}
	return service.editDraftConverging(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		lineAlreadyUnanswered,
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			index, line, err := findDraftLine(state.Lines, command.DraftLineID)
			if err != nil {
				return SupplierOfferDraftCommercialState{}, err
			}

			line.ResponseStatus = OfferLineUnanswered
			line.QuotedQuantity = nil
			line.UnitPriceExcludingTax = nil
			line.LineSubtotalExcludingTax = nil
			line.LineTax = nil
			line.Brand = ""
			line.SKU = ""
			line.ProductDescription = ""
			line.LeadTime = ""
			line.SupplierLineNotes = ""
			line.CommercialExceptions = ""
			line.ReviewRequired = false
			line.ConfirmationRequired = false

			state.Lines[index] = line
			return state, nil
		})
}

// Offer-level acknowledgement and removal commands.
//
// Each names exactly one gate. A generic PATCH that could clear several at once
// would let a Supplier submit copied terms they never actually reviewed, which
// is the whole reason these gates are server-owned.

// AcknowledgeOfferTaxCommand confirms copied offer-level tax.
type AcknowledgeOfferTaxCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
}

// AcknowledgeDeliveryChargeCommand confirms a copied delivery charge.
type AcknowledgeDeliveryChargeCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
}

// AcknowledgeChargeGroupCommand confirms ONE copied conditional charge group.
type AcknowledgeChargeGroupCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
	ChargeGroupID    string
}

// RemoveDeliveryChargeCommand drops the delivery charge entirely.
type RemoveDeliveryChargeCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
}

// RemoveChargeGroupCommand drops ONE conditional charge group entirely.
type RemoveChargeGroupCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
	ChargeGroupID    string
}

// AcknowledgeOfferTax clears only the offer-tax review gate, leaving the copied
// tax values themselves untouched: the Supplier is confirming them, not
// restating them. Repeating the call once already acknowledged converges
// without another revision (M8.1 checkpoint 5).
func (service *Service) AcknowledgeOfferTax(
	ctx context.Context,
	command AcknowledgeOfferTaxCommand,
) (SupplierOfferDraft, error) {
	return service.editDraftConverging(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft) bool { return !draft.OfferTaxReviewRequired },
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			state.OfferTaxReviewRequired = false
			return state, nil
		})
}

// AcknowledgeDeliveryCharge clears only the delivery review gate. Repeating
// the call once already acknowledged converges without another revision.
func (service *Service) AcknowledgeDeliveryCharge(
	ctx context.Context,
	command AcknowledgeDeliveryChargeCommand,
) (SupplierOfferDraft, error) {
	return service.editDraftConverging(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft) bool { return !draft.DeliveryChargeReviewRequired },
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			state.DeliveryChargeReviewRequired = false
			return state, nil
		})
}

// AcknowledgeChargeGroup clears the gate on exactly the named group. A group
// that does not exist at all is a not-found rather than a silent success, so
// a client cannot believe it resolved a gate that is still blocking
// submission; a group that exists and is ALREADY acknowledged converges.
func (service *Service) AcknowledgeChargeGroup(
	ctx context.Context,
	command AcknowledgeChargeGroupCommand,
) (SupplierOfferDraft, error) {
	findGroup := func(draft SupplierOfferDraft) (SupplierChargeGroupDraft, bool) {
		for _, group := range draft.ChargeGroups {
			if group.ID == command.ChargeGroupID {
				return group, true
			}
		}
		return SupplierChargeGroupDraft{}, false
	}
	return service.editDraftConverging(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft) bool {
			group, found := findGroup(draft)
			return found && !group.ReviewRequired
		},
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			if _, found := findGroup(draft); !found {
				return SupplierOfferDraftCommercialState{}, ErrOfferDraftNotFound
			}
			state := commercialStateOf(draft)
			for index, group := range state.ChargeGroups {
				if group.ID == command.ChargeGroupID {
					state.ChargeGroups[index].ReviewRequired = false
					return state, nil
				}
			}
			return SupplierOfferDraftCommercialState{}, ErrOfferDraftNotFound
		})
}

// RemoveDeliveryCharge drops the charge and its gate together. A gate guarding
// something that no longer exists would block submission forever. An already
// absent delivery charge IS the successful postcondition, so repeating the
// call converges without another revision.
func (service *Service) RemoveDeliveryCharge(
	ctx context.Context,
	command RemoveDeliveryChargeCommand,
) (SupplierOfferDraft, error) {
	return service.editDraftConverging(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft) bool {
			return draft.DeliveryCharge == nil && !draft.DeliveryChargeReviewRequired
		},
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			state.DeliveryCharge = nil
			state.DeliveryChargeReviewRequired = false
			return state, nil
		})
}

// RemoveChargeGroup drops the whole named group, gate included. An already
// absent group IS the successful postcondition, so repeating the call
// converges without another revision — the standard idempotent-delete shape,
// even though the public route is an explicit /remove action rather than a
// generic DELETE.
func (service *Service) RemoveChargeGroup(
	ctx context.Context,
	command RemoveChargeGroupCommand,
) (SupplierOfferDraft, error) {
	groupPresent := func(draft SupplierOfferDraft) bool {
		for _, group := range draft.ChargeGroups {
			if group.ID == command.ChargeGroupID {
				return true
			}
		}
		return false
	}
	return service.editDraftConverging(ctx, command.Context, command.DraftID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft) bool { return !groupPresent(draft) },
		func(draft SupplierOfferDraft, _ IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			state := commercialStateOf(draft)
			remaining := make([]SupplierChargeGroupDraft, 0, len(state.ChargeGroups))
			for _, group := range state.ChargeGroups {
				if group.ID == command.ChargeGroupID {
					continue
				}
				remaining = append(remaining, group)
			}
			state.ChargeGroups = remaining
			return state, nil
		})
}

// editDraft is the shared authorize -> load -> mutate -> CAS path.
//
// Every edit goes through the active-only, expected-revision CAS, so a
// concurrent submission claim or recipient-replacement claim always wins
// against a stale edit rather than being overwritten.
//
// draftID is optional: when empty (the M8.1 routes, which never accept a
// caller-supplied draft ID), the caller's own active draft is resolved here
// from the SAME authorized mutation context rather than requiring a separate
// prior read-context round trip. A separate read call would authorize the
// session twice per request, doubling contention on the sliding-session
// renewal CAS under concurrent load.
func (service *Service) editDraft(
	ctx context.Context,
	input SupplierOfferMutationContextInput,
	draftID string,
	expectedRevision int64,
	mutate func(SupplierOfferDraft, IssuedRFQSnapshot) (
		SupplierOfferDraftCommercialState, error),
) (SupplierOfferDraft, error) {
	if service.chains == nil || service.drafts == nil {
		return SupplierOfferDraft{}, ErrSupplierOffersNotConfigured
	}
	authorized, err := service.ResolveMutationContext(ctx, input)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	repository, err := service.editRepository()
	if err != nil {
		return SupplierOfferDraft{}, err
	}

	draft, err := resolveEditTargetDraft(ctx, repository, authorized, draftID)
	if err != nil {
		return SupplierOfferDraft{}, err
	}

	state, err := mutate(draft, authorized.RFQ)
	if err != nil {
		return SupplierOfferDraft{}, err
	}

	return repository.ReplaceActiveCommercialState(ctx, authorized.Access.CompanyID,
		draft.ID, expectedRevision, state, input.AccessedAt)
}

// resolveEditTargetDraft loads the draft an edit applies to. A caller-
// supplied draftID is looked up directly; an empty draftID resolves the
// caller's own active draft for the authorized invitation/RFQ instead.
func resolveEditTargetDraft(
	ctx context.Context,
	repository draftEditRepository,
	authorized AuthorizedSupplierOfferContext,
	draftID string,
) (SupplierOfferDraft, error) {
	if draftID == "" {
		activeRepository, ok := repository.(activeDraftRepository)
		if !ok {
			return SupplierOfferDraft{}, ErrSupplierOffersNotConfigured
		}
		draft, found, err := activeRepository.FindUnfinishedDraftForInvitation(
			ctx, authorized.Access.CompanyID, authorized.Access.InvitationID, authorized.RFQ.ID)
		if err != nil {
			return SupplierOfferDraft{}, err
		}
		if !found || draft.IsRecipientReplacementBarrier() ||
			draft.RecipientIdentity != authorized.Access.RecipientIdentity ||
			!draft.Status.AllowsMutation() {
			return SupplierOfferDraft{}, ErrOfferDraftNotFound
		}
		return draft, nil
	}

	draft, found, err := repository.FindDraft(ctx, authorized.Access.CompanyID, draftID)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	// Missing, foreign and other-recipient drafts share one not-found so a
	// Supplier cannot probe for another tenant's draft identifiers.
	if !found ||
		draft.InvitationID != authorized.Access.InvitationID ||
		draft.RecipientIdentity != authorized.Access.RecipientIdentity {
		return SupplierOfferDraft{}, ErrOfferDraftNotFound
	}
	if draft.IssuedRFQVersionID != authorized.RFQ.ID {
		return SupplierOfferDraft{}, ErrOfferDraftConflict
	}
	return draft, nil
}

// activeDraftRepository is the narrow read resolveEditTargetDraft needs to
// find the caller's own active draft without a caller-supplied ID.
type activeDraftRepository interface {
	FindUnfinishedDraftForInvitation(ctx context.Context, companyID, invitationID,
		issuedRFQVersionID string) (SupplierOfferDraft, bool, error)
}

// editDraftConverging wraps editDraft with state-based convergence (M8.1
// checkpoint 5): before attempting any mutation, it checks whether the
// requested postcondition ALREADY holds on the current draft, and if so
// returns that draft unchanged rather than requiring the caller to know a
// newer revision or incrementing it again. If a concurrent operation wins the
// CAS first, it reloads and re-checks the postcondition: satisfied means
// convergence, still unsatisfied means a genuine conflict between two
// different effective mutations.
//
// This is deliberately not operation-ID based: each of these seven
// operations has one deterministic postcondition, so re-deriving "did this
// already happen" from current state is sufficient and needs no separate
// idempotency-key storage.
func (service *Service) editDraftConverging(
	ctx context.Context,
	input SupplierOfferMutationContextInput,
	draftID string,
	expectedRevision int64,
	satisfied func(SupplierOfferDraft) bool,
	mutate func(SupplierOfferDraft, IssuedRFQSnapshot) (
		SupplierOfferDraftCommercialState, error),
) (SupplierOfferDraft, error) {
	if service.chains == nil || service.drafts == nil {
		return SupplierOfferDraft{}, ErrSupplierOffersNotConfigured
	}
	authorized, err := service.ResolveMutationContext(ctx, input)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	repository, err := service.editRepository()
	if err != nil {
		return SupplierOfferDraft{}, err
	}

	draft, err := resolveEditTargetDraft(ctx, repository, authorized, draftID)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	targetDraftID := draft.ID
	if satisfied(draft) {
		return draft, nil
	}

	state, err := mutate(draft, authorized.RFQ)
	if err != nil {
		return SupplierOfferDraft{}, err
	}

	updated, err := repository.ReplaceActiveCommercialState(ctx, authorized.Access.CompanyID,
		targetDraftID, expectedRevision, state, input.AccessedAt)
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, ErrOfferDraftConflict) {
		return SupplierOfferDraft{}, err
	}

	// The CAS lost. Reload and classify: a concurrent equivalent mutation may
	// have already satisfied the postcondition, in which case this call
	// converges instead of reporting a conflict for work that actually
	// succeeded elsewhere.
	reloaded, reloadFound, reloadErr := repository.FindDraft(ctx, authorized.Access.CompanyID, targetDraftID)
	if reloadErr != nil {
		return SupplierOfferDraft{}, reloadErr
	}
	if reloadFound && satisfied(reloaded) {
		return reloaded, nil
	}
	return SupplierOfferDraft{}, ErrOfferDraftConflict
}

// draftEditRepository is the narrow persistence surface edits require.
type draftEditRepository interface {
	FindDraft(ctx context.Context, companyID, draftID string) (
		SupplierOfferDraft, bool, error)
	ReplaceActiveCommercialState(
		ctx context.Context,
		companyID string,
		draftID string,
		expectedRevision int64,
		state SupplierOfferDraftCommercialState,
		updatedAt time.Time,
	) (SupplierOfferDraft, error)
}

func (service *Service) editRepository() (draftEditRepository, error) {
	repository, ok := service.drafts.(draftEditRepository)
	if !ok {
		return nil, ErrSupplierOffersNotConfigured
	}
	return repository, nil
}

// commercialStateOf copies the mutable commercial projection out of a draft.
// Server-owned flags travel with the state they protect rather than being
// rebuilt from client input.
func commercialStateOf(draft SupplierOfferDraft) SupplierOfferDraftCommercialState {
	lines := make([]SupplierOfferDraftLine, len(draft.Lines))
	copy(lines, draft.Lines)
	groups := make([]SupplierChargeGroupDraft, len(draft.ChargeGroups))
	copy(groups, draft.ChargeGroups)

	return SupplierOfferDraftCommercialState{
		Lines:                        lines,
		Tax:                          draft.Tax,
		OfferTaxReviewRequired:       draft.OfferTaxReviewRequired,
		ChargeGroups:                 groups,
		DeliveryCharge:               draft.DeliveryCharge,
		DeliveryChargeReviewRequired: draft.DeliveryChargeReviewRequired,
		OfferValidUntil:              draft.OfferValidUntil,
		SupplierNotes:                draft.SupplierNotes,
		SourceOfferVersionID:         draft.SourceOfferVersionID,
	}
}

func findDraftLine(lines []SupplierOfferDraftLine, draftLineID string) (
	int, SupplierOfferDraftLine, error) {

	for index, line := range lines {
		if line.ID == draftLineID {
			return index, line, nil
		}
	}
	return 0, SupplierOfferDraftLine{}, ErrOfferDraftNotFound
}

func findIssuedLine(rfq IssuedRFQSnapshot, rfqLineID string) (
	IssuedRFQLineSnapshot, error) {

	for _, line := range rfq.Lines {
		if line.ID == rfqLineID {
			return line, nil
		}
	}
	// The draft line no longer exists in the authorized issued version.
	return IssuedRFQLineSnapshot{}, ErrOfferDraftConflict
}
