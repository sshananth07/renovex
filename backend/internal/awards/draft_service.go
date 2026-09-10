package awards

import (
	"context"
	"time"
)

// Provisional award draft lifecycle (§8C).
//
// Every operation here is preparation: a draft claims nothing, touches no offer
// eligibility and has no externally visible effect. That is precisely why
// employees may perform them (Decision A §1A.1) — the irreversible, externally
// visible boundary starts at finalisation.

// AwardChainRepository is the narrow chain surface the draft lifecycle needs.
// Finalisation extends its own explicit boundary rather than this one growing
// into a generic repository escape hatch.
type AwardChainRepository interface {
	EnsureAwardChain(
		ctx context.Context,
		companyID, rfqChainID, issuedRFQVersionID string,
	) (AwardDecisionChain, error)
	FindChain(
		ctx context.Context,
		companyID, chainID string,
	) (AwardDecisionChain, bool, error)
	FindChainByIssuedVersion(
		ctx context.Context,
		companyID, issuedRFQVersionID string,
	) (AwardDecisionChain, bool, error)
}

type AwardDraftRepository interface {
	EnsureOpenDraft(
		ctx context.Context,
		candidate AwardDraft,
	) (AwardDraft, bool, error)
	FindOpenDraft(
		ctx context.Context,
		companyID, awardChainID string,
	) (AwardDraft, bool, error)
	FindDraft(
		ctx context.Context,
		companyID, draftID string,
	) (AwardDraft, bool, error)
	ReplaceDecisions(
		ctx context.Context,
		companyID, draftID string,
		expectedRevision int64,
		decisions []AwardLineDecisionDraft,
	) (AwardDraft, error)
	ArchiveDraft(
		ctx context.Context,
		companyID, draftID string,
		expectedRevision int64,
	) error
}

func WithAwardChainRepository(repository AwardChainRepository) ServiceOption {
	return func(service *Service) { service.chains = repository }
}

func WithAwardDraftRepository(repository AwardDraftRepository) ServiceOption {
	return func(service *Service) { service.drafts = repository }
}

// CreateAwardDraft opens the chain's provisional draft, or adopts the existing
// one. Adoption emits no audit event: no authoritative write happened, and an
// audit trail padded with non-events makes the real ones harder to find.
func (service *Service) CreateAwardDraft(
	ctx context.Context,
	companyID, actorUserID, issuedRFQVersionID string,
) (AwardDraft, error) {
	if service.issuedRFQ == nil || service.chains == nil || service.drafts == nil {
		return AwardDraft{}, ErrAwardsNotConfigured
	}

	issued, found, err := service.issuedRFQ.GetIssuedRFQForAward(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return AwardDraft{}, err
	}
	if !found {
		return AwardDraft{}, ErrIssuedRFQNotFound
	}

	chain, err := service.chains.EnsureAwardChain(
		ctx, companyID, issued.RFQChainID, issued.ID)
	if err != nil {
		return AwardDraft{}, err
	}

	draft, created, err := service.drafts.EnsureOpenDraft(ctx, AwardDraft{
		CompanyID:          companyID,
		AwardChainID:       chain.ID,
		IssuedRFQVersionID: issued.ID,
		CreatedByUserID:    actorUserID,
	})
	if err != nil {
		return AwardDraft{}, err
	}
	if created {
		service.recordDraftAudit(ctx, "created", companyID, actorUserID, draft)
	}
	return draft, nil
}

// GetAwardDraft returns the chain's open draft.
//
// A chain with no open draft is a bounded not-found rather than an empty draft:
// an empty draft would read as "every line deliberately undecided", which is a
// materially different claim.
func (service *Service) GetAwardDraft(
	ctx context.Context,
	companyID, issuedRFQVersionID string,
) (AwardDraft, error) {
	_, draft, err := service.resolveOpenDraft(ctx, companyID, issuedRFQVersionID)
	return draft, err
}

// resolveOpenDraft loads the chain and its open draft together, refusing the
// pair whenever the chain has left `draft` state.
func (service *Service) resolveOpenDraft(
	ctx context.Context,
	companyID, issuedRFQVersionID string,
) (AwardDecisionChain, AwardDraft, error) {
	if service.chains == nil || service.drafts == nil {
		return AwardDecisionChain{}, AwardDraft{}, ErrAwardsNotConfigured
	}

	chain, found, err := service.chains.FindChainByIssuedVersion(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return AwardDecisionChain{}, AwardDraft{}, err
	}
	if !found {
		return AwardDecisionChain{}, AwardDraft{}, ErrAwardDraftNotFound
	}

	draft, found, err := service.drafts.FindOpenDraft(ctx, companyID, chain.ID)
	if err != nil {
		return AwardDecisionChain{}, AwardDraft{}, err
	}
	if !found {
		return AwardDecisionChain{}, AwardDraft{}, ErrAwardDraftNotFound
	}
	return chain, draft, nil
}

type SelectAwardLineInput struct {
	IssuedRFQVersionID string
	IssuedRFQLineID    string
	OfferVersionID     string
	OfferLineID        string
	ExpectedRevision   int64
}

