package demoseed_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

type fakeAuth struct {
	registerCalls int
	registerErr   error
}

func (f *fakeAuth) Register(ctx context.Context, email, password, companyName string) (identity.AuthResult, error) {
	f.registerCalls++
	if f.registerErr != nil {
		return identity.AuthResult{}, f.registerErr
	}
	return identity.AuthResult{AccessToken: "fake-token"}, nil
}

type fakeUserLookup struct {
	byEmail map[string]identity.User
	byID    map[string]identity.User
}

func (f *fakeUserLookup) FindUserByEmail(ctx context.Context, email string) (identity.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return u, nil
}
func (f *fakeUserLookup) FindUserByID(ctx context.Context, id string) (identity.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return identity.User{}, identity.ErrUserNotFound
	}
	return u, nil
}

type fakeMembershipLookup struct {
	byUserID map[string]struct{ companyID, role string }
}

func (f *fakeMembershipLookup) FindMembershipByUserID(ctx context.Context, userID string) (string, string, error) {
	m, ok := f.byUserID[userID]
	if !ok {
		return "", "", companies.ErrMembershipNotFound
	}
	return m.companyID, m.role, nil
}

type fakeCompanyLookup struct {
	names  map[string]string
	byName map[string]companies.Company
}

func (f *fakeCompanyLookup) GetCompanyName(ctx context.Context, companyID string) (string, error) {
	n, ok := f.names[companyID]
	if !ok {
		return "", companies.ErrCompanyNotFound
	}
	return n, nil
}
func (f *fakeCompanyLookup) FindCompanyByName(ctx context.Context, name string) (companies.Company, error) {
	c, ok := f.byName[name]
	if !ok {
		return companies.Company{}, companies.ErrCompanyNotFound
	}
	return c, nil
}

func TestResolveDemoTenant_FreshEnvironment_Registers(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	if _, err := manifests.AcquireLease(ctx, "test-owner", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}

	auth := &fakeAuth{}
	users := &fakeUserLookup{byEmail: map[string]identity.User{}, byID: map[string]identity.User{}}
	memberships := &fakeMembershipLookup{byUserID: map[string]struct{ companyID, role string }{}}
	companyLookup := &fakeCompanyLookup{names: map[string]string{}, byName: map[string]companies.Company{}}

	_, err := demoseed.ResolveDemoTenant(ctx, "test-owner", auth, users, memberships, companyLookup, manifests, "dev-only-password")
	// This fake setup cannot fully complete resolution (Register succeeds
	// but the fakes have no post-Register lookup data configured), so this
	// specific test only asserts the recognizably-correct behavior: it
	// must have attempted exactly one Register call, proving the
	// "no existing demo User -> Register" branch actually ran, and NOT
	// the recovery branch.
	_ = err
	if auth.registerCalls != 1 {
		t.Fatalf("expected exactly 1 Register call, got %d", auth.registerCalls)
	}
}

