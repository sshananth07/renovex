package spatial_test

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

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// setupDB gives every persistence test its own real MongoDB database.
// Task 2's CAS/current-room-version guarantees live in MongoDB conditional
// writes, so an in-memory fake cannot prove these boundaries (pattern from
// internal/supplieroffers/mongo_test.go).
func setupDB(t *testing.T) *mongo.Database {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	if err != nil {
		t.Fatalf("starting MongoDB: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminating MongoDB: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("reading MongoDB connection string: %v", err)
	}
	// The single member advertises its container address, which is not
	// routable from a Windows host. Direct mode keeps discovery on the
	// mapped endpoint while retaining replica-set transaction support.
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetDirect(true))
	if err != nil {
		t.Fatalf("connecting to MongoDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	return client.Database(fmt.Sprintf("spatial_test_%d", time.Now().UnixNano()))
}

func TestMongoCaptureRepository_CreateAndFindByID(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoCaptureRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Status != spatial.CaptureStatusDraft {
		t.Fatalf("expected draft, got %s", found.Status)
	}
}

func TestMongoCaptureRepository_FindByID_CrossTenantDenied(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoCaptureRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != spatial.ErrCaptureNotFound {
		t.Fatalf("expected ErrCaptureNotFound for cross-tenant read, got %v", err)
	}
}

// TestMongoCaptureRepository_ClientCaptureIDIdempotentCreate proves plan
// §RP3.5/§RP4B0's StartCapture idempotency at the repository level: a
// second Create call with the same (companyId, clientCaptureId) does not
// insert a second document — it returns the existing one, enforced by the
// real partial unique index (not merely application-level logic).
func TestMongoCaptureRepository_ClientCaptureIDIdempotentCreate(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoCaptureRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	first, err := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
		ClientCaptureID: "client_capture_A",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retry, err := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
		ClientCaptureID: "client_capture_A",
	})
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if retry.ID != first.ID {
		t.Fatalf("expected retry to return the same document ID %s, got %s", first.ID, retry.ID)
	}

	count, err := db.Collection("spatial_captures").CountDocuments(ctx, map[string]any{"clientCaptureId": "client_capture_A"})
	if err != nil {
		t.Fatalf("unexpected error counting documents: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 document with this clientCaptureId, got %d", count)
	}
}

// TestMongoCaptureRepository_EmptyClientCaptureIDNeverCollides proves the
// partial unique index does not treat two captures with no ClientCaptureID
// as duplicates of each other — critical for backward compatibility with
// every capture created before this field existed.
func TestMongoCaptureRepository_EmptyClientCaptureIDNeverCollides(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoCaptureRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	first, err := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("expected two distinct captures when ClientCaptureID is empty on both")
	}
}

// TestMongoCaptureRepository_ClientCaptureIDConcurrentCreate_OnlyOneWins
// proves the partial unique index is the AUTHORITATIVE race guard for two
// concurrent first attempts with the same ClientCaptureID — both racing
// past any application-level pre-check — matching the existing
// SetCurrentRoomVersion concurrent-writer test precedent.
func TestMongoCaptureRepository_ClientCaptureIDConcurrentCreate_OnlyOneWins(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoCaptureRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	const concurrentWriters = 10
	var wg sync.WaitGroup
	resultIDs := make([]string, concurrentWriters)
	errs := make([]error, concurrentWriters)

	for i := 0; i < concurrentWriters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			created, err := repo.Create(ctx, spatial.SpatialCapture{
				CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
				Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
				ClientCaptureID: "client_capture_concurrent",
			})
			resultIDs[i] = created.ID
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: unexpected error: %v", i, err)
		}
	}
	firstID := resultIDs[0]
	for i, id := range resultIDs {
		if id != firstID {
			t.Fatalf("expected every concurrent writer to observe the same winning capture ID %s, writer %d got %s", firstID, i, id)
		}
	}

	count, err := db.Collection("spatial_captures").CountDocuments(ctx, map[string]any{"clientCaptureId": "client_capture_concurrent"})
	if err != nil {
		t.Fatalf("unexpected error counting documents: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 document despite %d concurrent Create calls, got %d", concurrentWriters, count)
	}
}

