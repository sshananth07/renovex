package supplieraccess

import (
	"context"
	"time"
)

// Supplier-facing award outcome access (M8 §8A.1, D3).
//
// The routes live HERE, not in awards, because they must sit behind the Phase D
// session boundary this module owns: session validation, current-generation
// binding and CSRF. awards owns the outcome record and acknowledgement state
// and never sees a browser credential.
//
// Outcomes reuse the existing invitation session. No second credential is
// minted and no invitation is reactivated to grant outcome access.

// SupplierOutcomeProjection is the frozen, already privacy-filtered content
// awards produces. This module renders it and adds nothing: it holds no
// competitor identity, price, total or unawarded reason to begin with (§8H).
type SupplierOutcomeProjection struct {
	Result    string
	RFQNumber string
	RFQTitle  string

	AwardedLines []SupplierOutcomeLine

	LineSubtotalMinorUnits   int64
	TaxTotalMinorUnits       int64
	ChargeTotalMinorUnits    int64
	DeliveryChargeMinorUnits int64
	AwardTotalMinorUnits     int64
	Currency                 string

	ContractorMessage string
	NextSteps         string
}

type SupplierOutcomeLine struct {
	IssuedRFQLineID        string
	MaterialName           string
	QuantityValue          string
	QuantityUnit           string
	UnitPriceMinorUnits    int64
	LineSubtotalMinorUnits int64
	LineTaxMinorUnits      int64
	Brand                  string
	SKU                    string
	LeadTime               string
}

type SupplierOutcome struct {
	ID         string
	Result     string
	Projection SupplierOutcomeProjection
	CreatedAt  time.Time
}

// SupplierOutcomeAcknowledgementResult reports whether THIS request recorded
// the receipt, so the route can answer 201 rather than 200 only when it did.
type SupplierOutcomeAcknowledgementResult struct {
	OutcomeID      string
	AcknowledgedAt time.Time
	Created        bool
}

// SupplierOutcomeRequest carries only AUTHORITATIVE session facts, all resolved
// by this module from the validated Phase D session. Nothing here comes from
// request content, so a Supplier cannot name another Supplier's outcome and
// have it honoured.
type SupplierOutcomeRequest struct {
	CompanyID         string
	SupplierID        string
	InvitationID      string
	SessionID         string
	RecipientIdentity string
	AccessGeneration  int64
	OutcomeID         string
	OperationID       string
	OccurredAt        time.Time
}

// SupplierOutcomeSource is this module's consumer-owned capability over awards.
//
// awards enforces the matching Supplier and Invitation and returns a
// non-disclosing not-found for a foreign outcome; this module never inspects
// award state itself.
type SupplierOutcomeSource interface {
	GetSupplierOutcome(
		ctx context.Context,
		request SupplierOutcomeRequest,
	) (SupplierOutcome, bool, error)
	AcknowledgeSupplierOutcome(
		ctx context.Context,
		request SupplierOutcomeRequest,
	) (SupplierOutcomeAcknowledgementResult, error)
}

// SetSupplierOutcomeSource closes the Phase D/F cycle.
//
// It is a setter for the same reason the M7/M8 issuance cycle is: awards is
// constructed after supplieraccess, because awards depends on capabilities this
// module's siblings provide. Without this call the outcome routes report a
// bounded unavailable rather than silently returning nothing.
func (service *Service) SetSupplierOutcomeSource(source SupplierOutcomeSource) {
	service.outcomes = source
}

// GetOutcomeForSession reads an outcome through a validated Phase D session.
//
// Every identity passed downstream comes from the session, never the request.
func (service *Service) GetOutcomeForSession(
	ctx context.Context,
	access AuthorizedInvitationAccess,
	outcomeID string,
) (SupplierOutcome, error) {
	if service.outcomes == nil {
		return SupplierOutcome{}, ErrSupplierOutcomesUnavailable
	}

	outcome, found, err := service.outcomes.GetSupplierOutcome(ctx,
		SupplierOutcomeRequest{
			CompanyID:         access.CompanyID,
			SupplierID:        access.SupplierID,
			InvitationID:      access.InvitationID,
			SessionID:         access.SessionID,
			RecipientIdentity: access.NormalizedRecipientEmail,
			AccessGeneration:  access.AccessGeneration,
			OutcomeID:         outcomeID,
			OccurredAt:        time.Now().UTC(),
		})
	if err != nil {
		return SupplierOutcome{}, err
	}
	if !found {
		// The same non-disclosing not-found Phase D uses throughout: another
		// Supplier's outcome is indistinguishable from one that does not exist.
		return SupplierOutcome{}, ErrSupplierOutcomeNotFound
	}
	return outcome, nil
}

// AcknowledgeOutcomeForSession records a receipt through a validated session.
//
// A replacement recipient MAY acknowledge; the record captures the identity
// that actually did so, which is the honest fact (§8J).
func (service *Service) AcknowledgeOutcomeForSession(
	ctx context.Context,
	access AuthorizedInvitationAccess,
	outcomeID string,
	operationID string,
) (SupplierOutcomeAcknowledgementResult, error) {
	if service.outcomes == nil {
		return SupplierOutcomeAcknowledgementResult{},
			ErrSupplierOutcomesUnavailable
	}

	return service.outcomes.AcknowledgeSupplierOutcome(ctx,
		SupplierOutcomeRequest{
			CompanyID:         access.CompanyID,
			SupplierID:        access.SupplierID,
			InvitationID:      access.InvitationID,
			SessionID:         access.SessionID,
			RecipientIdentity: access.NormalizedRecipientEmail,
			AccessGeneration:  access.AccessGeneration,
			OutcomeID:         outcomeID,
			OperationID:       operationID,
			OccurredAt:        time.Now().UTC(),
		})
}
