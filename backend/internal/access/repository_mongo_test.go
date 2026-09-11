package access_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/access"
)

func setupMongoDB(t *testing.T) *mongo.Database {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	return client.Database(fmt.Sprintf("access_test_%d", time.Now().UnixNano()))
}

func newRepos(t *testing.T, db *mongo.Database) (*access.MongoAccessGrantRepository, *access.MongoAccessGroupStateRepository) {
	t.Helper()
	grantRepo := access.NewMongoAccessGrantRepository(db)
	groupRepo := access.NewMongoAccessGroupStateRepository(db)
	ctx := context.Background()
	if err := grantRepo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("grant EnsureIndexes: %v", err)
	}
	if err := groupRepo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("group EnsureIndexes: %v", err)
	}
	return grantRepo, groupRepo
}

func sampleGrant(companyID, resourceID, groupKey, tokenHash string) access.AccessGrant {
	return access.AccessGrant{
		CompanyID: companyID, ProjectID: "project_1",
		ResourceType: access.ResourceTypeQuotation, ResourceID: resourceID,
		ResourceGroupKey: groupKey, QuotationNumber: "QT-000001",
		GranteeType: access.GranteeTypeClient, GranteeID: "client_1",
		Permissions: access.ClientQuotationPermissions,
		TokenHash:   tokenHash,
		Status:      access.AccessGrantStatusActive,
		CreatedAt:   time.Now(), ExpiresAt: time.Now().Add(24 * time.Hour),
		SchemaVersion: 1,
	}
}

func TestGrantRepositoryCreateAndFindByTokenHash(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, _ := newRepos(t, db)
	ctx := context.Background()

	created, err := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "hash_1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected an assigned ID")
	}

	found, err := grantRepo.FindByTokenHash(ctx, "hash_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID || found.CompanyID != "company_a" || found.ResourceID != "quotation_1" {
		t.Fatalf("round-trip mismatch: %+v", found)
	}
	if found.ExpiresAt.IsZero() {
		t.Fatal("expected a concrete ExpiresAt to round-trip")
	}
}

func TestGrantRepositoryFindByIDIsTenantScoped(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, _ := newRepos(t, db)
	ctx := context.Background()

	created, _ := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "hash_1"))

	if _, err := grantRepo.FindByID(ctx, "company_b", created.ID); err != access.ErrGrantNotFound {
		t.Fatalf("expected ErrGrantNotFound cross-tenant, got %v", err)
	}
}

func TestGrantRepositoryDuplicateTokenHashRejected(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, _ := newRepos(t, db)
	ctx := context.Background()

	if _, err := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "same_hash")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_2", "quotation:QT-000002", "same_hash"))
	if err != access.ErrUnclassifiedDuplicateKey {
		t.Fatalf("expected ErrUnclassifiedDuplicateKey on token-hash collision, got %v", err)
	}
}

// Multiple historical grants for the SAME resource must be allowed — the
// per-resource unique index was deliberately removed (design spec §2.1).
func TestGrantRepositoryAllowsMultipleGrantsPerResource(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, _ := newRepos(t, db)
	ctx := context.Background()

	if _, err := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "hash_1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "hash_2")); err != nil {
		t.Fatalf("expected a second historical grant for the same resource to be allowed, got %v", err)
	}

	latest, err := grantRepo.FindLatestByResource(ctx, "company_a", access.ResourceTypeQuotation, "quotation_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.TokenHash != "hash_2" {
		t.Fatalf("expected the newest grant, got token hash %q", latest.TokenHash)
	}
}

func TestGrantRepositoryRevokeIsRevisionGuarded(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, _ := newRepos(t, db)
	ctx := context.Background()

	created, _ := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "hash_1"))

	if _, err := grantRepo.Revoke(ctx, "company_a", created.ID, 99, access.RevokedReasonManual, time.Now()); err != access.ErrGrantRevisionMismatch {
		t.Fatalf("expected ErrGrantRevisionMismatch on stale revision, got %v", err)
	}

	revoked, err := grantRepo.Revoke(ctx, "company_a", created.ID, 0, access.RevokedReasonManual, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if revoked.Status != access.AccessGrantStatusRevoked || revoked.RevokedReason != access.RevokedReasonManual {
		t.Fatalf("expected a manually revoked grant, got %+v", revoked)
	}
	if revoked.Revision != 1 {
		t.Fatalf("expected revision incremented to 1, got %d", revoked.Revision)
	}

	// Already revoked -> no longer eligible.
	if _, err := grantRepo.Revoke(ctx, "company_a", created.ID, 1, access.RevokedReasonManual, time.Now()); err != access.ErrGrantRevisionMismatch {
		t.Fatalf("expected ErrGrantRevisionMismatch revoking twice, got %v", err)
	}
}

