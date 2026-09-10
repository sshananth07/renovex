package demoseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// demoEmail/demoCompanyName are declared in Task 3's manifest.go —
// AcquireLease needs them before this file exists. Not redeclared here.

// Registrar is the capability demoseed needs from identity: creating the
// demo User + owner Company + Membership as one compensating unit.
// Satisfied structurally by *identity.AuthService.
type Registrar interface {
	Register(ctx context.Context, email, password, companyName string) (identity.AuthResult, error)
}

// UserLookup is the capability demoseed needs to verify the demo password
// without creating a session. Satisfied structurally by
// *identity.UserService. Both methods MUST return identity.ErrUserNotFound
// (via errors.Is) specifically when no User matches — every caller in this
// package distinguishes "genuinely absent" from "lookup failed for some
// other reason" and only the former is safe to treat as "does not exist".
type UserLookup interface {
	FindUserByEmail(ctx context.Context, email string) (identity.User, error)
	FindUserByID(ctx context.Context, id string) (identity.User, error)
}

// MembershipLookup is the capability demoseed needs to resolve a User's
// Company. Satisfied structurally by *companies.Service.
type MembershipLookup interface {
	FindMembershipByUserID(ctx context.Context, userID string) (companyID, role string, err error)
}

// CompanyLookup is the capability demoseed needs to verify the resolved
// Company's display name, and (for Task 16's ambiguous-refuse check) to
// look up a Company by name directly. Satisfied structurally by
// *companies.Service.
type CompanyLookup interface {
	GetCompanyName(ctx context.Context, companyID string) (string, error)
	FindCompanyByName(ctx context.Context, name string) (companies.Company, error)
}

// ErrDemoTenantIdentityMismatch is returned whenever the identity checks
// below do not fully agree, or the supplied RENOVEX_DEMO_PASSWORD does not
// authenticate an existing demo User. demoseed never repairs identity on
// disagreement — it refuses (design spec §6.2).
var ErrDemoTenantIdentityMismatch = fmt.Errorf("demoseed: demo tenant identity did not verify")

// ResolveDemoTenant implements the manifest state machine's seed-side
// recovery/provisioning logic (design spec §6.2/§6.3) and returns the
// verified demo companyID, ready for scenario seeding. The caller must
// already hold the manifest lease (ownerToken) — see Task 15.
func ResolveDemoTenant(
	ctx context.Context,
	ownerToken string,
	auth Registrar,
	users UserLookup,
	memberships MembershipLookup,
	companyLookup CompanyLookup,
	manifests *ManifestStore,
	demoPassword string,
) (string, error) {
	manifest, found, err := manifests.Get(ctx)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("demoseed: no manifest found — the caller must AcquireLease before calling ResolveDemoTenant")
	}

	switch manifest.State {
	case ManifestStateResetting:
		return "", fmt.Errorf("%w: a reset is currently in progress (manifest state=resetting); finish or resume reset before seeding", ErrDemoTenantIdentityMismatch)

	case ManifestStateProvisioning:
		if manifest.DemoUserID == "" || manifest.DemoCompanyID == "" {
			// CRITICAL: inspect OBSERVED state before calling Register.
			// This is the exact fix for the crash the review described —
			// Register may have already succeeded in a previous run that
			// crashed before RecordIdentity was called.
			//
			// Fail CLOSED on lookup errors. Only a confirmed
			// identity.ErrUserNotFound proves the User genuinely does not
			// exist yet — any other error (a timeout, a dropped
			// connection, anything) must NOT be silently treated as "safe
			// to Register a new demo tenant." A destructive/creating tool
			// misinterpreting "I don't know" as "it doesn't exist" is
			// exactly the kind of bug this fail-closed rule prevents.
			existingUser, findErr := users.FindUserByEmail(ctx, demoEmail)
			var userAlreadyExists bool
			switch {
			case findErr == nil:
				userAlreadyExists = true
			case errors.Is(findErr, identity.ErrUserNotFound):
				userAlreadyExists = false
			default:
				return "", fmt.Errorf("demoseed: look up demo user by email: %w", findErr)
			}

			var demoUserID, demoCompanyID string
			if !userAlreadyExists {
				// Genuine fresh start.
				if _, err := auth.Register(ctx, demoEmail, demoPassword, demoCompanyName); err != nil {
					return "", fmt.Errorf("demoseed: register demo tenant: %w", err)
				}
				registeredUser, err := users.FindUserByEmail(ctx, demoEmail)
				if err != nil {
					return "", fmt.Errorf("demoseed: locate freshly-registered demo user: %w", err)
				}
				companyID, _, err := memberships.FindMembershipByUserID(ctx, registeredUser.ID)
				if err != nil {
					return "", fmt.Errorf("demoseed: locate freshly-registered demo company: %w", err)
				}
				demoUserID, demoCompanyID = registeredUser.ID, companyID
			} else {
				// RECOVERY: the User already exists — this is a crash
				// between a previous Register and RecordIdentity. Verify
				// the password (never reset it) before trusting this
				// identity.
				if err := identity.VerifyPassword(existingUser.PasswordHash, demoPassword); err != nil {
					return "", fmt.Errorf("%w: demo email already exists but RENOVEX_DEMO_PASSWORD does not authenticate it; refusing to reset its password or take ownership", ErrDemoTenantIdentityMismatch)
				}
				companyID, _, err := memberships.FindMembershipByUserID(ctx, existingUser.ID)
				if err != nil {
					return "", fmt.Errorf("%w: demo User exists but has no Membership: %v", ErrDemoTenantIdentityMismatch, err)
				}
				demoUserID, demoCompanyID = existingUser.ID, companyID
			}

			if err := verifyDemoIdentity(ctx, users, memberships, companyLookup, demoUserID, demoCompanyID); err != nil {
				return "", err
			}
			if err := manifests.RecordIdentity(ctx, ownerToken, demoUserID, demoCompanyID); err != nil {
				return "", err
			}
			manifest.DemoUserID, manifest.DemoCompanyID = demoUserID, demoCompanyID
		} else {
			// IDs already recorded, but state=ready was never reached —
			// re-verify before advancing.
			if err := verifyDemoIdentity(ctx, users, memberships, companyLookup, manifest.DemoUserID, manifest.DemoCompanyID); err != nil {
				return "", err
			}
		}
		if err := manifests.MarkReady(ctx, ownerToken); err != nil {
			return "", err
		}
		return manifest.DemoCompanyID, nil

	case ManifestStateReady:
		// verifyDemoIdentity (below) already re-confirms demoEmail resolves
		// to manifest.DemoUserID as part of its full chain — this lookup
		// exists ONLY to obtain the password hash for verification, which
		// is specific to Seed (Reset, Task 16, never checks a password).
		user, err := users.FindUserByEmail(ctx, demoEmail)
		if errors.Is(err, identity.ErrUserNotFound) {
			return "", fmt.Errorf("%w: demo email does not resolve to a User: %v", ErrDemoTenantIdentityMismatch, err)
		}
		if err != nil {
			return "", fmt.Errorf("demoseed: look up demo user by email: %w", err)
		}
		if err := identity.VerifyPassword(user.PasswordHash, demoPassword); err != nil {
			return "", fmt.Errorf("%w: RENOVEX_DEMO_PASSWORD does not authenticate the existing demo user; refusing to reset its password or take ownership", ErrDemoTenantIdentityMismatch)
		}
		if err := verifyDemoIdentity(ctx, users, memberships, companyLookup, manifest.DemoUserID, manifest.DemoCompanyID); err != nil {
			return "", err
		}
		return manifest.DemoCompanyID, nil
	}

	return "", fmt.Errorf("demoseed: manifest is in an unrecognized state %q", manifest.State)
}

