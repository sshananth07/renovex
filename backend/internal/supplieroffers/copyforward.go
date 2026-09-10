package supplieroffers

import (
	"context"
	"errors"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Copy-forward (spec §7, "Copy-forward source, matching, and provenance").
//
// Copying reads only an IMMUTABLE submitted Offer Version, never another
// recipient's mutable draft. The target issued RFQ always supplies the
// authoritative inputs — quantity, unit, material identity, specification —
// and copying never overwrites them. Previously calculated subtotals, tax
// amounts and totals are never carried across; they are recalculated from the
// target quantity and the copied unit price, so a stale total can never
// contradict the new requirements.

// CopyOmissionReason is the bounded, user-facing feedback vocabulary. These are
// copy feedback, not submission errors.
type CopyOmissionReason string

const (
	CopyOmissionMissingLine      CopyOmissionReason = "missing_line"
	CopyOmissionIncompatibleLine CopyOmissionReason = "incompatible_line"
	CopyOmissionNonQuotedLine    CopyOmissionReason = "non_quoted_line"
	CopyOmissionAmbiguousMapping CopyOmissionReason = "ambiguous_mapping"
	CopyOmissionOverlappingGroup CopyOmissionReason = "overlapping_group"
)

// OmittedChargeGroup reports one whole group that could not be copied.
type OmittedChargeGroup struct {
	SourceChargeGroupID   string
	SourceChargeGroupName string
	Reason                CopyOmissionReason
}

// CopyForwardSourceMapping carries the lineage facts needed to match source
// lines to target lines. M7-backed lines match on Material Requirement;
// M8-native lines match on lineage.
type CopyForwardSourceMapping struct {
	LineMaterialRequirementIDs map[string]string
	LineLineageIDs             map[string]string
}

// CopyForwardInput is the complete, explicit input to a copy.
type CopyForwardInput struct {
	Source    SupplierOfferVersion
	TargetRFQ IssuedRFQSnapshot
	Mapping   CopyForwardSourceMapping
	CopiedAt  time.Time
	// TargetLines are the blank draft lines already created for the target
	// issued version; copying fills them in rather than inventing identity.
	TargetLines []SupplierOfferDraftLine
	// SourceRFQLines lets the copy detect changed authoritative inputs. When a
	// source line is absent here, the copy cannot prove nothing changed and
	// gates the line for review.
	SourceRFQLines map[string]IssuedRFQLineSnapshot
}

// CopyForwardResult is the commercial state a copy produces, plus bounded
// feedback about whole groups that could not come across.
type CopyForwardResult struct {
	SupplierOfferDraftCommercialState
	OmittedGroups []OmittedChargeGroup
}

// CopyForward builds the target draft's commercial state from an immutable
// source version.
func CopyForward(input CopyForwardInput) (CopyForwardResult, error) {
	sourceByRFQLine := make(map[string]SupplierOfferLine, len(input.Source.Lines))
	for _, line := range input.Source.Lines {
		sourceByRFQLine[line.RFQLineID] = line
	}
	targetIssuedByID := make(map[string]IssuedRFQLineSnapshot, len(input.TargetRFQ.Lines))
	for _, line := range input.TargetRFQ.Lines {
		targetIssuedByID[line.ID] = line
	}

	// Match target lines back to source lines through the lineage facts.
	sourceRFQLineForTarget := matchSourceLines(input, targetIssuedByID)

	lines := make([]SupplierOfferDraftLine, 0, len(input.TargetLines))
	copiedTargetRFQLines := make(map[string]bool, len(input.TargetLines))

	for _, targetLine := range input.TargetLines {
		issued, hasIssued := targetIssuedByID[targetLine.RFQLineID]
		sourceRFQLineID, matched := sourceRFQLineForTarget[targetLine.RFQLineID]
		if !hasIssued || !matched {
			// A brand-new target line has no counterpart: the Supplier must
			// still answer it, so it is explicitly unanswered rather than
			// vanishing or inheriting an unset status.
			targetLine.ResponseStatus = OfferLineUnanswered
			lines = append(lines, targetLine)
			continue
		}
		source := sourceByRFQLine[sourceRFQLineID]

		copied, quoted := copyLine(targetLine, source, issued,
			input.SourceRFQLines[sourceRFQLineID], input.Source.ID, input.CopiedAt)
		if quoted {
			copiedTargetRFQLines[targetLine.RFQLineID] = true
		}
		lines = append(lines, copied)
	}

	state := SupplierOfferDraftCommercialState{
		Lines: lines,
		// SupplierNotes copies as plain editable text and needs no gate. It
		// carries no source-recipient ownership metadata.
		SupplierNotes: input.Source.SupplierNotes,
		// OfferValidUntil deliberately never copies: submission requires a
		// newly supplied date later than submission time, so a stale validity
		// can never make an expired offer look current.
		OfferValidUntil: nil,
	}

	state.Tax, state.OfferTaxReviewRequired = copyTax(input.Source.Tax)

	if input.Source.DeliveryCharge != nil {
		charge := *input.Source.DeliveryCharge
		state.DeliveryCharge = &charge
		// The charge was quoted for a different RFQ version, so it always
		// requires an explicit confirmation before submission.
		state.DeliveryChargeReviewRequired = true
	}

	groups, omitted := copyChargeGroups(input, sourceRFQLineForTarget,
		copiedTargetRFQLines)
	state.ChargeGroups = groups

	return CopyForwardResult{
		SupplierOfferDraftCommercialState: state,
		OmittedGroups:                     omitted,
	}, nil
}

// CopyForwardCommand copies the automatically-resolved prior submission into
// the caller's own commercially empty active draft (M8.1 amendment, §7.1).
// The caller never names a source: source selection is entirely server-owned,
// so a Supplier can never seed a draft from an offer they should not see.
type CopyForwardCommand struct {
	Context          SupplierOfferMutationContextInput
	DraftID          string
	ExpectedRevision int64
}

// CopyForwardOutcome returns the updated draft plus bounded copy feedback.
type CopyForwardOutcome struct {
	Draft         SupplierOfferDraft
	OmittedGroups []OmittedChargeGroup
}

// CopyForwardIntoDraft resolves the source automatically and applies the copy
// through the same revision-guarded, active-only CAS as every other edit
// (M8.1 amendment, §7.1).
//
// Convergence is state-based, not operation-ID based: if the draft already
// records a SourceOfferVersionID, copy-forward already completed, and this
// call returns the current draft unchanged rather than resolving again and
// possibly pinning a different (newer) source. A non-empty draft without a
// recorded source is a genuine conflict — copy-forward never merges into or
// overwrites Supplier-entered content.
func (service *Service) CopyForwardIntoDraft(
	ctx context.Context,
	command CopyForwardCommand,
) (CopyForwardOutcome, error) {
	if service.versions == nil {
		return CopyForwardOutcome{}, ErrSupplierOffersNotConfigured
	}

	repository, err := service.editRepository()
	if err != nil {
		return CopyForwardOutcome{}, err
	}
	authorized, err := service.ResolveMutationContext(ctx, command.Context)
	if err != nil {
		return CopyForwardOutcome{}, err
	}
	current, err := resolveEditTargetDraft(ctx, repository, authorized, command.DraftID)
	if err != nil {
		return CopyForwardOutcome{}, err
	}
	// The postcondition ("this draft has a resolved copy source") already
	// holds: converge on the existing draft without resolving or copying
	// again, regardless of what a fresh resolution might now produce.
	if current.SourceOfferVersionID != nil {
		return CopyForwardOutcome{Draft: current}, nil
	}
	if !current.IsCommerciallyEmpty() {
		return CopyForwardOutcome{}, ErrOfferDraftNotEmpty
	}

	source, sourceFound, err := service.resolveLatestEligibleSource(
		ctx, authorized.Access.CompanyID, authorized.Access.InvitationID, authorized.RFQ.ID)
	if err != nil {
		return CopyForwardOutcome{}, err
	}
	if !sourceFound {
		return CopyForwardOutcome{}, ErrCopySourceNotFound
	}

	// The fingerprint gate needs the EXACT RFQ line snapshots the source was
	// quoted against, not just the immutable version's own quoted values.
	sourceRFQ, sourceRFQFound, err := service.issuedRFQ.GetIssuedRFQForOffer(
		ctx, authorized.Access.CompanyID, source.IssuedRFQVersionID)
	if err != nil {
		return CopyForwardOutcome{}, err
	}
	if !sourceRFQFound {
		return CopyForwardOutcome{}, ErrIssuedRFQNotFound
	}
	sourceRFQLines := make(map[string]IssuedRFQLineSnapshot, len(sourceRFQ.Lines))
	for _, line := range sourceRFQ.Lines {
		sourceRFQLines[line.ID] = line
	}

	var omitted []OmittedChargeGroup
	draft, err := service.editDraft(ctx, command.Context, current.ID,
		command.ExpectedRevision,
		func(draft SupplierOfferDraft, rfq IssuedRFQSnapshot) (
			SupplierOfferDraftCommercialState, error) {

			result, copyErr := CopyForward(CopyForwardInput{
				Source:    source,
				TargetRFQ: rfq,
				// The mapping must carry the SOURCE's own lineage/Material
				// Requirement facts — matchSourceLines resolves target line ID
				// -> source line ID through it, so building it from the target
				// RFQ instead would make every "source" ID actually be a
				// target ID and silently break every lookup keyed by it.
				Mapping:        mappingFromIssuedLines(sourceRFQ),
				CopiedAt:       command.Context.AccessedAt,
				TargetLines:    draft.Lines,
				SourceRFQLines: sourceRFQLines,
			})
			if copyErr != nil {
				return SupplierOfferDraftCommercialState{}, copyErr
			}
			omitted = result.OmittedGroups
			state := result.SupplierOfferDraftCommercialState
			sourceID := source.ID
			state.SourceOfferVersionID = &sourceID
			return state, nil
		})
	if err != nil {
		if errors.Is(err, ErrOfferDraftConflict) {
			// The CAS lost. Reload and classify: a concurrent equivalent
			// copy-forward may have already pinned a source, in which case this
			// call converges instead of reporting a conflict for work that
			// actually succeeded.
			reloaded, reloadFound, reloadErr := repository.FindDraft(
				ctx, authorized.Access.CompanyID, current.ID)
			if reloadErr == nil && reloadFound && reloaded.SourceOfferVersionID != nil {
				return CopyForwardOutcome{Draft: reloaded}, nil
			}
		}
		return CopyForwardOutcome{}, err
	}
	if service.audit != nil {
		_ = service.audit.RecordOfferDraftCopied(ctx,
			draft.CompanyID, authorized.Access.SupplierID, draft.InvitationID,
			draft.OfferChainID, draft.ID, source.ID, draft.Revision, command.Context.AccessedAt)
	}
	return CopyForwardOutcome{Draft: draft, OmittedGroups: omitted}, nil
}

// sourceResolverChainRepository is the narrow read the M8.1 automatic source
// resolver needs beyond ordinary chain creation: every chain a company holds
// for one invitation, so resolution can search backward across earlier
// issued RFQ versions without reaching into another module's collection.
type sourceResolverChainRepository interface {
	ListChainsForInvitation(ctx context.Context, companyID, invitationID string) (
		[]SupplierOfferChain, error)
}

// resolveLatestEligibleSource finds the authoritative source for an automatic
// copy-forward (M8.1 amendment): the latest, non-withdrawn submission from an
// EARLIER issued RFQ version of the same invitation and Supplier. The caller
// never supplies a source ID; this is the only path that selects one.
//
// Search proceeds backward by issued RFQ version number, skipping versions
// with no submission and versions whose latest submission was withdrawn,
// until it finds one that is still eligible (or awarded — anything other than
// withdrawn) or exhausts every earlier version.
func (service *Service) resolveLatestEligibleSource(
	ctx context.Context,
	companyID string,
	invitationID string,
	targetIssuedRFQVersionID string,
) (SupplierOfferVersion, bool, error) {
	if service.chains == nil || service.issuedRFQ == nil ||
		service.versions == nil || service.eligibility == nil {
		return SupplierOfferVersion{}, false, ErrSupplierOffersNotConfigured
	}
	chainLister, ok := service.chains.(sourceResolverChainRepository)
	if !ok {
		return SupplierOfferVersion{}, false, ErrSupplierOffersNotConfigured
	}

	target, found, err := service.issuedRFQ.GetIssuedRFQForOffer(
		ctx, companyID, targetIssuedRFQVersionID)
	if err != nil {
		return SupplierOfferVersion{}, false, err
	}
	if !found {
		return SupplierOfferVersion{}, false, ErrIssuedRFQNotFound
	}

	chains, err := chainLister.ListChainsForInvitation(ctx, companyID, invitationID)
	if err != nil {
		return SupplierOfferVersion{}, false, err
	}

	// Order candidate chains by their issued RFQ version number, strictly
	// earlier than the target, newest first — the backward search order.
	type candidate struct {
		chain         SupplierOfferChain
		versionNumber int
	}
	candidates := make([]candidate, 0, len(chains))
	for _, chain := range chains {
		if chain.IssuedRFQVersionID == targetIssuedRFQVersionID {
			continue
		}
		if chain.LatestSubmittedID == nil {
			continue
		}
		issued, issuedFound, issuedErr := service.issuedRFQ.GetIssuedRFQForOffer(
			ctx, companyID, chain.IssuedRFQVersionID)
		if issuedErr != nil {
			return SupplierOfferVersion{}, false, issuedErr
		}
		if !issuedFound || issued.VersionNumber >= target.VersionNumber {
			continue
		}
		candidates = append(candidates, candidate{chain: chain, versionNumber: issued.VersionNumber})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].versionNumber > candidates[j].versionNumber
	})

	for _, entry := range candidates {
		versionID := *entry.chain.LatestSubmittedID
		version, versionFound, versionErr := service.versions.FindVersion(ctx, companyID, versionID)
		if versionErr != nil {
			return SupplierOfferVersion{}, false, versionErr
		}
		if !versionFound {
			continue
		}
		eligibility, eligibilityFound, eligibilityErr := service.eligibility.FindEligibility(
			ctx, companyID, versionID)
		if eligibilityErr != nil {
			return SupplierOfferVersion{}, false, eligibilityErr
		}
		if !eligibilityFound || eligibility.State == EligibilityWithdrawn {
			continue
		}
		return version, true, nil
	}
	return SupplierOfferVersion{}, false, nil
}