func TestGrantRepositoryUpdateExpiryIsRevisionGuarded(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, _ := newRepos(t, db)
	ctx := context.Background()

	created, _ := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "hash_1"))
	newExpiry := time.Now().Add(72 * time.Hour)

	if _, err := grantRepo.UpdateExpiry(ctx, "company_a", created.ID, 42, newExpiry); err != access.ErrGrantRevisionMismatch {
		t.Fatalf("expected ErrGrantRevisionMismatch on stale revision, got %v", err)
	}

	updated, err := grantRepo.UpdateExpiry(ctx, "company_a", created.ID, 0, newExpiry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", updated.Revision)
	}
	if updated.ExpiresAt.Unix() != newExpiry.Unix() {
		t.Fatalf("expected expiry updated, got %v", updated.ExpiresAt)
	}
}

// --- Coordinator (AccessGroupState) tests ---

func TestGroupStateFindOrCreateIsIdempotent(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	first, err := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.ID != second.ID {
		t.Fatal("expected FindOrCreate to return the same coordinator document")
	}
	if second.ActiveGrantID != nil || second.AcceptedResourceID != nil {
		t.Fatalf("expected a fresh coordinator to have no active/accepted grant, got %+v", second)
	}
}

// Concurrent FindOrCreate for a brand-new chain must yield exactly one
// coordinator document (guarded by uq_access_group_states_company_group).
func TestGroupStateConcurrentFindOrCreateYieldsOneDocument(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	const goroutines = 8
	ids := make([]string, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st, err := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())
			ids[i], errs[i] = st.ID, err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	for i := 1; i < goroutines; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("expected every concurrent FindOrCreate to converge on one document, got %q vs %q", ids[i], ids[0])
		}
	}
}

// The central race: exactly one of N concurrent claims may activate a grant.
func TestGroupStateClaimActiveGrantHasExactlyOneWinner(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	state, _ := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())

	const goroutines = 6
	results := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
				state.Revision, "quotation_1", nil, "quotation_1", fmt.Sprintf("candidate_%d", i), true)
			results[i] = err
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range results {
		switch err {
		case nil:
			winners++
		case access.ErrGroupStateRevisionMismatch:
			// expected loser
		default:
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 winning claim, got %d", winners)
	}

	final, _ := groupRepo.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if final.ActiveGrantID == nil {
		t.Fatal("expected an active grant after a successful claim")
	}
	if final.Revision != state.Revision+1 {
		t.Fatalf("expected exactly one revision increment, got %d", final.Revision)
	}
}

func TestGroupStateClaimActiveGrantRejectedWhenChainAccepted(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	state, _ := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())
	claimed, err := groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		state.Revision, "quotation_1", nil, "quotation_1", "grant_1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	accepted, err := groupRepo.ClaimAcceptance(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		claimed.Revision, "quotation_1", "grant_1")
	if err != nil {
		t.Fatalf("unexpected error accepting: %v", err)
	}

	// Sharing ANY version of an accepted chain must fail (requireAcceptedNil).
	_, err = groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		accepted.Revision, "quotation_1", strPtr("grant_1"), "quotation_2", "grant_2", true)
	if err != access.ErrGroupStateRevisionMismatch {
		t.Fatalf("expected the claim to fail on an accepted chain, got %v", err)
	}

	// But rotating the ACCEPTED version's own grant is allowed
	// (requireAcceptedNil=false, newCurrentResourceID == acceptedResourceId).
	if _, err := groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		accepted.Revision, "quotation_1", strPtr("grant_1"), "quotation_1", "grant_1b", false); err != nil {
		t.Fatalf("expected rotation of the accepted version's own grant to be allowed, got %v", err)
	}
}

func TestGroupStateClaimAcceptanceIsSingleWinner(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	state, _ := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())
	claimed, _ := groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		state.Revision, "quotation_1", nil, "quotation_1", "grant_1", true)

	const goroutines = 5
	results := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := groupRepo.ClaimAcceptance(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
				claimed.Revision, "quotation_1", "grant_1")
			results[i] = err
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, err := range results {
		if err == nil {
			winners++
		} else if err != access.ErrGroupStateRevisionMismatch {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly 1 winning acceptance claim, got %d", winners)
	}

	final, _ := groupRepo.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if final.AcceptedResourceID == nil || *final.AcceptedResourceID != "quotation_1" {
		t.Fatalf("expected AcceptedResourceID set to quotation_1, got %+v", final.AcceptedResourceID)
	}
}