// verifyDemoIdentity is the SINGLE authoritative identity-verification
// routine — ResolveDemoTenant's ready branch and Reset (Task 16) both call
// this exact function rather than each implementing their own partial
// version, so neither can accidentally skip a leg of the check. It
// verifies the FULL chain:
//
//	demoEmail resolves to a User whose ID == demoUserID
//	-> demoUserID resolves to a User                      (identity exists)
//	-> that User's Membership resolves to demoCompanyID    (ownership)
//	-> demoCompanyID resolves to a Company named demoCompanyName (naming)
//
// Every lookup below fails CLOSED on error: only a confirmed "not found"
// sentinel is treated as a genuine identity mismatch: every other error
// (timeouts, dropped connections, anything this function cannot positively
// attribute to "the record doesn't exist") is returned AS-IS, unwrapped
// from ErrDemoTenantIdentityMismatch, so callers do not mistake "the
// database was unreachable" for "the demo tenant's identity is corrupt" —
// the two demand very different responses from an operator.
func verifyDemoIdentity(ctx context.Context, users UserLookup, memberships MembershipLookup, companyLookup CompanyLookup, demoUserID, demoCompanyID string) error {
	emailUser, err := users.FindUserByEmail(ctx, demoEmail)
	if errors.Is(err, identity.ErrUserNotFound) {
		return fmt.Errorf("%w: demo email does not resolve to any User: %v", ErrDemoTenantIdentityMismatch, err)
	}
	if err != nil {
		return fmt.Errorf("demoseed: look up demo user by email: %w", err)
	}
	if emailUser.ID != demoUserID {
		return fmt.Errorf("%w: demo email resolves to a different User than the manifest recorded", ErrDemoTenantIdentityMismatch)
	}

	if _, err := users.FindUserByID(ctx, demoUserID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			return fmt.Errorf("%w: manifest's demoUserId does not resolve to a User: %v", ErrDemoTenantIdentityMismatch, err)
		}
		return fmt.Errorf("demoseed: look up demo user by id: %w", err)
	}
	companyID, _, err := memberships.FindMembershipByUserID(ctx, demoUserID)
	if err != nil {
		if errors.Is(err, companies.ErrMembershipNotFound) {
			return fmt.Errorf("%w: demo User has no Membership: %v", ErrDemoTenantIdentityMismatch, err)
		}
		return fmt.Errorf("demoseed: look up demo user's membership: %w", err)
	}
	if companyID != demoCompanyID {
		return fmt.Errorf("%w: demo User's Membership points at a different Company than the manifest recorded", ErrDemoTenantIdentityMismatch)
	}
	name, err := companyLookup.GetCompanyName(ctx, demoCompanyID)
	if err != nil {
		if errors.Is(err, companies.ErrCompanyNotFound) {
			return fmt.Errorf("%w: manifest's demoCompanyId does not resolve to a Company: %v", ErrDemoTenantIdentityMismatch, err)
		}
		return fmt.Errorf("demoseed: look up demo company name: %w", err)
	}
	if name != demoCompanyName {
		return fmt.Errorf("%w: Company name is %q, expected %q", ErrDemoTenantIdentityMismatch, name, demoCompanyName)
	}
	return nil
}