// mappingFromIssuedLines derives the lineage facts from the authoritative
// issued version. Deriving them here rather than accepting them from the client
// keeps matching under server control.
func mappingFromIssuedLines(rfq IssuedRFQSnapshot) CopyForwardSourceMapping {
	mapping := CopyForwardSourceMapping{
		LineMaterialRequirementIDs: map[string]string{},
		LineLineageIDs:             map[string]string{},
	}
	for _, line := range rfq.Lines {
		if line.SourceMaterialRequirementID != nil {
			mapping.LineMaterialRequirementIDs[line.ID] = *line.SourceMaterialRequirementID
		}
		mapping.LineLineageIDs[line.ID] = line.LineageID
	}
	return mapping
}

// matchSourceLines resolves target RFQ line ID -> source RFQ line ID.
//
// M7-backed lines match on SourceMaterialRequirementID; M8-native lines match
// on LineageID. A target matching more than one source line is ambiguous and
// is left unmatched rather than guessed at.
func matchSourceLines(
	input CopyForwardInput,
	targetIssuedByID map[string]IssuedRFQLineSnapshot,
) map[string]string {
	byMaterialRequirement := make(map[string][]string)
	byLineage := make(map[string][]string)
	for sourceRFQLineID, requirementID := range input.Mapping.LineMaterialRequirementIDs {
		byMaterialRequirement[requirementID] = append(
			byMaterialRequirement[requirementID], sourceRFQLineID)
	}
	for sourceRFQLineID, lineageID := range input.Mapping.LineLineageIDs {
		byLineage[lineageID] = append(byLineage[lineageID], sourceRFQLineID)
	}

	matched := make(map[string]string, len(targetIssuedByID))
	for targetID, issued := range targetIssuedByID {
		if issued.SourceMaterialRequirementID != nil {
			candidates := byMaterialRequirement[*issued.SourceMaterialRequirementID]
			if len(candidates) == 1 {
				matched[targetID] = candidates[0]
			}
			continue
		}
		// M8-native lines carry no Material Requirement, so lineage is the
		// only stable identity across versions.
		if candidates := byLineage[issued.LineageID]; len(candidates) == 1 {
			matched[targetID] = candidates[0]
		}
	}
	return matched
}

