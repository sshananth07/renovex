package demoseed

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
)

type ResetResult struct {
	CompanyID string
	WasNoOp   bool
}

type CleanupRepository interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

type UserDeleter interface {
	DeleteUser(ctx context.Context, id string) error
}

type SessionDeleter interface {
	DeleteAllSessionsForUser(ctx context.Context, userID string) error
}

type MembershipDeleter interface {
	DeleteMembershipByUserID(ctx context.Context, userID string) error
}

type CompanyDeleter interface {
	DeleteCompany(ctx context.Context, id string) error
}

var ErrDemoTenantAmbiguous = fmt.Errorf("demoseed: cannot positively identify the demo tenant to reset")

// Reset is the real entrypoint cmd/demoseed/main.go calls.
func Reset(ctx context.Context, services *composition.Services, db *mongo.Database) (ResetResult, error) {
	manifests := NewManifestStore(db)
	if err := manifests.EnsureIndexes(ctx); err != nil {
		return ResetResult{}, err
	}
	cleanupRepos := cleanupRepositoriesInOrder(services)
	return ResetWithDependencies(ctx, manifests, services.Users, services.Companies, services.Companies,
		services.Users, services.Auth, services.Companies, services.Companies, cleanupRepos)
}

// ResetWithDependencies implements the reset-side manifest state machine
// (design spec §6.2, terminal-case table §10):
//
//	manifest absent + demo User ABSENT + demo Company name ABSENT
//	    -> already reset, no-op success
//	manifest absent + (demo User exists OR demo Company name exists)
//	    -> refuse (ErrDemoTenantAmbiguous) — BOTH checked, not just User
//	manifest state=provisioning -> refuse (an interrupted seed)
//	manifest state=resetting -> resume from the manifest's stored anchor IDs,
//	    tolerating "already deleted" at every teardown step
//	manifest state=ready -> verify via the SAME verifyDemoIdentity Seed
//	    uses, transition to resetting, then run the full teardown:
//	    cleanup repositories -> AuthSessions -> Membership -> User ->
//	    Company -> manifest (LAST)
func ResetWithDependencies(
	ctx context.Context,
	manifests *ManifestStore,
	users UserLookup,
	memberships MembershipLookup,
	companyLookup CompanyLookup,
	userDeleter UserDeleter,
	sessionDeleter SessionDeleter,
	membershipDeleter MembershipDeleter,
	companyDeleter CompanyDeleter,
	cleanupRepos []CleanupRepository,
) (ResetResult, error) {
	manifest, found, err := manifests.Get(ctx)
	if err != nil {
		return ResetResult{}, err
	}

	if !found {
		// BOTH legs checked — either one existing without a manifest is
		// ambiguous, not a no-op. Fail CLOSED on lookup errors here too —
		// only a confirmed "not found" sentinel proves the User/Company
		// genuinely doesn't exist; any other error must abort the whole
		// reset, not be silently folded into "doesn't exist, safe to
		// no-op."
		userExists := false
		if users != nil {
			_, err := users.FindUserByEmail(ctx, demoEmail)
			switch {
			case err == nil:
				userExists = true
			case errors.Is(err, identity.ErrUserNotFound):
				userExists = false
			default:
				return ResetResult{}, fmt.Errorf("demoseed: look up demo user by email: %w", err)
			}
		}
		companyNameExists := false
		if companyLookup != nil {
			_, err := companyLookup.FindCompanyByName(ctx, demoCompanyName)
			switch {
			case err == nil:
				companyNameExists = true
			case errors.Is(err, companies.ErrCompanyNotFound):
				companyNameExists = false
			default:
				return ResetResult{}, fmt.Errorf("demoseed: look up demo company by name: %w", err)
			}
		}
		if userExists || companyNameExists {
			return ResetResult{}, fmt.Errorf("%w: the demo email or demo company name resolves to a record but no manifest exists to prove this tool provisioned it", ErrDemoTenantAmbiguous)
		}
		return ResetResult{WasNoOp: true}, nil
	}

	var ownerToken string
	switch manifest.State {
	case ManifestStateProvisioning:
		return ResetResult{}, fmt.Errorf("demoseed: manifest state=provisioning (an interrupted seed) — run seed to finish or repair it before reset")

	case ManifestStateReady:
		if err := verifyDemoIdentity(ctx, users, memberships, companyLookup, manifest.DemoUserID, manifest.DemoCompanyID); err != nil {
			return ResetResult{}, err
		}
		token, err := newLeaseOwnerToken()
		if err != nil {
			return ResetResult{}, err
		}
		if _, err := manifests.AcquireLease(ctx, token, leaseDuration); err != nil {
			return ResetResult{}, err
		}
		if _, err := manifests.BeginResetting(ctx, token); err != nil {
			return ResetResult{}, err
		}
		ownerToken = token

	case ManifestStateResetting:
		// Resume: reclaim the lease under a fresh token (the crashed
		// process's lease will have expired, or this genuinely is a retry
		// within the same process) and continue using the manifest's
		// STORED anchor IDs — never re-derive identity.
		token, err := newLeaseOwnerToken()
		if err != nil {
			return ResetResult{}, err
		}
		if _, err := manifests.AcquireLease(ctx, token, leaseDuration); err != nil {
			return ResetResult{}, err
		}
		if _, err := manifests.BeginResetting(ctx, token); err != nil {
			return ResetResult{}, err
		}
		ownerToken = token

	default:
		return ResetResult{}, fmt.Errorf("demoseed: manifest is in an unrecognized state %q", manifest.State)
	}

	// Seed already defers ReleaseLease on every exit path; Reset does the
	// same here — a failed deleteAndFinish must not leave the lease held
	// until it expires naturally, needlessly blocking a retry for up to
	// leaseDuration. And a long teardown (many CleanupRepository calls
	// across ~20 modules against real Mongo) needs the same heartbeat Seed
	// uses, or its own lease can be reclaimed out from under it
	// mid-teardown. ReleaseLease on an already-deleted manifest (the
	// success path deletes the manifest itself) is already documented as a
	// safe no-op, so this defer is unconditionally correct on every exit
	// path, success or failure.
	defer func() {
		_ = manifests.ReleaseLease(ctx, ownerToken)
	}()
	stopHeartbeat, _ := manifests.StartLeaseHeartbeat(ctx, ownerToken, leaseDuration)
	defer stopHeartbeat()

	return deleteAndFinish(ctx, manifests, ownerToken, cleanupRepos,
		userDeleter, sessionDeleter, membershipDeleter, companyDeleter,
		manifest.DemoCompanyID, manifest.DemoUserID)
}

