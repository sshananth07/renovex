package rfqissuance_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Invitation persistence (design spec §3.4, §5, §11.2).
//
// Real MongoDB: the stable-invitation invariant IS a unique index, and the
// generation/view updates ARE conditional writes. A fake would assert the
// test's own logic instead of what the database enforces.

func newInvitationRepo(t *testing.T, db *mongo.Database) *rfqissuance.MongoInvitationRepository {
	t.Helper()
	repo := rfqissuance.NewMongoInvitationRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure indexes: %v", err)
	}
	return repo
}

func invitationFixture(t *testing.T, companyID, rfqChainID, supplierID string) rfqissuance.SupplierInvitation {
	t.Helper()
	invitation, err := rfqissuance.NewInvitation(rfqissuance.NewInvitationInput{
		CompanyID: companyID, RFQChainID: rfqChainID, SupplierID: supplierID,
		CurrentIssuedRFQVersionID: "version-1",
		RecipientName:             "Aisha Rahman",
		RecipientEmail:            "sales@supplier.com",
		ExpiresAt:                 time.Now().Add(30 * 24 * time.Hour),
		CreatedByUserID:           "user-1",
	})
	if err != nil {
		t.Fatalf("building invitation: %v", err)
	}
	// Real invitations derive a different credential for every stable identity.
	// Keeping that property in the shared fixture prevents unrelated tests from
	// accidentally colliding with the global public-lookup index.
	invitation.AccessSecretHash = secrets.HashInvitationSecret(
		companyID + "/" + rfqChainID + "/" + supplierID,
	)
	invitation.SecretKeyVersion = 1
	return invitation
}

func TestCreateInvitationRoundTripsEveryField(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	input := invitationFixture(t, "company-1", "chain-1", "supplier-1")
	created, err := repo.CreateInvitation(ctx, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("the created invitation must carry its persisted ID")
	}

	loaded, err := repo.FindInvitation(ctx, "company-1", created.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	if loaded.SupplierID != "supplier-1" || loaded.RFQChainID != "chain-1" {
		t.Errorf("identity round-trip = %q/%q", loaded.SupplierID, loaded.RFQChainID)
	}
	if loaded.RecipientEmailNormalized != "sales@supplier.com" {
		t.Errorf("RecipientEmailNormalized = %q", loaded.RecipientEmailNormalized)
	}
	if loaded.Status != rfqissuance.InvitationStatusDraft {
		t.Errorf("Status = %q, want draft", loaded.Status)
	}
	if loaded.AccessGeneration != 1 || loaded.SecretKeyVersion != 1 {
		t.Errorf("generation/keyVersion = %d/%d, want 1/1",
			loaded.AccessGeneration, loaded.SecretKeyVersion)
	}
	if loaded.AccessSecretHash != input.AccessSecretHash {
		t.Errorf("AccessSecretHash = %q, want %q", loaded.AccessSecretHash, input.AccessSecretHash)
	}
	if loaded.CurrentIssuedRFQVersionID != "version-1" {
		t.Errorf("CurrentIssuedRFQVersionID = %q", loaded.CurrentIssuedRFQVersionID)
	}
}

// THE stable-invitation invariant (§3.4, §11.2): one per
// Company + RFQChain + Supplier.
func TestCreateInvitationRejectsASecondInvitationForTheSameSupplier(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.CreateInvitation(ctx, invitationFixture(t, "company-1", "chain-1", "supplier-1"))

	if !errors.Is(err, rfqissuance.ErrInvitationAlreadyExists) {
		t.Errorf("error = %v, want ErrInvitationAlreadyExists. The invitation is STABLE: "+
			"it advances across versions rather than being replaced (§3.4)", err)
	}
}

// Different Suppliers, chains and companies each get their own invitation.
func TestCreateInvitationPermitsDistinctSupplierChainAndCompany(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, tc := range []struct {
		name                     string
		company, chain, supplier string
	}{
		{"another supplier", "company-1", "chain-1", "supplier-2"},
		{"another chain", "company-1", "chain-2", "supplier-1"},
		{"another company", "company-2", "chain-1", "supplier-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := repo.CreateInvitation(ctx,
				invitationFixture(t, tc.company, tc.chain, tc.supplier)); err != nil {
				t.Errorf("must be permitted, got %v", err)
			}
		})
	}
}

func TestCreateInvitationRejectsASecretHashAlreadyUsedByAnotherTenant(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	first := invitationFixture(t, "company-1", "chain-1", "supplier-1")
	first.AccessSecretHash = "shared-current-access-secret-hash"
	if _, err := repo.CreateInvitation(ctx, first); err != nil {
		t.Fatalf("creating the first invitation: %v", err)
	}

	second := invitationFixture(t, "company-2", "chain-2", "supplier-2")
	second.AccessSecretHash = first.AccessSecretHash
	if _, err := repo.CreateInvitation(ctx, second); !errors.Is(
		err, rfqissuance.ErrAccessSecretHashCollision) {
		t.Fatalf("error = %v, want ErrAccessSecretHashCollision; a collision is "+
			"a configuration or derivation defect, not a duplicate invitation", err)
	}
}