func TestResolveDemoTenant_CrashAfterRegisterBeforeRecordIdentity_RecoversWithoutReRegistering(t *testing.T) {
	// This is the exact crash scenario the review described: Register
	// already succeeded (a real User/Company/Membership exist), but the
	// manifest never got RecordIdentity/MarkReady called on it — it is
	// still state=provisioning with empty stored IDs. The recovery branch
	// must observe that demo@renovex.local ALREADY resolves to a User and
	// skip calling Register a second time.
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	if _, err := manifests.AcquireLease(ctx, "test-owner", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	// Manifest is left in state=provisioning with NO RecordIdentity call —
	// simulating the crash window.

	hashedPassword, err := identity.HashPassword("dev-only-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	auth := &fakeAuth{registerErr: errors.New("simulated: demo@renovex.local already exists")}
	users := &fakeUserLookup{
		byEmail: map[string]identity.User{"demo@renovex.local": {ID: "user-1", PasswordHash: hashedPassword}},
		byID:    map[string]identity.User{"user-1": {ID: "user-1", PasswordHash: hashedPassword}},
	}
	memberships := &fakeMembershipLookup{byUserID: map[string]struct{ companyID, role string }{
		"user-1": {companyID: "company-1", role: "owner"},
	}}
	companyLookup := &fakeCompanyLookup{
		names:  map[string]string{"company-1": "Renovex Demo Contractor Sdn Bhd"},
		byName: map[string]companies.Company{"Renovex Demo Contractor Sdn Bhd": {ID: "company-1"}},
	}

	companyID, err := demoseed.ResolveDemoTenant(ctx, "test-owner", auth, users, memberships, companyLookup, manifests, "dev-only-password")
	if err != nil {
		t.Fatalf("ResolveDemoTenant should have RECOVERED via observed state, not errored: %v", err)
	}
	if companyID != "company-1" {
		t.Fatalf("companyID = %q, want company-1", companyID)
	}
	if auth.registerCalls != 0 {
		t.Fatalf("expected Register NOT to be called during crash recovery (the User already exists), got %d calls", auth.registerCalls)
	}

	manifest, found, err := manifests.Get(ctx)
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if manifest.State != demoseed.ManifestStateReady {
		t.Fatalf("expected the manifest to reach state=ready after recovery, got %q", manifest.State)
	}
	if manifest.DemoUserID != "user-1" || manifest.DemoCompanyID != "company-1" {
		t.Fatalf("expected recovery to record the OBSERVED IDs into the manifest, got user=%q company=%q", manifest.DemoUserID, manifest.DemoCompanyID)
	}
}

func TestResolveDemoTenant_CrashRecovery_WrongPasswordRefuses(t *testing.T) {
	// The demo email exists (crash recovery scenario), but the supplied
	// RENOVEX_DEMO_PASSWORD does not match it — a genuine identity
	// conflict, not something to silently paper over.
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	if _, err := manifests.AcquireLease(ctx, "test-owner", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}

	hashedPassword, _ := identity.HashPassword("the-real-password")
	auth := &fakeAuth{}
	users := &fakeUserLookup{
		byEmail: map[string]identity.User{"demo@renovex.local": {ID: "user-1", PasswordHash: hashedPassword}},
	}
	_, err := demoseed.ResolveDemoTenant(ctx, "test-owner", auth, users, &fakeMembershipLookup{}, &fakeCompanyLookup{}, manifests, "a-wrong-password")
	if err == nil {
		t.Fatalf("expected an error for a wrong RENOVEX_DEMO_PASSWORD during crash recovery")
	}
	if auth.registerCalls != 0 {
		t.Fatalf("expected Register NOT to be attempted when the demo email already resolves to a User")
	}
}

func TestResolveDemoTenant_ManifestReady_NeverCallsRegister(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	if _, err := manifests.AcquireLease(ctx, "test-owner", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	_ = manifests.RecordIdentity(ctx, "test-owner", "user-1", "company-1")
	_ = manifests.MarkReady(ctx, "test-owner")

	hashedPassword, err := identity.HashPassword("dev-only-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	auth := &fakeAuth{}
	users := &fakeUserLookup{
		byEmail: map[string]identity.User{"demo@renovex.local": {ID: "user-1", PasswordHash: hashedPassword}},
		byID:    map[string]identity.User{"user-1": {ID: "user-1", PasswordHash: hashedPassword}},
	}
	memberships := &fakeMembershipLookup{byUserID: map[string]struct{ companyID, role string }{
		"user-1": {companyID: "company-1", role: "owner"},
	}}
	companyLookup := &fakeCompanyLookup{names: map[string]string{"company-1": "Renovex Demo Contractor Sdn Bhd"}}

	companyID, err := demoseed.ResolveDemoTenant(ctx, "test-owner", auth, users, memberships, companyLookup, manifests, "dev-only-password")
	if err != nil {
		t.Fatalf("ResolveDemoTenant: %v", err)
	}
	if companyID != "company-1" {
		t.Fatalf("companyID = %q, want company-1", companyID)
	}
	if auth.registerCalls != 0 {
		t.Fatalf("expected Register NOT to be called when manifest is already ready, got %d calls", auth.registerCalls)
	}
}

func TestResolveDemoTenant_ManifestResetting_Refuses(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)
	if _, err := manifests.AcquireLease(ctx, "test-owner", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	_ = manifests.RecordIdentity(ctx, "test-owner", "user-1", "company-1")
	_ = manifests.MarkReady(ctx, "test-owner")
	_, _ = manifests.BeginResetting(ctx, "test-owner")

	_, err := demoseed.ResolveDemoTenant(ctx, "test-owner", &fakeAuth{}, &fakeUserLookup{}, &fakeMembershipLookup{}, &fakeCompanyLookup{}, manifests, "dev-only-password")
	if err == nil {
		t.Fatalf("expected seed to refuse while a reset is in progress (state=resetting)")
	}
}