// deleteAndFinish runs the FULL resumable teardown sequence: cleanup
// repositories (company-scoped, each internally tolerant of "nothing
// left") -> AuthSessions -> Membership -> User -> Company -> manifest
// LAST. Every single-document delete step treats its own "already deleted"
// sentinel as success, so this function is safe to call again after a
// crash at any point within it.
func deleteAndFinish(
	ctx context.Context,
	manifests *ManifestStore,
	ownerToken string,
	cleanupRepos []CleanupRepository,
	userDeleter UserDeleter,
	sessionDeleter SessionDeleter,
	membershipDeleter MembershipDeleter,
	companyDeleter CompanyDeleter,
	companyID, userID string,
) (ResetResult, error) {
	for _, repo := range cleanupRepos {
		if err := repo.DeleteAllForCompany(ctx, companyID); err != nil {
			return ResetResult{}, err
		}
	}

	if err := sessionDeleter.DeleteAllSessionsForUser(ctx, userID); err != nil {
		return ResetResult{}, err
	}

	if err := membershipDeleter.DeleteMembershipByUserID(ctx, userID); err != nil && !errors.Is(err, companies.ErrMembershipNotFound) {
		return ResetResult{}, err
	}

	if err := userDeleter.DeleteUser(ctx, userID); err != nil && !errors.Is(err, identity.ErrUserNotFound) {
		return ResetResult{}, err
	}

	if err := companyDeleter.DeleteCompany(ctx, companyID); err != nil && !errors.Is(err, companies.ErrCompanyNotFound) {
		return ResetResult{}, err
	}

	if err := manifests.Delete(ctx, ownerToken); err != nil {
		return ResetResult{}, err
	}

	return ResetResult{CompanyID: companyID}, nil
}

// cleanupRepositoriesInOrder returns every module's Service (each now
// satisfying CleanupRepository via its DeleteAllForCompany method, Task
// 1a) in dependency-safe order — deepest first, matching design spec §6.5.
// Company, Membership, User, and the manifest itself are deleted
// separately in deleteAndFinish, not through this list, since they use
// their own dedicated single-document delete methods.
func cleanupRepositoriesInOrder(services *composition.Services) []CleanupRepository {
	return []CleanupRepository{
		// Award records
		services.Awards,
		// Supplier Offer records
		services.SupplierOffers,
		// Supplier Access records
		services.SupplierAccess,
		// RFQ issuance records
		services.RFQIssuance,
		// RFQs and Material Requirements
		services.RFQs, services.MaterialRequirements,
		// AI records
		services.AI,
		// WorkResourceRequirements
		services.WorkResources,
		// Access records (AccessGrants, AccessGroupStates)
		services.Access,
		// Approvals and Audit
		services.Approvals, services.Audit,
		// Quotations, Estimates, Costs, Labour, Materials
		services.Quotations, services.Estimates, services.Costs,
		services.Labour, services.Materials,
		// Spatial Intelligence records (captures, room versions, space state)
		services.Spatial,
		// Work Items, Spaces, Properties, Projects, Clients
		services.Work, services.Spaces, services.Properties,
		services.Projects, services.Clients,
		// Suppliers/Offerings/Preferences
		services.Suppliers,
	}
}
