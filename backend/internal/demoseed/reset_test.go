package demoseed_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

type fakeCleanupRepository struct {
	name        string
	deleteCalls []string
}

func (f *fakeCleanupRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	f.deleteCalls = append(f.deleteCalls, companyID)
	return nil
}

type fakeUserDeleter struct {
	deleteCalls []string
	notFoundErr error
}

func (f *fakeUserDeleter) DeleteUser(ctx context.Context, id string) error {
	f.deleteCalls = append(f.deleteCalls, id)
	return nil
}

type fakeSessionDeleter struct{ deleteCalls []string }

func (f *fakeSessionDeleter) DeleteAllSessionsForUser(ctx context.Context, userID string) error {
	f.deleteCalls = append(f.deleteCalls, userID)
	return nil
}

type fakeMembershipDeleter struct {
	deleteCalls []string
	err         error
}

func (f *fakeMembershipDeleter) DeleteMembershipByUserID(ctx context.Context, userID string) error {
	f.deleteCalls = append(f.deleteCalls, userID)
	return f.err
}

type fakeCompanyDeleter struct {
	deleteCalls []string
	err         error
}

func (f *fakeCompanyDeleter) DeleteCompany(ctx context.Context, id string) error {
	f.deleteCalls = append(f.deleteCalls, id)
	return f.err
}

func TestReset_NoManifestNoIdentity_IsNoOp(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)

	users := &fakeUserLookup{byEmail: map[string]identity.User{}}
	companyLookup := &fakeCompanyLookup{byName: map[string]companies.Company{}}
	result, err := demoseed.ResetWithDependencies(ctx, manifests, users, nil, companyLookup,
		&fakeUserDeleter{}, &fakeSessionDeleter{}, &fakeMembershipDeleter{}, &fakeCompanyDeleter{}, nil)
	if err != nil {
		t.Fatalf("ResetWithDependencies: %v", err)
	}
	if !result.WasNoOp {
		t.Fatalf("expected WasNoOp=true when no manifest, no User, and no Company exist")
	}
}

func TestReset_NoManifestButUserExists_Refuses(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)

	users := &fakeUserLookup{byEmail: map[string]identity.User{"demo@renovex.local": {ID: "user-1"}}}
	companyLookup := &fakeCompanyLookup{byName: map[string]companies.Company{}}
	_, err := demoseed.ResetWithDependencies(ctx, manifests, users, nil, companyLookup,
		&fakeUserDeleter{}, &fakeSessionDeleter{}, &fakeMembershipDeleter{}, &fakeCompanyDeleter{}, nil)
	if err == nil {
		t.Fatalf("expected refusal when the demo email exists but no manifest can prove ownership")
	}
}

func TestReset_NoManifestButCompanyNameExists_Refuses(t *testing.T) {
	// THE EXACT GAP THE REVIEW FOUND: a Company named
	// "Renovex Demo Contractor Sdn Bhd" exists, but demo@renovex.local does
	// NOT resolve to any User, and no manifest exists. The old code only
	// checked the User side and would have wrongly treated this as a no-op.
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)

	users := &fakeUserLookup{byEmail: map[string]identity.User{}}
	companyLookup := &fakeCompanyLookup{byName: map[string]companies.Company{
		"Renovex Demo Contractor Sdn Bhd": {ID: "company-orphan"},
	}}
	_, err := demoseed.ResetWithDependencies(ctx, manifests, users, nil, companyLookup,
		&fakeUserDeleter{}, &fakeSessionDeleter{}, &fakeMembershipDeleter{}, &fakeCompanyDeleter{}, nil)
	if err == nil {
		t.Fatalf("expected refusal when a Company named 'Renovex Demo Contractor Sdn Bhd' exists but no manifest can prove ownership")
	}
}

func TestReset_ManifestProvisioning_Refuses(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	_, _ = manifests.AcquireLease(ctx, "test-owner", testLeaseDuration)

	_, err := demoseed.ResetWithDependencies(ctx, manifests, &fakeUserLookup{}, &fakeMembershipLookup{}, &fakeCompanyLookup{},
		&fakeUserDeleter{}, &fakeSessionDeleter{}, &fakeMembershipDeleter{}, &fakeCompanyDeleter{}, nil)
	if err == nil {
		t.Fatalf("expected refusal when manifest state=provisioning")
	}
}

