package approvals

import (
	"context"
	"errors"
)

// ErrApprovalNotFound is returned when no Approval exists for a subject.
var ErrApprovalNotFound = errors.New("approvals: approval not found")

// ErrApprovalAlreadyExists is returned by Create when a document already
// exists for {companyId, subjectType, subjectId} — the uq_approvals_subject
// collision. The service resolves it by re-reading and applying the
// transition rules instead (never a caller-visible error).
var ErrApprovalAlreadyExists = errors.New("approvals: approval already exists for this subject")

// ErrApprovalRevisionMismatch is returned when a Revision-guarded update
// does not match.
var ErrApprovalRevisionMismatch = errors.New("approvals: approval revision mismatch")

// ErrDecisionAlreadyAccepted is returned when a reject/request-changes is
// attempted against an already-accepted subject. Acceptance is terminal
// (phase1.md §31).
var ErrDecisionAlreadyAccepted = errors.New("approvals: subject has already been accepted")

// ErrInvalidApprovalStatus is returned for a status outside the 3 defined values.
var ErrInvalidApprovalStatus = errors.New("approvals: invalid approval status")

// ErrUnclassifiedDuplicateKey is returned for a duplicate-key error that does
// not match a known index by name. NEVER retried.
var ErrUnclassifiedDuplicateKey = errors.New("approvals: unclassified duplicate-key error")

// ApprovalRepository persists Approvals. approvals owns the approvals
// collection exclusively.
type ApprovalRepository interface {
	// Create inserts the first Approval for a subject. Returns
	// ErrApprovalAlreadyExists on the uq_approvals_subject collision.
	Create(ctx context.Context, a Approval) (Approval, error)

	FindBySubject(ctx context.Context, companyID, subjectType, subjectID string) (Approval, error)

	// UpdateDecision conditionally replaces the decision fields, matching
	// {companyId, subjectType, subjectId, revision: expectedRevision}.
	UpdateDecision(ctx context.Context, companyID, subjectType, subjectID string, expectedRevision int64, updated Approval) (Approval, error)
}