func TestAccessHashUniqueIndexAllowsTransitionalDocumentsWithoutAHash(t *testing.T) {
	db := setupDB(t)
	_ = newInvitationRepo(t, db)
	ctx := context.Background()
	collection := db.Collection("supplier_invitations")

	// Revision 6 deliberately uses a partial unique index. Older documents may
	// predate opaque-link credentials, and two missing values must not collide.
	for _, document := range []bson.M{
		{"companyId": "company-1", "rfqChainId": "chain-1", "supplierId": "supplier-1"},
		{"companyId": "company-2", "rfqChainId": "chain-2", "supplierId": "supplier-2"},
	} {
		if _, err := collection.InsertOne(ctx, document); err != nil {
			t.Fatalf("inserting a transitional document without accessSecretHash: %v", err)
		}
	}
}

func TestFindInvitationIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := repo.FindInvitation(ctx, "company-2",
		created.ID); !errors.Is(err, rfqissuance.ErrInvitationNotFound) {
		t.Errorf("error = %v, want ErrInvitationNotFound for a foreign company", err)
	}
}

func TestFindInvitationByAccessSecretHashDerivesTenantFromTheStoredInvitation(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("creating invitation: %v", err)
	}

	loaded, err := repo.FindInvitationByAccessSecretHash(ctx, created.AccessSecretHash)
	if err != nil {
		t.Fatalf("global hash lookup: %v", err)
	}
	if loaded.ID != created.ID || loaded.CompanyID != "company-1" ||
		loaded.SupplierID != "supplier-1" {
		t.Fatalf("resolved identity = %#v, want the stored invitation identity", loaded)
	}
}

func TestRotatingSecretMakesThePreviousHashGloballyUnresolvable(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("creating invitation: %v", err)
	}
	oldHash := created.AccessSecretHash

	if _, err := repo.RotateSecret(ctx, created.CompanyID, created.ID,
		created.Revision, secrets.HashInvitationSecret("generation-2"), 2); err != nil {
		t.Fatalf("rotating invitation: %v", err)
	}

	if _, err := repo.FindInvitationByAccessSecretHash(ctx, oldHash); !errors.Is(
		err, rfqissuance.ErrInvitationNotFound) {
		t.Fatalf("old-hash lookup error = %v, want ErrInvitationNotFound", err)
	}
}

func TestListInvitationsForChainIsScopedAndComplete(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	for _, supplier := range []string{"supplier-1", "supplier-2"} {
		if _, err := repo.CreateInvitation(ctx,
			invitationFixture(t, "company-1", "chain-1", supplier)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	// Noise that must not appear.
	if _, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-2", "supplier-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-2", "chain-1", "supplier-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listed, err := repo.ListInvitationsForChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("got %d invitations, want exactly the 2 for this company+chain", len(listed))
	}
	for _, invitation := range listed {
		if invitation.CompanyID != "company-1" || invitation.RFQChainID != "chain-1" {
			t.Errorf("leaked an invitation from %s/%s", invitation.CompanyID, invitation.RFQChainID)
		}
	}
}

// Optimistic concurrency on every mutation (§11.3).
func TestUpdateInvitationRequiresTheExpectedRevision(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edited := created
	edited.Status = rfqissuance.InvitationStatusActive

	updated, err := repo.UpdateInvitation(ctx, "company-1", created.ID, created.Revision, edited)
	if err != nil {
		t.Fatalf("updating under the current revision failed: %v", err)
	}
	if updated.Status != rfqissuance.InvitationStatusActive {
		t.Errorf("Status = %q, want the edit applied", updated.Status)
	}
	if updated.Revision == created.Revision {
		t.Error("Revision must increment so a stale caller cannot match again")
	}

	if _, err := repo.UpdateInvitation(ctx, "company-1", created.ID, created.Revision,
		edited); !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch", err)
	}
}

// Secret rotation is what invalidates a previously sent link (§6.1A): the
// generation increments and the stored hash changes together, atomically.
func TestRotateSecretIncrementsGenerationAndReplacesTheHash(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := repo.RotateSecret(ctx, "company-1", created.ID, created.Revision,
		"hash-generation-2", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rotated.AccessGeneration != created.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want %d: the increment is what invalidates the "+
			"previously issued link", rotated.AccessGeneration, created.AccessGeneration+1)
	}
	if rotated.AccessSecretHash != "hash-generation-2" {
		t.Errorf("AccessSecretHash = %q, want the new hash", rotated.AccessSecretHash)
	}
	if rotated.SecretKeyVersion != 2 {
		t.Errorf("SecretKeyVersion = %d, want 2", rotated.SecretKeyVersion)
	}

	if _, err := repo.RotateSecret(ctx, "company-1", created.ID, created.Revision,
		"hash-x", 2); !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch for a stale rotation", err)
	}
}

