// Package composition holds composition-root adapters: the small conversion
// shims that let one domain module satisfy another's consumer-defined
// capability interface when the two disagree on a return TYPE.
//
// This is the one place permitted to import two domain modules at once. Go
// interface satisfaction requires exact method signatures, so a provider
// whose natural return type differs from the consumer's declared type cannot
// satisfy it structurally — without a shim, the consumer would have to import
// the provider's types (or vice versa), which ADR 0002 forbids. Keeping the
// shims here, at the leaf of the dependency graph, means no domain module
// gains a dependency it should not have.
//
// Adapters here are deliberately thin: field mapping and error translation
// only, never business logic.
package composition

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/shananth/renovation-platform/backend/internal/access"
	"github.com/shananth/renovation-platform/backend/internal/approvals"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
)

// --- quotations -> access.QuotationSource ---

// QuotationSourceAdapter converts quotations.Quotation into the
// access-owned ShareableQuotationSnapshot. The conversion is the privacy
// boundary in code: it copies ONLY the allowlisted fields, so EstimateID,
// GeneratedSubtotal, Notes, Revision, SourceWorkItemIDs and the internal line
// IDs never leave the quotations module (M6 design spec §14).
type QuotationSourceAdapter struct {
	quotations *quotations.Service
}

// NewQuotationSourceAdapter wraps a quotations.Service.
func NewQuotationSourceAdapter(svc *quotations.Service) *QuotationSourceAdapter {
	return &QuotationSourceAdapter{quotations: svc}
}

func (a *QuotationSourceAdapter) GetFinalizedQuotationForShare(ctx context.Context, companyID, quotationID string) (access.ShareableQuotationSnapshot, bool, error) {
	q, err := a.quotations.GetQuotation(ctx, companyID, quotationID)
	if err == quotations.ErrQuotationNotFound {
		return access.ShareableQuotationSnapshot{}, false, nil
	}
	if err != nil {
		return access.ShareableQuotationSnapshot{}, false, err
	}

	lines := make([]access.ShareableQuotationLine, len(q.Lines))
	for i, l := range q.Lines {
		// Note the deliberate omissions: l.ID and l.SourceWorkItemIDs are NOT
		// copied — the target type has no field for either.
		lines[i] = access.ShareableQuotationLine{
			Description: l.Description,
			Quantity:    l.Quantity,
			Unit:        l.Unit,
			UnitPrice:   l.UnitPrice,
			Amount:      l.Amount,
		}
	}

	return access.ShareableQuotationSnapshot{
		QuotationID: q.ID, CompanyID: q.CompanyID, ProjectID: q.ProjectID,
		ClientID: q.ClientID, QuotationNumber: q.QuotationNumber, Version: q.Version,
		Status: string(q.Status), Currency: q.Currency, Lines: lines,
		Subtotal: q.Subtotal, TaxMode: string(q.TaxMode), TaxLabel: q.TaxLabel,
		TaxRateBPS: q.TaxRateBPS, TaxAmount: q.TaxAmount, Total: q.Total,
		Terms: q.Terms, PaymentSchedule: q.PaymentSchedule, ValidUntil: q.ValidUntil,
	}, true, nil
}

// --- approvals -> access.DecisionRecorder ---

// ApprovalsAdapter flattens approvals.Approval into the primitive tuples
// access declares, so neither module imports the other.
type ApprovalsAdapter struct {
	approvals *approvals.Service
}

// NewApprovalsAdapter wraps an approvals.Service.
func NewApprovalsAdapter(svc *approvals.Service) *ApprovalsAdapter {
	return &ApprovalsAdapter{approvals: svc}
}

func (a *ApprovalsAdapter) RecordDecision(ctx context.Context, companyID, subjectType, subjectID, subjectGroupKey, actorName, actorEmail, comment, status, accessGrantID string) (string, time.Time, bool, error) {
	approval, isNew, err := a.approvals.RecordDecision(ctx, approvals.RecordDecisionInput{
		CompanyID: companyID, SubjectType: subjectType, SubjectID: subjectID,
		SubjectGroupKey: subjectGroupKey, ActorName: actorName, ActorEmail: actorEmail,
		Comment: comment, Status: approvals.ApprovalStatus(status), AccessGrantID: accessGrantID,
	})
	if err == approvals.ErrDecisionAlreadyAccepted {
		// Translate the provider's sentinel into the consumer's own.
		return "", time.Time{}, false, access.ErrDecisionAlreadyAccepted
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	return approval.ID, approval.DecidedAt, isNew, nil
}

func (a *ApprovalsAdapter) ReconcileAccepted(ctx context.Context, companyID, subjectType, subjectID, subjectGroupKey, actorName, actorEmail, comment, accessGrantID string) (string, time.Time, error) {
	approval, err := a.approvals.ReconcileAccepted(ctx, approvals.RecordDecisionInput{
		CompanyID: companyID, SubjectType: subjectType, SubjectID: subjectID,
		SubjectGroupKey: subjectGroupKey, ActorName: actorName, ActorEmail: actorEmail,
		Comment: comment, AccessGrantID: accessGrantID,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return approval.ID, approval.DecidedAt, nil
}

func (a *ApprovalsAdapter) GetDecision(ctx context.Context, companyID, subjectType, subjectID string) (string, string, string, string, time.Time, bool, error) {
	approval, found, err := a.approvals.GetDecision(ctx, companyID, subjectType, subjectID)
	if err != nil || !found {
		return "", "", "", "", time.Time{}, false, err
	}
	return string(approval.Status), approval.Comment, approval.ActorName,
		approval.ActorEmail, approval.DecidedAt, true, nil
}

// --- cleanup logging ---

// CleanupLogger satisfies access.CleanupLogger using the platform's zerolog
// logger. Best-effort cleanup failures are recorded at WARN with enough
// context to reconcile a stored/effective-state mismatch later, and never
// carry a raw token — the interface has no parameter capable of holding one.
type CleanupLogger struct {
	logger zerolog.Logger
}

// NewCleanupLogger wraps a zerolog logger.
func NewCleanupLogger(logger zerolog.Logger) *CleanupLogger {
	return &CleanupLogger{logger: logger}
}

func (c *CleanupLogger) LogCleanupFailure(_ context.Context, operation, companyID, grantID string, err error) {
	c.logger.Warn().
		Str("operation", operation).
		Str("companyId", companyID).
		Str("grantId", grantID).
		Err(err).
		Msg("m6 best-effort cleanup failed; coordinator state remains authoritative")
}
