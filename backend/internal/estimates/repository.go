package estimates

import (
	"context"
	"errors"
	"time"
)

// ErrEstimateNotFound is returned when an Estimate lookup finds no match —
// including an Estimate that exists but belongs to a different company.
var ErrEstimateNotFound = errors.New("estimates: estimate not found")

// ErrNoEstimatesForProject is returned by FindLatestByProject when a Project
// has no Estimate versions at all.
var ErrNoEstimatesForProject = errors.New("estimates: no estimates exist for this project")

// ErrVersionConflict is returned by Create when the attempted Version
// number collides with the {companyId, projectId, version} unique index —
// i.e. another concurrent writer already claimed that exact version
// number. This is a TRANSIENT condition the caller should resolve by
// re-reading MAX(version) and retrying with a fresh version number
// (design spec §21.2 steps 1-3/5-6) — distinct from ErrDraftAlreadyExists
// below, which is NOT transient in the same way.
var ErrVersionConflict = errors.New("estimates: version number conflict, retry with a fresh version")

// ErrDraftAlreadyExists is returned by Create when the attempted document
// (status=draft) collides with the {companyId, projectId} PARTIAL unique
// index (design spec §20, §32-3) — i.e. a draft already exists for this
// Project. This is NOT a transient condition to blindly retry: retrying
// "create a new draft" when one already exists will fail again forever.
// The caller's correct response is to surface a 409 pointing at the
// existing draft, not to loop (design spec §21.2 step 4).
var ErrDraftAlreadyExists = errors.New("estimates: a draft already exists for this project")

// ErrUnclassifiedDuplicateKey is returned by Create when MongoDB reports a
// duplicate-key error that does not match either known unique index by
// name (design spec §20-21, added in the third review round). Unlike
// ErrVersionConflict, this is deliberately NOT retried anywhere — an error
// this function cannot positively identify must not be assumed safe to
// retry; it surfaces as an ordinary internal error (500) instead.
var ErrUnclassifiedDuplicateKey = errors.New("estimates: unclassified duplicate-key error")

// EstimateRepository persists Estimates. estimates owns the estimates
// collection exclusively; no other module may query it directly.
type EstimateRepository interface {
	Create(ctx context.Context, e Estimate) (Estimate, error)
	FindByID(ctx context.Context, companyID, id string) (Estimate, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]Estimate, error)
	FindLatestByProject(ctx context.Context, companyID, projectID string) (Estimate, error)
	FindMaxVersion(ctx context.Context, companyID, projectID string) (int, error)

	// ReplaceSnapshot atomically replaces Lines/CostSubtotal/
	// ExcludedCostItemCount and recomputed pricing outputs on a draft,
	// conditioned on {_id, companyId, status: draft, revision: expectedRevision}
	// matching. Returns ErrRevisionMismatch if the condition does not match
	// an existing draft document (design spec §16.2).
	ReplaceSnapshot(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error)

	// UpdatePricing atomically replaces PricingMode/PricingRate and
	// recomputed pricing outputs on a draft, same conditional-match
	// semantics as ReplaceSnapshot (design spec §16.1).
	UpdatePricing(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error)

	// Finalize atomically transitions a draft to finalized, conditioned on
	// {_id, companyId, status: draft, revision: expectedRevision} matching.
	// Returns ErrRevisionMismatch on a stale expectedRevision against a
	// still-draft document (design spec §16.3).
	Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Estimate, error)
}

// ErrRevisionMismatch is returned by ReplaceSnapshot/UpdatePricing/Finalize
// when the supplied expectedRevision does not match the document's current
// Revision (or the document is no longer a draft) — the caller must re-GET
// and retry with the current Revision (design spec §16.1-16.3).
var ErrRevisionMismatch = errors.New("estimates: revision mismatch or estimate is no longer a draft")