// The exact cross-collection race the coordinator exists to close: a share of
// V2 and an acceptance of V1, racing. Exactly one may win.
func TestGroupStateShareVersusAcceptHasOneWinner(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	state, _ := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())
	claimed, _ := groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		state.Revision, "quotation_1", nil, "quotation_1", "grant_v1", true)

	var shareErr, acceptErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, shareErr = groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
			claimed.Revision, "quotation_1", strPtr("grant_v1"), "quotation_2", "grant_v2", true)
	}()
	go func() {
		defer wg.Done()
		_, acceptErr = groupRepo.ClaimAcceptance(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
			claimed.Revision, "quotation_1", "grant_v1")
	}()
	wg.Wait()

	if (shareErr == nil) == (acceptErr == nil) {
		t.Fatalf("expected exactly one winner, got shareErr=%v acceptErr=%v", shareErr, acceptErr)
	}

	final, _ := groupRepo.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if acceptErr == nil {
		// Acceptance won: V1 accepted, chain never moved to V2.
		if final.AcceptedResourceID == nil || *final.AcceptedResourceID != "quotation_1" {
			t.Fatal("acceptance won but AcceptedResourceID is not quotation_1")
		}
		if final.CurrentResourceID != "quotation_1" {
			t.Fatalf("acceptance won but chain moved to %q", final.CurrentResourceID)
		}
	} else {
		// Share won: chain on V2, nothing accepted.
		if final.AcceptedResourceID != nil {
			t.Fatal("share won but the chain shows an acceptance")
		}
		if final.CurrentResourceID != "quotation_2" {
			t.Fatalf("share won but chain is on %q", final.CurrentResourceID)
		}
	}
}

func TestGroupStateFenceNonTerminalDecisionBlocksAfterAcceptance(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	state, _ := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now())
	claimed, _ := groupRepo.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		state.Revision, "quotation_1", nil, "quotation_1", "grant_1", true)

	// A fence succeeds while the chain is un-accepted.
	fenced, err := groupRepo.FenceNonTerminalDecision(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		claimed.Revision, "quotation_1", "grant_1")
	if err != nil {
		t.Fatalf("unexpected error fencing: %v", err)
	}

	accepted, err := groupRepo.ClaimAcceptance(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		fenced.Revision, "quotation_1", "grant_1")
	if err != nil {
		t.Fatalf("unexpected error accepting: %v", err)
	}

	// After acceptance, a non-terminal decision can never fence.
	if _, err := groupRepo.FenceNonTerminalDecision(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		accepted.Revision, "quotation_1", "grant_1"); err != access.ErrGroupStateRevisionMismatch {
		t.Fatalf("expected the fence to fail after acceptance, got %v", err)
	}
}

func TestGroupStateIsTenantScoped(t *testing.T) {
	db := setupMongoDB(t)
	_, groupRepo := newRepos(t, db)
	ctx := context.Background()

	if _, err := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := groupRepo.Find(ctx, "company_b", access.ResourceTypeQuotation, "quotation:QT-000001"); err != access.ErrGroupStateNotFound {
		t.Fatalf("expected ErrGroupStateNotFound cross-tenant, got %v", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's multi-collection
// guidance (spec §6.6): ONE Service.DeleteAllForCompany call removes BOTH
// AccessGrant and AccessGroupState records for companyID. Seeds via both
// repositories directly (matching every sibling test in this file).
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, groupRepo := newRepos(t, db)
	svc := access.NewService(grantRepo, groupRepo, nil, nil, nil, nil, nil, nil, "")
	ctx := context.Background()

	if _, err := grantRepo.Create(ctx, sampleGrant("company_a", "quotation_1", "quotation:QT-000001", "hash_a")); err != nil {
		t.Fatalf("create company_a grant: %v", err)
	}
	if _, err := grantRepo.Create(ctx, sampleGrant("company_b", "quotation_2", "quotation:QT-000002", "hash_b")); err != nil {
		t.Fatalf("create company_b grant: %v", err)
	}
	if _, err := groupRepo.FindOrCreate(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001", "quotation_1", time.Now()); err != nil {
		t.Fatalf("create company_a group state: %v", err)
	}
	if _, err := groupRepo.FindOrCreate(ctx, "company_b", access.ResourceTypeQuotation, "quotation:QT-000002", "quotation_2", time.Now()); err != nil {
		t.Fatalf("create company_b group state: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	if _, err := grantRepo.FindLatestByResource(ctx, "company_a", access.ResourceTypeQuotation, "quotation_1"); err != access.ErrGrantNotFound {
		t.Fatalf("expected ErrGrantNotFound for company_a's deleted grant, got %v", err)
	}
	if _, err := groupRepo.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001"); err != access.ErrGroupStateNotFound {
		t.Fatalf("expected ErrGroupStateNotFound for company_a's deleted group state, got %v", err)
	}

	if _, err := grantRepo.FindLatestByResource(ctx, "company_b", access.ResourceTypeQuotation, "quotation_2"); err != nil {
		t.Fatalf("expected company_b's grant to be untouched, got %v", err)
	}
	if _, err := groupRepo.Find(ctx, "company_b", access.ResourceTypeQuotation, "quotation:QT-000002"); err != nil {
		t.Fatalf("expected company_b's group state to be untouched, got %v", err)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupMongoDB(t)
	grantRepo, groupRepo := newRepos(t, db)
	svc := access.NewService(grantRepo, groupRepo, nil, nil, nil, nil, nil, nil, "")
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}

func strPtr(s string) *string { return &s }
