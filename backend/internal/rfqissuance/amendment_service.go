package rfqissuance

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

// The amendment flow (design spec §4.2):
//
//	Issued Version N -> create draft -> edit -> issue -> immutable Version N+1
//
// Nothing here ever mutates an issued version. That is the whole point of the
// draft: a Supplier reading Version N is unaffected by preparation of N+1.

// currentIssuedVersion resolves the version the chain currently points at.
//
// A chain with no pointer has nothing to amend and is reported as not-found,
// collapsing "no chain", "foreign chain" and "chain never issued" so a foreign
// caller learns nothing about which is true.
func (s *Service) currentIssuedVersion(ctx context.Context,
	companyID, rfqChainID string) (IssuedRFQVersion, error) {

	chain, err := s.chains.FindChain(ctx, companyID, rfqChainID)
	if err != nil {
		return IssuedRFQVersion{}, err
	}
	if chain.CurrentIssuedVersionID == nil {
		return IssuedRFQVersion{}, ErrIssuanceChainNotFound
	}
	return s.versions.FindVersion(ctx, companyID, *chain.CurrentIssuedVersionID)
}

// GetIssuedVersion reads one immutable version, tenant-scoped.
func (s *Service) GetIssuedVersion(ctx context.Context,
	companyID, versionID string) (IssuedRFQVersion, error) {
	return s.versions.FindVersion(ctx, companyID, versionID)
}

// GetAmendmentDraft reads the chain's single draft.
func (s *Service) GetAmendmentDraft(ctx context.Context,
	companyID, rfqChainID string) (RFQAmendmentDraft, error) {
	if s.drafts == nil {
		return RFQAmendmentDraft{}, ErrIssuanceNotConfigured
	}
	return s.drafts.FindDraft(ctx, companyID, rfqChainID)
}

// CreateAmendmentDraft clones the current issued version into an editable
// draft.
//
// Only one draft may exist per chain: two would let two contractors prepare
// divergent "next versions", and only one could ever be issued.
func (s *Service) CreateAmendmentDraft(ctx context.Context, companyID, actorUserID,
	rfqChainID string) (RFQAmendmentDraft, error) {

	if s.drafts == nil || s.chains == nil {
		return RFQAmendmentDraft{}, ErrIssuanceNotConfigured
	}

	current, err := s.currentIssuedVersion(ctx, companyID, rfqChainID)
	if err != nil {
		return RFQAmendmentDraft{}, err
	}

	draft, err := s.drafts.CreateDraft(
		ctx, newAmendmentDraftFrom(current, actorUserID))
	if err != nil {
		return RFQAmendmentDraft{}, err
	}
	_ = s.audit.RecordRFQAmendmentDraftCreated(
		ctx, companyID, current.ProjectID, actorUserID,
		current.RFQChainID, current.RFQNumber, draft.ID, draft.BaseVersionNumber)
	return draft, nil
}

// UpdateAmendmentDraft applies a sparse edit under a revision guard, so a stale
// editor cannot silently overwrite a newer edit (§11.3).
func (s *Service) UpdateAmendmentDraft(ctx context.Context, companyID, actorUserID,
	rfqChainID string, expectedRevision int64, patch AmendmentDraftPatch) (
	RFQAmendmentDraft, error) {

	if s.drafts == nil {
		return RFQAmendmentDraft{}, ErrIssuanceNotConfigured
	}

	existing, err := s.drafts.FindDraft(ctx, companyID, rfqChainID)
	if err != nil {
		return RFQAmendmentDraft{}, err
	}

	next, err := patch.applyTo(existing)
	if err != nil {
		return RFQAmendmentDraft{}, err
	}
	updated, err := s.drafts.UpdateDraft(
		ctx, companyID, rfqChainID, expectedRevision, next)
	if err != nil {
		return RFQAmendmentDraft{}, err
	}
	_ = s.audit.RecordRFQAmendmentDraftUpdated(
		ctx, companyID, actorUserID, rfqChainID, updated.ID, updated.Revision)
	return updated, nil
}

// DiscardAmendmentDraft removes the draft, leaving every issued version intact.
func (s *Service) DiscardAmendmentDraft(ctx context.Context, companyID, actorUserID,
	rfqChainID string, expectedRevision int64) error {

	if s.drafts == nil {
		return ErrIssuanceNotConfigured
	}
	draft, err := s.drafts.FindDraft(ctx, companyID, rfqChainID)
	if err != nil {
		return err
	}
	if err := s.drafts.DeleteDraft(
		ctx, companyID, rfqChainID, expectedRevision); err != nil {
		return err
	}
	_ = s.audit.RecordRFQAmendmentDraftDiscarded(
		ctx, companyID, actorUserID, rfqChainID, draft.ID, draft.Revision)
	return nil
}

