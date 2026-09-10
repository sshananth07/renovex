package awards

import (
	"strings"
	"time"
)

// Supplier acknowledgement (§8J).
//
// Acknowledgement confirms RECEIPT ONLY. It is NOT acceptance of a purchase
// order, contractual acceptance, confirmation of delivery, invoice approval,
// consent to the award, or any precondition for the award being authoritative.
//
// Therefore an inability to acknowledge — expired or revoked credentials —
// never blocks award publication, recovery, outcome creation, other Suppliers'
// notifications, or later procurement stages.

const AwardOutcomeAcknowledgementSchemaVersion = 1

// AcknowledgementMeaning is the fixed wording the API and interface must state.
//
// It lives here rather than in a template so no caller can soften it into
// something a Supplier could reasonably read as acceptance.
const AcknowledgementMeaning = "Acknowledgement confirms receipt only. " +
	"It is not acceptance of a Purchase Order or contractual commitment."

// AwardOutcomeAcknowledgement is the first receipt, and the only one.
//
// RecipientIdentity captures who ACTUALLY acknowledged, which may be a
// replacement recipient rather than whoever submitted the offer. That is the
// honest fact and is what makes the receipt meaningful later.
type AwardOutcomeAcknowledgement struct {
	ID                string
	CompanyID         string
	AwardOutcomeID    string
	SupplierID        string
	InvitationID      string
	SessionID         string
	RecipientIdentity string
	OperationID       string
	AcknowledgedAt    time.Time
	SchemaVersion     int
}

func (acknowledgement AwardOutcomeAcknowledgement) Validate() error {
	if strings.TrimSpace(acknowledgement.CompanyID) == "" ||
		strings.TrimSpace(acknowledgement.AwardOutcomeID) == "" ||
		strings.TrimSpace(acknowledgement.SupplierID) == "" ||
		strings.TrimSpace(acknowledgement.InvitationID) == "" ||
		strings.TrimSpace(acknowledgement.SessionID) == "" ||
		strings.TrimSpace(acknowledgement.RecipientIdentity) == "" ||
		strings.TrimSpace(acknowledgement.OperationID) == "" {
		return ErrInvalidAcknowledgement
	}
	// A receipt with no time records nothing useful: its whole value is that it
	// happened at a knowable moment.
	if acknowledgement.AcknowledgedAt.IsZero() {
		return ErrInvalidAcknowledgement
	}
	return nil
}

// timeZero is a readable stand-in for the zero time in tests and guards.
func timeZero() time.Time { return time.Time{} }