// Exactly one of N concurrent rotations may win. Two winners would mean two
// generations claiming to be current, and a link derived under the loser would
// silently stop working.
func TestConcurrentRotateSecretHasExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const concurrency = 12
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.RotateSecret(ctx, "company-1", created.ID, created.Revision,
				"hash-concurrent", 1)
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, rfqissuance.ErrRevisionMismatch):
		default:
			t.Fatalf("goroutine %d failed unexpectedly: %v", i, err)
		}
	}

	if winners != 1 {
		t.Errorf("got %d winners, want exactly 1", winners)
	}

	final, err := repo.FindInvitation(ctx, "company-1", created.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if final.AccessGeneration != created.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want exactly one increment", final.AccessGeneration)
	}
}

// --- view tracking (§6.5) ---

// FirstViewedAt is the EARLIEST open and LastViewedAt the latest, applied with
// $min/$max so retries and concurrent opens are order-independent.
func TestRecordViewedSetsFirstAndLastViewedMonotonically(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	middle := time.Now().UTC().Truncate(time.Millisecond)
	earlier := middle.Add(-time.Hour)
	later := middle.Add(time.Hour)

	if err := repo.RecordViewed(ctx, "company-1", created.ID,
		created.AccessGeneration, middle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// An OUT-OF-ORDER earlier open must move First backwards but not Last.
	if err := repo.RecordViewed(ctx, "company-1", created.ID,
		created.AccessGeneration, earlier); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repo.RecordViewed(ctx, "company-1", created.ID,
		created.AccessGeneration, later); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := repo.FindInvitation(ctx, "company-1", created.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	if loaded.FirstViewedAt == nil || !loaded.FirstViewedAt.Equal(earlier) {
		t.Errorf("FirstViewedAt = %v, want the EARLIEST open %v", loaded.FirstViewedAt, earlier)
	}
	if loaded.LastViewedAt == nil || !loaded.LastViewedAt.Equal(later) {
		t.Errorf("LastViewedAt = %v, want the LATEST open %v", loaded.LastViewedAt, later)
	}
}

// THE race §6.5 exists to prevent: a validated OLD-generation request must not
// mark the replacement recipient's current invitation as viewed.
func TestRecordViewedIgnoresAnObsoleteAccessGeneration(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rotated, err := repo.RotateSecret(ctx, "company-1", created.ID, created.Revision,
		"hash-generation-2", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A request validated under the PREVIOUS generation now lands.
	err = repo.RecordViewed(ctx, "company-1", created.ID,
		created.AccessGeneration, time.Now())

	if !errors.Is(err, rfqissuance.ErrInvitationNotFound) {
		t.Errorf("error = %v, want ErrInvitationNotFound: an obsolete generation must not "+
			"record a view", err)
	}

	loaded, err := repo.FindInvitation(ctx, "company-1", created.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if loaded.FirstViewedAt != nil || loaded.LastViewedAt != nil {
		t.Error("an obsolete-generation attempt marked the replacement recipient's " +
			"invitation as viewed (§6.5)")
	}
	_ = rotated
}

func TestRecordViewedIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := repo.RecordViewed(ctx, "company-2", created.ID, created.AccessGeneration,
		time.Now()); !errors.Is(err, rfqissuance.ErrInvitationNotFound) {
		t.Errorf("error = %v, want ErrInvitationNotFound for a foreign company", err)
	}
}

// Advancing every non-revoked invitation to a newly issued version (§4.2 step 6).
func TestAdvanceInvitationsToVersionSkipsRevokedOnes(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	active, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	revokedInvitation := invitationFixture(t, "company-1", "chain-1", "supplier-2")
	revokedAt := time.Now()
	revokedInvitation.Status = rfqissuance.InvitationStatusRevoked
	revokedInvitation.RevokedAt = &revokedAt
	revoked, err := repo.CreateInvitation(ctx, revokedInvitation)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	advanced, err := repo.AdvanceInvitationsToVersion(ctx, "company-1", "chain-1", "version-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advanced != 1 {
		t.Errorf("advanced %d invitations, want 1: a revoked invitation must not be "+
			"advanced (§4.2)", advanced)
	}

	loadedActive, err := repo.FindInvitation(ctx, "company-1", active.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if loadedActive.CurrentIssuedRFQVersionID != "version-2" {
		t.Errorf("the active invitation points at %q, want version-2",
			loadedActive.CurrentIssuedRFQVersionID)
	}

	loadedRevoked, err := repo.FindInvitation(ctx, "company-1", revoked.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if loadedRevoked.CurrentIssuedRFQVersionID != "version-1" {
		t.Errorf("the revoked invitation moved to %q; revocation preserves its historical "+
			"pointer (§5.4)", loadedRevoked.CurrentIssuedRFQVersionID)
	}
}
