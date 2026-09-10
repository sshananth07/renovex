package access

import (
	"context"
	"errors"
	"time"
)

// --- Domain error sentinels (design spec §15) ---

// ErrGrantNotFound is returned when a grant lookup finds no match — including
// a grant that exists but belongs to a different company.
var ErrGrantNotFound = errors.New("access: access grant not found")

// ErrGroupStateNotFound is returned when a coordinator lookup finds no match.
// A grant must never exist without its coordinator; encountering this from a
// grant-driven path indicates a prior bug, not a normal user-facing case.
var ErrGroupStateNotFound = errors.New("access: access group state not found")

// ErrGrantRevisionMismatch is returned when a Revision-guarded single-grant
// mutation (revoke, expiry edit) does not match an eligible document.
var ErrGrantRevisionMismatch = errors.New("access: grant revision mismatch or grant is not in the required state")

// ErrGroupStateRevisionMismatch is returned when a coordinator conditional
// claim does not match. Every cross-grant operation (share, supersession,
// rotation, client decision) funnels through this (design spec §2.4/§7.2/§7.3).
var ErrGroupStateRevisionMismatch = errors.New("access: access group state changed since it was read")

// ErrUnclassifiedDuplicateKey is returned when MongoDB reports a duplicate-key
// error that does not match a known index by name. NEVER retried.
var ErrUnclassifiedDuplicateKey = errors.New("access: unclassified duplicate-key error")

// --- Repository interfaces ---

// AccessGrantRepository persists AccessGrants. access owns the access_grants
// collection exclusively; no other module may query it directly.
type AccessGrantRepository interface {
	// Create inserts a candidate grant. Per design spec §2.4 the candidate is
	// inserted BEFORE the coordinator claim, and is only ever disclosed after
	// that claim succeeds — an unclaimed candidate is an undisclosed orphan.
	Create(ctx context.Context, g AccessGrant) (AccessGrant, error)

	FindByID(ctx context.Context, companyID, id string) (AccessGrant, error)

	// FindByTokenHash resolves a raw token's hash to its grant WITHOUT a
	// companyID — the external Client path has no authenticated tenant. The
	// grant's own stored CompanyID is what every downstream lookup then uses,
	// never a caller-supplied value (design spec §14).
	FindByTokenHash(ctx context.Context, tokenHash string) (AccessGrant, error)

	// FindLatestByResource returns the most recently created grant for one
	// exact Quotation version, or ErrGrantNotFound.
	FindLatestByResource(ctx context.Context, companyID, resourceType, resourceID string) (AccessGrant, error)

	// Revoke conditionally marks a grant revoked, matching
	// {_id, companyId, status: active, revision: expectedRevision}.
	// Returns ErrGrantRevisionMismatch when no eligible document matches.
	Revoke(ctx context.Context, companyID, id string, expectedRevision int64, reason string, revokedAt time.Time) (AccessGrant, error)

	// RevokeBestEffort marks a grant revoked without a Revision guard, used
	// for the post-claim cleanup in §2.4 step 4. Returns the updated grant, or
	// ErrGrantRevisionMismatch if the grant was not stored-active. Its failure
	// is never authoritative — the coordinator has already switched.
	RevokeBestEffort(ctx context.Context, companyID, id, reason string, revokedAt time.Time) (AccessGrant, error)

	// UpdateExpiry conditionally replaces ExpiresAt on an active grant,
	// matching {_id, companyId, status: active, revision: expectedRevision}.
	UpdateExpiry(ctx context.Context, companyID, id string, expectedRevision int64, expiresAt time.Time) (AccessGrant, error)
}

// AccessGroupStateRepository persists the per-chain coordinator documents.
// Every conditional method here is the single serialization point for one
// class of race (design spec §2.5/§11).
type AccessGroupStateRepository interface {
	// FindOrCreate atomically returns the existing coordinator for a chain, or
	// creates one seeded with initialResourceID and no active/accepted grant.
	FindOrCreate(ctx context.Context, companyID, resourceType, resourceGroupKey, initialResourceID string, now time.Time) (AccessGroupState, error)

	Find(ctx context.Context, companyID, resourceType, resourceGroupKey string) (AccessGroupState, error)

	// ClaimActiveGrant conditionally activates candidateGrantID. It matches
	// {companyId, resourceType, resourceGroupKey, revision: expectedRevision,
	//  currentResourceId: expectedCurrentResourceID,
	//  activeGrantId: expectedActiveGrantID}
	// and additionally requires acceptedResourceId to satisfy the caller's
	// acceptance condition:
	//   - requireAcceptedNil == true  -> acceptedResourceId must be nil
	//   - requireAcceptedNil == false -> acceptedResourceId must be nil OR
	//     equal to newCurrentResourceID (rotation of the accepted version's
	//     own grant, design spec §2.4)
	// On success it sets currentResourceId=newCurrentResourceID,
	// activeGrantId=candidateGrantID, revision=revision+1.
	ClaimActiveGrant(
		ctx context.Context,
		companyID, resourceType, resourceGroupKey string,
		expectedRevision int64,
		expectedCurrentResourceID string,
		expectedActiveGrantID *string,
		newCurrentResourceID string,
		candidateGrantID string,
		requireAcceptedNil bool,
	) (AccessGroupState, error)

	// ClearActiveGrant conditionally sets activeGrantId=nil, matching
	// {companyId, resourceType, resourceGroupKey, activeGrantId: expectedActiveGrantID}.
	// Used by the manual-revoke cleanup path (design spec §2.6).
	ClearActiveGrant(ctx context.Context, companyID, resourceType, resourceGroupKey, expectedActiveGrantID string) (AccessGroupState, error)

	// ClaimAcceptance conditionally sets acceptedResourceId=resourceID, matching
	// {companyId, resourceType, resourceGroupKey, revision: expectedRevision,
	//  currentResourceId: resourceID, activeGrantId: grantID,
	//  acceptedResourceId: nil}.
	// This is the acceptance fence: it runs BEFORE any Approval write, so no
	// backward compensation is ever needed (design spec §7.2).
	ClaimAcceptance(ctx context.Context, companyID, resourceType, resourceGroupKey string, expectedRevision int64, resourceID, grantID string) (AccessGroupState, error)

	// FenceNonTerminalDecision conditionally bumps revision only, matching
	// {companyId, resourceType, resourceGroupKey, revision: expectedRevision,
	//  currentResourceId: resourceID, activeGrantId: grantID,
	//  acceptedResourceId: nil}.
	// Serializes reject/request-changes against concurrent supersession and
	// acceptance (design spec §7.3).
	FenceNonTerminalDecision(ctx context.Context, companyID, resourceType, resourceGroupKey string, expectedRevision int64, resourceID, grantID string) (AccessGroupState, error)
}
