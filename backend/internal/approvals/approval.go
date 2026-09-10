package approvals

import "time"

// ApprovalStatus is the current decision for one subject.
type ApprovalStatus string

const (
	ApprovalStatusAccepted         ApprovalStatus = "accepted"
	ApprovalStatusRejected         ApprovalStatus = "rejected"
	ApprovalStatusChangesRequested ApprovalStatus = "changes_requested"
)

// IsValid reports whether s is one of the 3 defined statuses.
func (s ApprovalStatus) IsValid() bool {
	switch s {
	case ApprovalStatusAccepted, ApprovalStatusRejected, ApprovalStatusChangesRequested:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether s locks the subject against further decisions.
// Only acceptance is terminal (phase1.md §31: "Quotation Version Locked"
// happens at acceptance, not at rejection).
func (s ApprovalStatus) IsTerminal() bool {
	return s == ApprovalStatusAccepted
}

// SubjectType values. M6 implements only "quotation".
const SubjectTypeQuotation = "quotation"

// ActorType values. M6 implements only "client".
const ActorTypeClient = "client"

// Approval is the single, mutable-in-place aggregate representing the
// CURRENT decision for one exact subject. Exactly one document exists per
// {CompanyID, SubjectType, SubjectID}. The append-only history of every
// submitted decision lives in audit_events, not here.
type Approval struct {
	ID              string
	CompanyID       string
	SubjectType     string
	SubjectID       string
	SubjectGroupKey string

	ActorType  string
	ActorName  string
	ActorEmail string

	Status        ApprovalStatus
	Comment       string
	AccessGrantID string

	Revision      int64
	DecidedAt     time.Time
	SchemaVersion int
}