// IssueAmendmentInput carries one amendment issuance.
type IssueAmendmentInput struct {
	RFQChainID       string
	ExpectedRevision int64
	OperationID      string
}

func amendmentIssuanceMatches(existing IssuedRFQVersion,
	input IssueAmendmentInput, draft RFQAmendmentDraft,
	base IssuedRFQVersion) bool {

	return existing.RFQChainID == input.RFQChainID &&
		existing.VersionNumber == draft.BaseVersionNumber+1 &&
		existing.Currency == draft.Currency &&
		existing.SourceM7RFQRevision == base.SourceM7RFQRevision &&
		existing.SourceFingerprint == fingerprintAmendmentDraft(draft)
}

// IssueAmendment turns the draft into immutable Version N+1 (§4.2).
//
// The ordered checks matter:
//
//  1. resolve idempotency, so a retry returns its original version;
//  2. load the draft under its expected revision;
//  3. verify the base is STILL the chain's current version — otherwise this
//     draft was built on a version another amendment has since superseded, and
//     issuing it would silently discard that amendment's changes;
//  4. require a response deadline, exactly as first issuance does (§4.1A);
//  5. derive the one candidate N+1 from the chain, create, then advance — the
//     same write order as first issuance, so concurrent callers contend for
//     the SAME named unique key and an interruption leaves a reconcilable
//     pointer rather than a phantom version.
//
// The draft is archived only after the version exists.
func (s *Service) IssueAmendment(ctx context.Context, companyID, actorUserID string,
	input IssueAmendmentInput) (IssuedRFQVersion, error) {

	if procurementlimits.ValidateID(strings.TrimSpace(input.OperationID)) != nil {
		return IssuedRFQVersion{}, ErrOperationIDRequired
	}
	if s.drafts == nil || s.chains == nil {
		return IssuedRFQVersion{}, ErrIssuanceNotConfigured
	}

	if existing, found, err := s.versions.FindByOperationID(ctx, companyID,
		input.OperationID); err != nil {
		return IssuedRFQVersion{}, err
	} else if found {
		// The operation index is company-wide. A valid amendment retry must
		// resolve to a later version on this exact chain; Version 1 belongs to
		// the distinct initial-issuance operation.
		if existing.RFQChainID != input.RFQChainID ||
			existing.VersionNumber <= 1 {
			return IssuedRFQVersion{}, ErrOperationAlreadyUsed
		}
		// A draft may still exist when the first attempt inserted the version
		// but stopped before pointer/draft cleanup. It is the same operation
		// only when the exact draft fingerprint and expected revision match.
		draft, draftErr := s.drafts.FindDraft(ctx, companyID, input.RFQChainID)
		switch {
		case draftErr == nil:
			if draft.Revision != input.ExpectedRevision ||
				existing.VersionNumber != draft.BaseVersionNumber+1 ||
				existing.SourceFingerprint != fingerprintAmendmentDraft(draft) {
				return IssuedRFQVersion{}, ErrOperationAlreadyUsed
			}
			_, _ = s.ReconcileIssuanceChain(
				ctx, companyID, actorUserID, input.RFQChainID)
			_ = s.drafts.DeleteDraft(
				ctx, companyID, input.RFQChainID, draft.Revision)
		case errors.Is(draftErr, ErrAmendmentDraftNotFound):
			// Normal retry after successful cleanup: the immutable version and
			// operation index are now the complete authority.
		default:
			return IssuedRFQVersion{}, draftErr
		}
		return existing, nil
	}

	draft, err := s.drafts.FindDraft(ctx, companyID, input.RFQChainID)
	if err != nil {
		return IssuedRFQVersion{}, err
	}
	if draft.Revision != input.ExpectedRevision {
		return IssuedRFQVersion{}, ErrRevisionMismatch
	}

	current, err := s.currentIssuedVersion(ctx, companyID, input.RFQChainID)
	if err != nil {
		return IssuedRFQVersion{}, err
	}
	if draft.BaseIssuedVersionID != current.ID {
		// A concurrent retry of THIS operation may have advanced the chain
		// after our first operation lookup but before this read. Re-resolve at
		// the boundary where that success becomes observable; accept it only
		// when the immutable version matches this exact draft fingerprint.
		existing, found, findErr := s.versions.FindByOperationID(
			ctx, companyID, input.OperationID)
		if findErr != nil {
			return IssuedRFQVersion{}, findErr
		}
		if found && amendmentIssuanceMatches(existing, input, draft, current) {
			_, _ = s.ReconcileIssuanceChain(
				ctx, companyID, actorUserID, input.RFQChainID)
			_ = s.drafts.DeleteDraft(
				ctx, companyID, input.RFQChainID, draft.Revision)
			return existing, nil
		}
		return IssuedRFQVersion{}, ErrStaleBaseVersion
	}

	if draft.ResponseDeadline == nil {
		return IssuedRFQVersion{}, ErrResponseDeadlineRequired
	}

	chain, err := s.chains.FindChain(ctx, companyID, input.RFQChainID)
	if err != nil {
		return IssuedRFQVersion{}, err
	}
	if chain.CurrentIssuedVersionID == nil ||
		*chain.CurrentIssuedVersionID != draft.BaseIssuedVersionID {
		// The same-operation winner can advance the chain between the current
		// version read above and this chain read. Re-resolve its immutable
		// operation identity at this second authoritative observation boundary;
		// otherwise a legitimate retry is misclassified as a stale draft.
		existing, found, findErr := s.versions.FindByOperationID(
			ctx, companyID, input.OperationID)
		if findErr != nil {
			return IssuedRFQVersion{}, findErr
		}
		if found && amendmentIssuanceMatches(existing, input, draft, current) {
			_, _ = s.ReconcileIssuanceChain(
				ctx, companyID, actorUserID, input.RFQChainID)
			_ = s.drafts.DeleteDraft(
				ctx, companyID, input.RFQChainID, draft.Revision)
			return existing, nil
		}
		return IssuedRFQVersion{}, ErrStaleBaseVersion
	}

	version, err := NewIssuedVersion(NewIssuedVersionInput{
		CompanyID: companyID, ProjectID: current.ProjectID,
		RFQChainID: input.RFQChainID, RFQNumber: current.RFQNumber,
		VersionNumber: chain.LatestIssuedVersion + 1,
		// Cloned from the chain's first issuance and never editable: changing
		// currency mid-chain would invalidate every offer already submitted.
		Currency:             draft.Currency,
		Title:                draft.Title,
		DeliveryAddress:      draft.DeliveryAddress,
		RequiredByDate:       draft.RequiredByDate,
		ResponseDeadline:     draft.ResponseDeadline,
		SupplierInstructions: draft.SupplierInstructions,
		Lines:                draft.Lines,
		// The amendment preserves the original M7 provenance while replacing
		// the source fingerprint with the exact draft revision that produced
		// this version. These identities are what make an operation-ID retry
		// verifiable after the mutable draft is consumed.
		SourceM7RFQRevision: current.SourceM7RFQRevision,
		SourceFingerprint:   fingerprintAmendmentDraft(draft),
		IssuanceOperationID: input.OperationID,
		IssuedByUserID:      actorUserID,
		IssuedAt:            time.Now(),
	})
	if err != nil {
		return IssuedRFQVersion{}, err
	}

	created, err := s.versions.CreateVersion(ctx, version)
	if err != nil {
		// As with initial issuance, concurrent retries can both miss the
		// operation lookup before Mongo selects one insert winner. Resolve the
		// winner by operation ID and accept it only when it is exactly this
		// draft revision and base; a different operation keeps the conflict.
		if errors.Is(err, ErrVersionAlreadyExists) ||
			errors.Is(err, ErrOperationAlreadyUsed) {
			existing, found, findErr := s.versions.FindByOperationID(
				ctx, companyID, input.OperationID)
			if findErr != nil {
				return IssuedRFQVersion{}, findErr
			}
			if found {
				if !amendmentIssuanceMatches(existing, input, draft, current) {
					return IssuedRFQVersion{}, ErrOperationAlreadyUsed
				}
				_, _ = s.ReconcileIssuanceChain(
					ctx, companyID, actorUserID, input.RFQChainID)
				_ = s.drafts.DeleteDraft(
					ctx, companyID, input.RFQChainID, draft.Revision)
				return existing, nil
			}
		}
		return IssuedRFQVersion{}, err
	}

	_ = s.audit.RecordRFQAmendmentIssued(
		ctx, companyID, created.ProjectID, actorUserID,
		created.RFQChainID, created.RFQNumber, draft.ID, created.ID,
		created.VersionNumber)

	// The version now exists and is authoritative. Everything below is
	// reconcilable cleanup: failing it must not report the issuance as failed,
	// because a Supplier may already be able to reach the new version.
	_, _ = s.chains.AdvanceChain(ctx, companyID, input.RFQChainID, chain.Revision,
		created.VersionNumber, created.ID)

	// Step 6: move every non-revoked invitation onto the new version. This runs
	// AFTER the version exists, so an invitation can never point at a partially
	// created one; a failure here leaves invitations on the complete previous
	// version for ReconcileInvitationAdvancement to repair (§4.2 steps 6-7).
	s.advanceInvitationsAfterIssuance(ctx, companyID, actorUserID,
		input.RFQChainID, created.ID)

	_ = s.drafts.DeleteDraft(ctx, companyID, input.RFQChainID, draft.Revision)

	return created, nil
}