// copyLine fills one target line from its source counterpart. It reports
// whether the result is a quoted response, which group eligibility depends on.
func copyLine(
	target SupplierOfferDraftLine,
	source SupplierOfferLine,
	targetIssued IssuedRFQLineSnapshot,
	sourceIssued IssuedRFQLineSnapshot,
	sourceVersionID string,
	copiedAt time.Time,
) (SupplierOfferDraftLine, bool) {

	stamp := copiedAt
	target.CopiedFromOfferVersionID = sourceVersionID
	target.CopiedFromOfferLineID = source.ID
	target.CopiedAt = &stamp

	// A different material is a different product. Carrying a price across
	// would misrepresent what the Supplier actually quoted.
	if sourceIssued.MaterialID != "" &&
		targetIssued.MaterialID != "" &&
		sourceIssued.MaterialID != targetIssued.MaterialID {
		target.ResponseStatus = OfferLineUnanswered
		return target, false
	}

	// M8.1 amendment to M8 §7.1: stable lineage is necessary but no longer
	// sufficient. The response copies only when the source and target RFQ
	// line snapshots share the same rfq-line-commercial-v1 fingerprint. A
	// fingerprint mismatch — including an unknown source snapshot, which
	// cannot prove equivalence — leaves the line unanswered rather than
	// copying it with a review flag: the fingerprint alone cannot establish
	// that a previously quoted price is still correct for a materially
	// different commercial ask.
	if sourceIssued.ID == "" ||
		CommercialFingerprintV1(sourceIssued) != CommercialFingerprintV1(targetIssued) {
		target.ResponseStatus = OfferLineUnanswered
		return target, false
	}

	switch source.ResponseStatus {
	case OfferLineNoBid, OfferLineUnavailable:
		target.ResponseStatus = source.ResponseStatus
		target.SupplierLineNotes = source.SupplierLineNotes
		// A decline always requires explicit reconfirmation, even when nothing
		// changed: the Supplier must restate an intent not to quote.
		target.ConfirmationRequired = true
		return target, false

	case OfferLineQuoted:
		if source.UnitPriceExcludingTax == nil {
			target.ResponseStatus = OfferLineUnanswered
			return target, false
		}
		// Recalculate against the TARGET quantity and the copied unit price,
		// through the same centralized calculation submission uses.
		unitPrice := *source.UnitPriceExcludingTax
		calculated, err := CalculateQuotedLine(unitPrice.Currency, QuotedLineInput{
			RFQLineID:             targetIssued.ID,
			AuthoritativeQuantity: targetIssued.Quantity,
			QuotedQuantity:        targetIssued.Quantity,
			UnitPrice:             unitPrice,
		})
		if err != nil {
			// An incompatible price cannot be carried forward silently.
			target.ResponseStatus = OfferLineUnanswered
			return target, false
		}
		quotedQuantity := calculated.QuotedQuantity
		subtotal := calculated.LineSubtotal

		target.ResponseStatus = OfferLineQuoted
		target.QuotedQuantity = &quotedQuantity
		target.UnitPriceExcludingTax = &unitPrice
		target.LineSubtotalExcludingTax = &subtotal
		target.Brand = source.Brand
		target.SKU = source.SKU
		target.ProductDescription = source.ProductDescription
		target.LeadTime = source.LeadTime
		target.SupplierLineNotes = source.SupplierLineNotes
		target.CommercialExceptions = source.CommercialExceptions
		target.LineTax = source.LineTax
		// A matching fingerprint already proves nothing the price was quoted
		// against has moved, so no review gate is needed.
		target.ReviewRequired = false
		return target, true

	default:
		target.ResponseStatus = OfferLineUnanswered
		return target, false
	}
}