// SelectAwardLine records a provisional selection for one issued line.
//
// The stable lineage is read from the ISSUED version, never from the request:
// F4's cross-version claim is keyed on it, so a caller-supplied lineage could
// redirect a claim at a line it does not own.
func (service *Service) SelectAwardLine(
	ctx context.Context,
	companyID, actorUserID string,
	input SelectAwardLineInput,
) (AwardDraft, error) {
	return service.applyDecision(ctx, companyID, actorUserID,
		input.IssuedRFQVersionID, input.IssuedRFQLineID, input.ExpectedRevision,
		func(lineageID string) AwardLineDecisionDraft {
			return AwardLineDecisionDraft{
				IssuedRFQLineID: input.IssuedRFQLineID,
				StableLineageID: lineageID,
				Decision:        AwardDecisionSelected,
				OfferVersionID:  input.OfferVersionID,
				OfferLineID:     input.OfferLineID,
			}
		})
}

type UnawardLineInput struct {
	IssuedRFQVersionID string
	IssuedRFQLineID    string
	Reason             UnawardedReason
	Note               string
	ExpectedRevision   int64
}

// UnawardLine records a deliberate decision not to buy one issued line.
func (service *Service) UnawardLine(
	ctx context.Context,
	companyID, actorUserID string,
	input UnawardLineInput,
) (AwardDraft, error) {
	return service.applyDecision(ctx, companyID, actorUserID,
		input.IssuedRFQVersionID, input.IssuedRFQLineID, input.ExpectedRevision,
		func(lineageID string) AwardLineDecisionDraft {
			return AwardLineDecisionDraft{
				IssuedRFQLineID: input.IssuedRFQLineID,
				StableLineageID: lineageID,
				Decision:        AwardDecisionUnawarded,
				UnawardedReason: input.Reason,
				UnawardedNote:   input.Note,
			}
		})
}

// applyDecision is the shared edit path: resolve the issued line, build the
// decision, replace any existing decision for that line, and CAS the draft.
func (service *Service) applyDecision(
	ctx context.Context,
	companyID, actorUserID, issuedRFQVersionID, issuedRFQLineID string,
	expectedRevision int64,
	build func(lineageID string) AwardLineDecisionDraft,
) (AwardDraft, error) {
	if service.issuedRFQ == nil {
		return AwardDraft{}, ErrAwardsNotConfigured
	}

	issued, found, err := service.issuedRFQ.GetIssuedRFQForAward(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return AwardDraft{}, err
	}
	if !found {
		return AwardDraft{}, ErrIssuedRFQNotFound
	}

	// Awarding a line the Supplier was never asked to quote would produce a
	// commitment with no authoritative source.
	lineageID := ""
	for _, line := range issued.Lines {
		if line.ID == issuedRFQLineID {
			lineageID = line.LineageID
			break
		}
	}
	if lineageID == "" {
		return AwardDraft{}, ErrInvalidAwardDecision
	}

	chain, draft, err := service.resolveOpenDraft(ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return AwardDraft{}, err
	}
	// The chain state, not the draft row, is the guard that survives a crash
	// mid-finalisation: the draft stays open while the chain is finalising.
	if !chain.AllowsDraftMutation() {
		return AwardDraft{}, ErrAwardDraftConflict
	}

	decision := build(lineageID)
	if err := decision.Validate(); err != nil {
		return AwardDraft{}, err
	}

	// One line carries exactly one decision: replace rather than append, or a
	// lineage claim cannot resolve which decision applies.
	decisions := make([]AwardLineDecisionDraft, 0, len(draft.LineDecisions)+1)
	for _, existing := range draft.LineDecisions {
		if existing.IssuedRFQLineID != issuedRFQLineID {
			decisions = append(decisions, existing)
		}
	}
	decisions = append(decisions, decision)

	updated, err := service.drafts.ReplaceDecisions(
		ctx, companyID, draft.ID, expectedRevision, decisions)
	if err != nil {
		return AwardDraft{}, err
	}
	service.recordDraftAudit(ctx, "updated", companyID, actorUserID, updated)
	return updated, nil
}

// DiscardAwardDraft archives the chain's open draft, freeing the slot for a
// fresh set of decisions.
func (service *Service) DiscardAwardDraft(
	ctx context.Context,
	companyID, actorUserID, issuedRFQVersionID string,
	expectedRevision int64,
) error {
	chain, draft, err := service.resolveOpenDraft(ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return err
	}
	if !chain.AllowsDraftMutation() {
		return ErrAwardDraftConflict
	}
	if err := service.drafts.ArchiveDraft(
		ctx, companyID, draft.ID, expectedRevision); err != nil {
		return err
	}
	draft.Revision = expectedRevision + 1
	service.recordDraftAudit(ctx, "discarded", companyID, actorUserID, draft)
	return nil
}

// recordDraftAudit emits a primitive-only draft event.
//
// Audit failure never fails the write: the decision is already persisted, and
// refusing the response would invite a retry that changes nothing while leaving
// the caller believing their edit was lost.
func (service *Service) recordDraftAudit(
	ctx context.Context,
	kind, companyID, actorUserID string,
	draft AwardDraft,
) {
	if service.audit == nil {
		return
	}
	now := time.Now().UTC()
	switch kind {
	case "created":
		_ = service.audit.RecordAwardDraftCreated(ctx, companyID, actorUserID,
			draft.AwardChainID, draft.ID, draft.Revision, now)
	case "updated":
		_ = service.audit.RecordAwardDraftUpdated(ctx, companyID, actorUserID,
			draft.AwardChainID, draft.ID, draft.Revision, now)
	case "discarded":
		_ = service.audit.RecordAwardDraftDiscarded(ctx, companyID, actorUserID,
			draft.AwardChainID, draft.ID, draft.Revision, now)
	}
}
