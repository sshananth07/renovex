package demoseed

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
)

// AwardDriver is the capability demoseed needs from awards. GetComparison
// is the SAME real method the frontend's Supplier-offer comparison view
// uses — it reads offer version lines session-independently, unlike
// supplieroffers.Service's Supplier-session-gated read methods.
//
// GetCurrentAward's signature matches *awards.Service exactly: it takes
// issuedRFQVersionID (not an awardChainID) and returns a plain error —
// ErrAwardRevisionNotFound when no award has been finalised yet — rather
// than a found bool.
type AwardDriver interface {
	CreateAwardDraft(ctx context.Context, companyID, actorUserID, issuedRFQVersionID string) (awards.AwardDraft, error)
	SelectAwardLine(ctx context.Context, companyID, actorUserID string, input awards.SelectAwardLineInput) (awards.AwardDraft, error)
	FinaliseAward(ctx context.Context, companyID, actorUserID string, input awards.FinaliseAwardInput) (awards.AwardRevision, error)
	GetCurrentAward(ctx context.Context, companyID, issuedRFQVersionID string) (awards.AwardRevision, error)
	GetComparison(ctx context.Context, companyID, issuedRFQVersionID string, observedAt time.Time) (awards.Comparison, error)
}
