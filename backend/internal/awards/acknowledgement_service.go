package awards

import (
	"context"
	"time"
)

// Acknowledgement recording (§8J).
//
// The public route lives in `supplieraccess`, which owns the Phase D session,
// CSRF and binding checks. This module owns the record and its idempotency, and
// exposes the operation through the same SupplierOutcomeSource boundary the
// outcome read uses (§8A.1).

// AwardAcknowledgementRepository is the narrow receipt surface.
type AwardAcknowledgementRepository interface {
	EnsureAcknowledgement(
		ctx context.Context,
		candidate AwardOutcomeAcknowledgement,
	) (AwardOutcomeAcknowledgement, bool, error)
	FindAcknowledgement(
		ctx context.Context,
		companyID, awardOutcomeID string,
	) (AwardOutcomeAcknowledgement, bool, error)
}

func WithAwardAcknowledgementRepository(
	repository AwardAcknowledgementRepository,
) ServiceOption {
	return func(service *Service) { service.acknowledgements = repository }
}

// AcknowledgeOutcomeInput carries identities the CALLER has already validated
// against the Phase D session. This module never accepts a claimed Supplier or
// Company from request content.
type AcknowledgeOutcomeInput struct {
	OutcomeID         string
	SupplierID        string
	InvitationID      string
	SessionID         string
	RecipientIdentity string
	OperationID       string
	AcknowledgedAt    time.Time
}

// AcknowledgeOutcome records the first receipt for an outcome.
//
// It returns created=false when a receipt already exists, which the route maps
// to 200 rather than 201: the second caller did not create anything, and
// saying otherwise would misreport what happened.
func (service *Service) AcknowledgeOutcome(
	ctx context.Context,
	companyID string,
	input AcknowledgeOutcomeInput,
) (AwardOutcomeAcknowledgement, bool, error) {
	if service.outcomes == nil || service.acknowledgements == nil {
		return AwardOutcomeAcknowledgement{}, false, ErrAwardsNotConfigured
	}

	// The outcome must belong to THIS Supplier under THIS invitation (D3).
	// Another Supplier's outcome is a bounded not-found, never a 403 that would
	// confirm the outcome exists.
	outcome, found, err := service.outcomes.FindOutcomeForSupplier(
		ctx, companyID, input.OutcomeID, input.SupplierID, input.InvitationID)
	if err != nil {
		return AwardOutcomeAcknowledgement{}, false, err
	}
	if !found {
		return AwardOutcomeAcknowledgement{}, false, ErrAwardOutcomeNotFound
	}

	acknowledgedAt := input.AcknowledgedAt
	if acknowledgedAt.IsZero() {
		acknowledgedAt = time.Now().UTC()
	}

	acknowledgement, created, err := service.acknowledgements.
		EnsureAcknowledgement(ctx, AwardOutcomeAcknowledgement{
			CompanyID:      companyID,
			AwardOutcomeID: outcome.ID,
			// Taken from the outcome, not the request: they were already
			// verified when the outcome resolved.
			SupplierID:        outcome.SupplierID,
			InvitationID:      outcome.InvitationID,
			SessionID:         input.SessionID,
			RecipientIdentity: input.RecipientIdentity,
			OperationID:       input.OperationID,
			AcknowledgedAt:    acknowledgedAt,
		})
	if err != nil {
		return AwardOutcomeAcknowledgement{}, false, err
	}

	// Ensure on every completion path. The audit repository deduplicates the
	// authoritative outcome subject, so replay can repair a crash after the
	// receipt insert without representing a second acknowledgement.
	if service.audit != nil {
		_ = service.audit.RecordAwardOutcomeAcknowledged(ctx,
			companyID, acknowledgement.AwardOutcomeID,
			acknowledgement.SupplierID, acknowledgement.InvitationID,
			acknowledgement.SessionID,
			acknowledgement.AcknowledgedAt)
	}
	return acknowledgement, created, nil
}

// GetOutcomeAcknowledgement reads an outcome's receipt, if any.
func (service *Service) GetOutcomeAcknowledgement(
	ctx context.Context,
	companyID, outcomeID string,
) (AwardOutcomeAcknowledgement, bool, error) {
	if service.acknowledgements == nil {
		return AwardOutcomeAcknowledgement{}, false, ErrAwardsNotConfigured
	}
	return service.acknowledgements.FindAcknowledgement(
		ctx, companyID, outcomeID)
}