func TestReset_ManifestReady_FullTeardownInOrder(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	_, _ = manifests.AcquireLease(ctx, "test-owner", testLeaseDuration)
	_ = manifests.RecordIdentity(ctx, "test-owner", "user-1", "company-1")
	_ = manifests.MarkReady(ctx, "test-owner")
	_ = manifests.ReleaseLease(ctx, "test-owner")

	repo1 := &fakeCleanupRepository{name: "awards"}
	repo2 := &fakeCleanupRepository{name: "clients"}
	cleanupRepos := []demoseed.CleanupRepository{repo1, repo2}

	users := &fakeUserLookup{
		byEmail: map[string]identity.User{"demo@renovex.local": {ID: "user-1"}},
		byID:    map[string]identity.User{"user-1": {ID: "user-1"}},
	}
	memberships := &fakeMembershipLookup{byUserID: map[string]struct{ companyID, role string }{"user-1": {companyID: "company-1"}}}
	companyLookup := &fakeCompanyLookup{names: map[string]string{"company-1": "Renovex Demo Contractor Sdn Bhd"}, byName: map[string]companies.Company{}}
	userDeleter := &fakeUserDeleter{}
	sessionDeleter := &fakeSessionDeleter{}
	membershipDeleter := &fakeMembershipDeleter{}
	companyDeleter := &fakeCompanyDeleter{}

	result, err := demoseed.ResetWithDependencies(ctx, manifests, users, memberships, companyLookup,
		userDeleter, sessionDeleter, membershipDeleter, companyDeleter, cleanupRepos)
	if err != nil {
		t.Fatalf("ResetWithDependencies: %v", err)
	}
	if result.WasNoOp {
		t.Fatalf("expected a real reset, not a no-op")
	}
	if len(repo1.deleteCalls) != 1 || repo1.deleteCalls[0] != "company-1" {
		t.Fatalf("expected repo1.DeleteAllForCompany(company-1) exactly once, got %v", repo1.deleteCalls)
	}
	if len(sessionDeleter.deleteCalls) != 1 || sessionDeleter.deleteCalls[0] != "user-1" {
		t.Fatalf("expected AuthSessions deleted for user-1, got %v", sessionDeleter.deleteCalls)
	}
	if len(membershipDeleter.deleteCalls) != 1 || membershipDeleter.deleteCalls[0] != "user-1" {
		t.Fatalf("expected Membership deleted for user-1, got %v", membershipDeleter.deleteCalls)
	}
	if len(userDeleter.deleteCalls) != 1 || userDeleter.deleteCalls[0] != "user-1" {
		t.Fatalf("expected User user-1 deleted, got %v", userDeleter.deleteCalls)
	}
	if len(companyDeleter.deleteCalls) != 1 || companyDeleter.deleteCalls[0] != "company-1" {
		t.Fatalf("expected Company company-1 deleted, got %v", companyDeleter.deleteCalls)
	}
	if _, found, err := manifests.Get(ctx); err != nil || found {
		t.Fatalf("expected the manifest to be deleted after a successful reset, found=%v err=%v", found, err)
	}
}

func TestReset_InterruptedReset_ResumesAndToleratesAlreadyDeletedSteps(t *testing.T) {
	// Simulates a crash AFTER the Membership was already deleted (a
	// previous reset attempt got partway through), by having the fake
	// membership/company deleters return "not found" sentinel errors —
	// proving the resumed reset treats those as success, not failure.
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	// A short lease that expires almost immediately, simulating the crash:
	// the prior attempt's lease is never released, but its expiry passes,
	// so ResetWithDependencies's own AcquireLease call can reclaim it —
	// exactly as a real crashed process's stale lease would be reclaimed.
	shortLease := 1 * time.Millisecond
	_, _ = manifests.AcquireLease(ctx, "test-owner", shortLease)
	_ = manifests.RecordIdentity(ctx, "test-owner", "user-1", "company-1")
	_ = manifests.MarkReady(ctx, "test-owner")
	_, _ = manifests.BeginResetting(ctx, "test-owner") // simulates a prior reset that reached "resetting" then crashed
	time.Sleep(10 * time.Millisecond)

	repo := &fakeCleanupRepository{name: "awards"}
	cleanupRepos := []demoseed.CleanupRepository{repo}
	membershipDeleter := &fakeMembershipDeleter{err: companies.ErrMembershipNotFound} // "already deleted" by the crashed prior attempt
	companyDeleter := &fakeCompanyDeleter{err: companies.ErrCompanyNotFound}          // "already deleted" by the crashed prior attempt

	result, err := demoseed.ResetWithDependencies(ctx, manifests, nil, nil, nil,
		&fakeUserDeleter{}, &fakeSessionDeleter{}, membershipDeleter, companyDeleter, cleanupRepos)
	if err != nil {
		t.Fatalf("ResetWithDependencies (resuming an interrupted reset with already-deleted steps): %v", err)
	}
	if result.WasNoOp {
		t.Fatalf("expected a real (resumed) reset, not a no-op")
	}
	if len(repo.deleteCalls) != 1 {
		t.Fatalf("expected DeleteAllForCompany to be called exactly once during resume, got %d", len(repo.deleteCalls))
	}
	if _, found, err := manifests.Get(ctx); err != nil || found {
		t.Fatalf("expected the manifest to be deleted after the resumed reset completes, found=%v err=%v", found, err)
	}
}