// TestMongoCaptureRepository_ProviderAndCaptureNumberRoundTrip proves the
// plan §RP3 additive SpatialCapture fields (Provider, CaptureNumber,
// RoomDraftID) survive a real Mongo round-trip — required since RoomPlan
// multi-run persistence depends on these fields, not just in-memory state.
func TestMongoCaptureRepository_ProviderAndCaptureNumberRoundTrip(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoCaptureRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
		Provider: spatial.CaptureProviderRoomPlan, CaptureNumber: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Provider != spatial.CaptureProviderRoomPlan {
		t.Fatalf("expected provider roomplan, got %s", found.Provider)
	}
	if found.CaptureNumber != 2 {
		t.Fatalf("expected captureNumber 2, got %d", found.CaptureNumber)
	}
	if found.RoomDraftID != "" {
		t.Fatalf("expected empty RoomDraftID before SetRoomDraft, got %s", found.RoomDraftID)
	}

	updated, err := repo.SetRoomDraft(ctx, "company_a", created.ID, "draft_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.RoomDraftID != "draft_123" {
		t.Fatalf("expected RoomDraftID draft_123, got %s", updated.RoomDraftID)
	}

	found, err = repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.RoomDraftID != "draft_123" {
		t.Fatalf("expected persisted RoomDraftID draft_123, got %s", found.RoomDraftID)
	}
}

func TestMongoCaptureRepository_UpdateStatus_RejectsStaleExpectedStatus(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoCaptureRepository(db)
	ctx := context.Background()

	created, _ := repo.Create(ctx, spatial.SpatialCapture{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x",
		Status: spatial.CaptureStatusDraft, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})

	// Actual stored status is draft, but we claim expected=capturing.
	_, err := repo.UpdateStatus(ctx, "company_a", created.ID, spatial.CaptureStatusCapturing, spatial.CaptureStatusUploading)
	if err != spatial.ErrIllegalCaptureTransition {
		t.Fatalf("expected ErrIllegalCaptureTransition, got %v", err)
	}
}