// copyTax applies the discriminated tax copy rules.
func copyTax(source SupplierOfferTax) (SupplierOfferTax, bool) {
	switch source.Mode {
	case TaxModeOfferLevel:
		// Copy only a structurally valid record, and always gate it: the
		// amount was calculated against a different offer.
		if source.OfferLevel == nil || source.OfferLevel.TaxAmount.Amount <= 0 {
			return SupplierOfferTax{Mode: TaxModeNotApplicable}, false
		}
		offerLevel := *source.OfferLevel
		return SupplierOfferTax{Mode: TaxModeOfferLevel, OfferLevel: &offerLevel}, true

	case TaxModeLineLevel:
		// Line-level records travel with their lines and are recalculated
		// there, so the whole-offer flag stays clear.
		return SupplierOfferTax{Mode: TaxModeLineLevel}, false

	default:
		// not_applicable has nothing to confirm.
		return SupplierOfferTax{Mode: TaxModeNotApplicable}, false
	}
}

// copyChargeGroups copies only fully eligible groups and omits the rest WHOLE.
//
// Narrowing a group's membership would silently change its commercial meaning,
// so a single ineligible line disqualifies the entire group.
func copyChargeGroups(
	input CopyForwardInput,
	sourceRFQLineForTarget map[string]string,
	copiedTargetRFQLines map[string]bool,
) ([]SupplierChargeGroupDraft, []OmittedChargeGroup) {

	targetForSourceRFQLine := make(map[string]string, len(sourceRFQLineForTarget))
	for targetID, sourceID := range sourceRFQLineForTarget {
		if _, duplicate := targetForSourceRFQLine[sourceID]; duplicate {
			// One source line mapping to several targets is ambiguous.
			targetForSourceRFQLine[sourceID] = ""
			continue
		}
		targetForSourceRFQLine[sourceID] = targetID
	}

	groups := make([]SupplierChargeGroupDraft, 0, len(input.Source.ChargeGroups))
	omitted := make([]OmittedChargeGroup, 0)
	claimedLines := make(map[string]bool)

	for _, source := range input.Source.ChargeGroups {
		remapped := make([]string, 0, len(source.ApplicableRFQLineIDs))
		reason, failed := CopyOmissionReason(""), false

		for _, sourceRFQLineID := range source.ApplicableRFQLineIDs {
			targetRFQLineID, mapped := targetForSourceRFQLine[sourceRFQLineID]
			switch {
			case !mapped:
				reason, failed = CopyOmissionMissingLine, true
			case targetRFQLineID == "":
				reason, failed = CopyOmissionAmbiguousMapping, true
			case !copiedTargetRFQLines[targetRFQLineID]:
				// Present but not carried across as a quoted response.
				reason, failed = CopyOmissionNonQuotedLine, true
			case claimedLines[targetRFQLineID]:
				// Already inside another copied group: groups must not overlap.
				reason, failed = CopyOmissionOverlappingGroup, true
			default:
				remapped = append(remapped, targetRFQLineID)
			}
			if failed {
				break
			}
		}

		if failed || len(remapped) == 0 {
			if reason == "" {
				reason = CopyOmissionMissingLine
			}
			omitted = append(omitted, OmittedChargeGroup{
				SourceChargeGroupID:   source.ID,
				SourceChargeGroupName: source.Name,
				Reason:                reason,
			})
			continue
		}

		for _, targetRFQLineID := range remapped {
			claimedLines[targetRFQLineID] = true
		}

		copied := source
		// A new draft-owned identity: the copy is a different group on a
		// different offer, even though it retains its source provenance.
		copied.ID = bson.NewObjectID().Hex()
		copied.ApplicableRFQLineIDs = remapped

		groups = append(groups, SupplierChargeGroupDraft{
			ConditionalChargeGroup:  copied,
			CopiedFromChargeGroupID: source.ID,
			ReviewRequired:          true,
		})
	}

	return groups, omitted
}
