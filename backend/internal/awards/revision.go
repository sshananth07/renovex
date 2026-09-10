package awards

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

const (
	AwardRevisionSchemaVersion = 1

	MaxChangeReasonLength = 500
)

// AwardRevision is the immutable authoritative award record (§8F).
//
// Its INSERT is the publication point (D2), mirroring Phase E submission.
// Prior revisions are never mutated: the record of what was decided, and
// communicated to Suppliers, must survive being changed.
type AwardRevision struct {
	ID                      string
	CompanyID               string
	AwardChainID            string
	RFQChainID              string
	IssuedRFQVersionID      string
	RevisionNumber          int
	FinalisationOperationID string
	SelectionFingerprint    string

	// ChangeReason is required from revision 2 onward. A superseding record
	// with no stated reason would leave the change unexplainable later.
	ChangeReason string

	AwardedLines      []AwardedLine
	UnawardedLines    []UnawardedLine
	SupplierSummaries []AwardSupplierSummary
	GrandAwardTotal   money.Money

	SupersedesRevisionID *string
	FinalisedByUserID    string
	FinalisedAt          time.Time
	SchemaVersion        int
}

// Validate enforces revision shape before it can be persisted.
func (revision AwardRevision) Validate() error {
	if strings.TrimSpace(revision.CompanyID) == "" ||
		strings.TrimSpace(revision.AwardChainID) == "" ||
		strings.TrimSpace(revision.RFQChainID) == "" ||
		strings.TrimSpace(revision.IssuedRFQVersionID) == "" ||
		strings.TrimSpace(revision.FinalisationOperationID) == "" ||
		strings.TrimSpace(revision.SelectionFingerprint) == "" ||
		revision.RevisionNumber < 1 {
		return ErrInvalidAwardRevision
	}

	reason := strings.TrimSpace(revision.ChangeReason)
	if revision.RevisionNumber > 1 {
		// A correction that cannot say why it happened is not auditable.
		if reason == "" {
			return ErrChangeReasonRequired
		}
		if revision.SupersedesRevisionID == nil ||
			strings.TrimSpace(*revision.SupersedesRevisionID) == "" {
			return ErrInvalidAwardRevision
		}
	}
	if utf8.RuneCountInString(reason) > MaxChangeReasonLength {
		return ErrChangeReasonRequired
	}
	return nil
}