func TestMongoRoomVersionRepository_CreateAndSupersede(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoRoomVersionRepository(db)
	ctx := context.Background()

	v1, err := repo.Create(ctx, spatial.SpatialRoomVersion{
		CompanyID: "company_a", ProjectID: "project_1", SpaceID: "space_x", CaptureID: "capture_1",
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v1.Status != spatial.RoomVersionStatusCurrent {
		t.Fatalf("expected current, got %s", v1.Status)
	}

	if err := repo.Supersede(ctx, "company_a", v1.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", v1.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Status != spatial.RoomVersionStatusSuperseded {
		t.Fatalf("expected superseded, got %s", found.Status)
	}
}

func TestMongoRoomVersionRepository_Supersede_NoOpWhenNotFound(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoRoomVersionRepository(db)
	ctx := context.Background()

	// Well-formed but nonexistent ObjectID hex.
	if err := repo.Supersede(ctx, "company_a", "64b64b64b64b64b64b64b64b"); err != nil {
		t.Fatalf("expected no-op success, got %v", err)
	}
}

func TestMongoSpaceStateRepository_FindOrCreateBySpace_Idempotent(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoSpaceStateRepository(db)
	ctx := context.Background()

	first, err := repo.FindOrCreateBySpace(ctx, "company_a", "project_1", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.Revision != 0 {
		t.Fatalf("expected revision 0 on creation, got %d", first.Revision)
	}

	second, err := repo.FindOrCreateBySpace(ctx, "company_a", "project_1", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected idempotent find, got different IDs %s vs %s", first.ID, second.ID)
	}
}

func TestMongoSpaceStateRepository_SetCurrentRoomVersion_RejectsStaleRevision(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoSpaceStateRepository(db)
	ctx := context.Background()

	state, _ := repo.FindOrCreateBySpace(ctx, "company_a", "project_1", "space_x")

	// Advance revision to 1 with a correct CAS write.
	updated, err := repo.SetCurrentRoomVersion(ctx, "company_a", "space_x", "version_1", state.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", updated.Revision)
	}

	// Retry with the now-stale expectedRevision=0 must fail.
	_, err = repo.SetCurrentRoomVersion(ctx, "company_a", "space_x", "version_2", 0)
	if err != spatial.ErrSpaceStateRevisionMismatch {
		t.Fatalf("expected ErrSpaceStateRevisionMismatch, got %v", err)
	}
}

// TestMongoSpaceStateRepository_ConcurrentSetCurrentRoomVersion_OnlyOneWins
// proves the CAS is enforced by MongoDB itself under real concurrent
// writers, not merely by single-threaded test sequencing (design spec
// §4.2's "atomic or strongly CAS-protected transition").
func TestMongoSpaceStateRepository_ConcurrentSetCurrentRoomVersion_OnlyOneWins(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoSpaceStateRepository(db)
	ctx := context.Background()

	state, _ := repo.FindOrCreateBySpace(ctx, "company_a", "project_1", "space_x")

	const concurrentWriters = 10
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < concurrentWriters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.SetCurrentRoomVersion(ctx, "company_a", "space_x", fmt.Sprintf("version_%d", i), state.Revision)
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			} else if err != spatial.ErrSpaceStateRevisionMismatch {
				t.Errorf("unexpected error from writer %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 writer to win the CAS race, got %d", successCount)
	}
}

// TestMongoRoomDraftRepository_CreateFindAndReopenPreservesStableIDs proves
// plan §RP3's core durability guarantee: a persisted RoomDraft's stable
// Renovex element IDs survive a Mongo round-trip unchanged — reopening a
// draft must never regenerate WallID/OpeningID/ObjectID identities.
func TestMongoRoomDraftRepository_CreateFindAndReopenPreservesStableIDs(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoRoomDraftRepository(db)
	ctx := context.Background()

	wallHeight := 2.4
	draft := spatial.RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls: []spatial.RoomDraftWall{
			{
				ID:              "wall_stable_1",
				Start:           spatial.RoomLocalPoint{X: 0, Y: 0, Z: 0},
				End:             spatial.RoomLocalPoint{X: 4, Y: 0, Z: 0},
				Height:          &wallHeight,
				ThicknessStatus: spatial.MeasurementStatusEstimated,
				Provenance: spatial.ElementProvenance{
					Provider: spatial.SourceProviderRoomPlan, SourceElementIdentifier: "roomplan-uuid-1",
				},
			},
		},
		SourceProvider: spatial.SourceProviderRoomPlan,
		CreatedAt:      time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}

	created, err := repo.Create(ctx, draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.Revision != 0 {
		t.Fatalf("expected initial revision 0, got %d", created.Revision)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(found.Walls) != 1 || found.Walls[0].ID != "wall_stable_1" {
		t.Fatalf("expected stable wall ID wall_stable_1 to survive reopen, got %+v", found.Walls)
	}
	if found.Walls[0].Provenance.SourceElementIdentifier != "roomplan-uuid-1" {
		t.Fatalf("expected provenance to survive reopen, got %+v", found.Walls[0].Provenance)
	}

	byCapture, err := repo.FindByCaptureID(ctx, "company_a", "capture_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if byCapture.ID != created.ID {
		t.Fatalf("expected FindByCaptureID to return the same draft, got a different ID")
	}
}

func TestMongoRoomDraftRepository_FindByCaptureID_NotFound(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoRoomDraftRepository(db)
	ctx := context.Background()

	_, err := repo.FindByCaptureID(ctx, "company_a", "no_such_capture")
	if err != spatial.ErrRoomDraftNotFound {
		t.Fatalf("expected ErrRoomDraftNotFound, got %v", err)
	}
}

// TestMongoRoomDraftRepository_Update_EditThenReopenPreservesEdits proves
// the Tier A/B requirement "edit RoomDraft -> persist -> reopen -> edits
// remain."
func TestMongoRoomDraftRepository_Update_EditThenReopenPreservesEdits(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoRoomDraftRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, spatial.RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls:          []spatial.RoomDraftWall{{ID: "wall_1", Start: spatial.RoomLocalPoint{}, End: spatial.RoomLocalPoint{X: 3}}},
		SourceProvider: spatial.SourceProviderRoomPlan,
		CreatedAt:      time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	editedHeight := 2.7
	created.Walls[0].Height = &editedHeight
	updated, err := repo.Update(ctx, "company_a", created.ID, created, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("expected revision to increment, got %d", updated.Revision)
	}

	reopened, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reopened.Walls[0].Height == nil || *reopened.Walls[0].Height != editedHeight {
		t.Fatalf("expected edited height %v to survive reopen, got %+v", editedHeight, reopened.Walls[0].Height)
	}
	if reopened.Walls[0].ID != "wall_1" {
		t.Fatalf("expected stable wall ID to survive edit, got %s", reopened.Walls[0].ID)
	}
}

func TestMongoRoomDraftRepository_Update_RejectsStaleRevision(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoRoomDraftRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, spatial.RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		SourceProvider: spatial.SourceProviderRoomPlan,
		CreatedAt:      time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	staleRevision := created.Revision + 99
	_, err = repo.Update(ctx, "company_a", created.ID, created, staleRevision)
	if err != spatial.ErrRoomDraftRevisionMismatch {
		t.Fatalf("expected ErrRoomDraftRevisionMismatch, got %v", err)
	}
}

// TestMongoRoomDraftRepository_RP4AFieldsRoundTrip proves plan §RP4A's new
// RoomDraft fields (extended opening metadata, Fixtures/ServicePoints/
// Constraints, the discriminated CreatedBy/Provenance invariant) survive a
// real Mongo round-trip — not just an in-memory fake.
func TestMongoRoomDraftRepository_RP4AFieldsRoundTrip(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoRoomDraftRepository(db)
	ctx := context.Background()

	archRise := 0.3
	created, err := repo.Create(ctx, spatial.RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Openings: []spatial.RoomDraftOpening{
			{
				ID: "opening_1", ParentWallID: "wall_1",
				Kind: spatial.OpeningKindArchway, Profile: spatial.OpeningProfileArch,
				ArchParameters: &spatial.ArchParameters{SpringHeight: 2.0, ArchRise: archRise},
				Provenance:     spatial.ElementProvenance{Provider: spatial.SourceProviderRoomPlan, SourceElementIdentifier: "src-1"},
			},
			{
				ID: "door_1", ParentWallID: "wall_1", Kind: spatial.OpeningKindDoor, Profile: spatial.OpeningProfileRectangle,
				Door:       &spatial.DoorMetadata{LeafCount: 2, Hinge: spatial.DoorHingeLeft, Swing: spatial.DoorSwingInward},
				Provenance: spatial.ElementProvenance{Provider: spatial.SourceProviderRoomPlan, SourceElementIdentifier: "src-2"},
			},
		},
		Fixtures: []spatial.RoomDraftFixture{
			{ID: "fixture_1", Category: spatial.FixtureCategoryBoiler, CreatedBy: spatial.ElementOriginContractor},
		},
		ServicePoints: []spatial.RoomDraftServicePoint{
			{ID: "sp_1", Kind: spatial.ServicePointKindElectrical, CreatedBy: spatial.ElementOriginContractor},
		},
		Constraints: []spatial.RoomDraftConstraint{
			{ID: "constraint_1", Kind: spatial.ConstraintKindColumn, CreatedBy: spatial.ElementOriginContractor},
		},
		SourceProvider: spatial.SourceProviderRoomPlan,
		CreatedAt:      time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(found.Openings) != 2 {
		t.Fatalf("expected 2 openings, got %d", len(found.Openings))
	}
	archOpening := found.Openings[0]
	if archOpening.Kind != spatial.OpeningKindArchway || archOpening.Profile != spatial.OpeningProfileArch {
		t.Fatalf("expected archway/arch, got %s/%s", archOpening.Kind, archOpening.Profile)
	}
	if archOpening.ArchParameters == nil || archOpening.ArchParameters.ArchRise != archRise {
		t.Fatalf("expected arch parameters to survive round-trip, got %+v", archOpening.ArchParameters)
	}

	doorOpening := found.Openings[1]
	if doorOpening.Door == nil || doorOpening.Door.LeafCount != 2 || doorOpening.Door.Hinge != spatial.DoorHingeLeft {
		t.Fatalf("expected door metadata to survive round-trip, got %+v", doorOpening.Door)
	}

	if len(found.Fixtures) != 1 || found.Fixtures[0].CreatedBy != spatial.ElementOriginContractor {
		t.Fatalf("expected fixture to survive round-trip, got %+v", found.Fixtures)
	}
	if found.Fixtures[0].Provenance != nil {
		t.Fatalf("expected nil provenance for contractor-created fixture to survive as nil, got %+v", found.Fixtures[0].Provenance)
	}
	if len(found.ServicePoints) != 1 || found.ServicePoints[0].Kind != spatial.ServicePointKindElectrical {
		t.Fatalf("expected service point to survive round-trip, got %+v", found.ServicePoints)
	}
	if len(found.Constraints) != 1 || found.Constraints[0].Kind != spatial.ConstraintKindColumn {
		t.Fatalf("expected constraint to survive round-trip, got %+v", found.Constraints)
	}
}
