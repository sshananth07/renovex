package materialrequirements_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// setupDB follows the M6 bootstrap convention (internal/access): raw
// mongo.Connect, a uniquely named database per test, TerminateContainer in
// cleanup. A unique DB matters here because several tests exercise unique
// indexes and must not observe each other's documents.
func setupDB(t *testing.T) *mongo.Database {
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

	return client.Database(fmt.Sprintf("materialrequirements_test_%d", time.Now().UnixNano()))
}

func newRepo(t *testing.T, db *mongo.Database) *mr.MongoMaterialRequirementRepository {
	t.Helper()
	repo := mr.NewMongoMaterialRequirementRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure indexes: %v", err)
	}
	return repo
}

func qty(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("bad quantity %q %q: %v", value, unit, err)
	}
	return q
}

// generatedAnchor builds a cost_item anchor with a full accepted source
// snapshot, so round-trip tests cover every persisted field group.
func generatedAnchor(t *testing.T, companyID, projectID, workItemID, materialID string) mr.MaterialRequirement {
	t.Helper()
	sourceQty := qty(t, "33.5", "m2")
	syncedAt := time.Now().UTC().Truncate(time.Millisecond)
	requiredBy := syncedAt.Add(72 * time.Hour)
	return mr.MaterialRequirement{
		CompanyID:             companyID,
		ProjectID:             projectID,
		WorkItemID:            &workItemID,
		MaterialID:            materialID,
		MaterialName:          "Ceramic Tiles",
		Specification:         "600x600 matte",
		RequiredQuantity:      qty(t, "33.5", "m2"),
		CatalogUnit:           "m2",
		RequiredByDate:        &requiredBy,
		ProcurementNotes:      "deliver to site gate",
		InternalNotes:         "contractor-only note",
		Status:                mr.RequirementStatusDraft,
		SourceType:            mr.SourceTypeCostItem,
		SourceAggregationUnit: "m2",
		SourceAggregationKey: mr.ComputeSourceAggregationKey(companyID, projectID,
			mr.WorkItemRef(workItemID), materialID, "m2"),
		SourceCostItemIDs: []string{"c1", "c2"},
		SourceQuantity:    &sourceQty,
		SourceFingerprint: mr.ComputeSourceFingerprint(projectID, mr.WorkItemRef(workItemID), materialID, "m2",
			[]mr.SourceRow{{CostItemID: "c1", Quantity: decimal.RequireFromString("25.5")},
				{CostItemID: "c2", Quantity: decimal.RequireFromString("8")}}),
		SourceSyncedAt:  &syncedAt,
		SourceSyncState: mr.SourceSyncStateClean,
		SourceCheckedAt: &syncedAt,
		CreatedByUserID: "user_1",
		CreatedAt:       syncedAt,
		UpdatedAt:       syncedAt,
		SchemaVersion:   1,
	}
}

// --- Indexes (design spec §12.1) ---

func TestMongoEnsureIndexesCreatesEveryNamedIndex(t *testing.T) {
	db := setupDB(t)
	newRepo(t, db)

	cursor, err := db.Collection("material_requirements").Indexes().List(context.Background())
	if err != nil {
		t.Fatalf("failed to list indexes: %v", err)
	}
	var specs []bson.M
	if err := cursor.All(context.Background(), &specs); err != nil {
		t.Fatalf("failed to decode indexes: %v", err)
	}

	found := map[string]bson.M{}
	for _, s := range specs {
		found[s["name"].(string)] = s
	}

	// All ten §12.1 indexes must exist under their EXPLICIT names, because
	// classifyCreateError matches duplicate-key errors by index name.
	for _, name := range []string{
		"idx_material_requirements_company",
		"idx_material_requirements_company_project",
		"idx_material_requirements_company_project_status",
		"idx_material_requirements_company_project_sourcetype",
		"idx_material_requirements_company_workitem",
		"idx_material_requirements_company_material",
		"uq_material_requirements_source_key",
		"uq_material_requirements_resolution_op",
		"idx_material_requirements_company_rfq_chain",
		"idx_material_requirements_company_split_group",
	} {
		if _, ok := found[name]; !ok {
			t.Errorf("missing index %q (have: %v)", name, keysOf(found))
		}
	}
}

func TestMongoSourceKeyIndexIsUniqueAndPartialOnCostItem(t *testing.T) {
	db := setupDB(t)
	newRepo(t, db)

	spec := indexSpec(t, db, "uq_material_requirements_source_key")
	if unique, _ := spec["unique"].(bool); !unique {
		t.Error("uq_material_requirements_source_key must be unique")
	}
	// Partial on sourceType=cost_item: manual requirements and split children
	// have no aggregation key and must not collide with each other on a missing
	// field (design spec §12.1).
	pfe := subdocument(t, spec["partialFilterExpression"])
	if pfe == nil {
		t.Fatalf("uq_material_requirements_source_key must be partial, got spec %v", spec)
	}
	if pfe["sourceType"] != string(mr.SourceTypeCostItem) {
		t.Errorf("partialFilterExpression = %v, want sourceType=cost_item", pfe)
	}
}

func TestMongoResolutionOpIndexIsUniqueAndPartial(t *testing.T) {
	db := setupDB(t)
	newRepo(t, db)

	spec := indexSpec(t, db, "uq_material_requirements_resolution_op")
	if unique, _ := spec["unique"].(bool); !unique {
		t.Error("uq_material_requirements_resolution_op must be unique")
	}
	// Partial on the field EXISTING as a string: the vast majority of
	// requirements carry no resolution operation id, and many nulls would
	// otherwise collide under a plain unique index.
	if subdocument(t, spec["partialFilterExpression"]) == nil {
		t.Fatalf("uq_material_requirements_resolution_op must be partial, got spec %v", spec)
	}
}

// --- Round trip (design spec §2) ---

func TestMongoCreateAndFindRoundTripsEveryField(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	want := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	created, err := repo.Create(ctx, want)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create must return the assigned ID")
	}

	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.CompanyID != want.CompanyID || got.ProjectID != want.ProjectID {
		t.Errorf("tenant/project mismatch: %+v", got)
	}
	if got.WorkItemID == nil || *got.WorkItemID != "work_1" {
		t.Errorf("WorkItemID = %v, want work_1", got.WorkItemID)
	}
	if got.MaterialID != "material_1" || got.MaterialName != "Ceramic Tiles" {
		t.Errorf("material fields wrong: %+v", got)
	}
	if got.Specification != "600x600 matte" {
		t.Errorf("Specification = %q", got.Specification)
	}
	// Quantities must survive as exact decimals.
	if !got.RequiredQuantity.Value.Equal(want.RequiredQuantity.Value) || got.RequiredQuantity.Unit != "m2" {
		t.Errorf("RequiredQuantity = %s %s, want 33.5 m2",
			got.RequiredQuantity.Value.String(), got.RequiredQuantity.Unit)
	}
	if got.SourceQuantity == nil || !got.SourceQuantity.Value.Equal(want.SourceQuantity.Value) {
		t.Errorf("SourceQuantity = %v", got.SourceQuantity)
	}
	if got.CatalogUnit != "m2" || got.SourceAggregationUnit != "m2" {
		t.Errorf("units wrong: catalog=%q aggregation=%q", got.CatalogUnit, got.SourceAggregationUnit)
	}
	if got.SourceAggregationKey != want.SourceAggregationKey {
		t.Errorf("SourceAggregationKey not round-tripped")
	}
	if got.SourceFingerprint != want.SourceFingerprint {
		t.Errorf("SourceFingerprint not round-tripped")
	}
	if len(got.SourceCostItemIDs) != 2 || got.SourceCostItemIDs[0] != "c1" {
		t.Errorf("SourceCostItemIDs = %v", got.SourceCostItemIDs)
	}
	if got.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q", got.SourceSyncState)
	}
	if got.SourceSyncedAt == nil || got.SourceCheckedAt == nil {
		t.Error("both source timestamps must round-trip")
	}
	if got.RequiredByDate == nil {
		t.Error("RequiredByDate must round-trip")
	}
	if got.ProcurementNotes != "deliver to site gate" || got.InternalNotes != "contractor-only note" {
		t.Errorf("notes wrong: %q / %q", got.ProcurementNotes, got.InternalNotes)
	}
	if got.Status != mr.RequirementStatusDraft || got.SourceType != mr.SourceTypeCostItem {
		t.Errorf("status/sourceType wrong: %q/%q", got.Status, got.SourceType)
	}
	if got.CreatedByUserID != "user_1" || got.SchemaVersion != 1 {
		t.Errorf("audit fields wrong: %+v", got)
	}
}

// Decimals are persisted as STRINGS. shopspring/decimal has no BSON codec, so
// reflection or Decimal128 could silently lose precision (mirrors the note in
// work/repository_mongo.go).
func TestMongoQuantitiesArePersistedAsStrings(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	anchor := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	anchor.RequiredQuantity = qty(t, "32.7501", "m2")
	created, err := repo.Create(ctx, anchor)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var raw bson.M
	objID, err := bson.ObjectIDFromHex(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Collection("material_requirements").
		FindOne(ctx, bson.M{"_id": objID}).Decode(&raw); err != nil {
		t.Fatalf("failed to read raw document: %v", err)
	}

	rq := subdocument(t, raw["requiredQuantity"])
	if rq == nil {
		t.Fatalf("requiredQuantity is not a subdocument: %T", raw["requiredQuantity"])
	}
	value, ok := rq["value"].(string)
	if !ok {
		t.Fatalf("requiredQuantity.value must be stored as a STRING, got %T (%v)", rq["value"], rq["value"])
	}
	if value != "32.7501" {
		t.Errorf("requiredQuantity.value = %q, want %q", value, "32.7501")
	}

	// And it must survive the round trip exactly.
	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequiredQuantity.Value.Equal(decimal.RequireFromString("32.7501")) {
		t.Errorf("round-tripped quantity = %s, want 32.7501", got.RequiredQuantity.Value.String())
	}
}

// A manual project-level requirement has no WorkItem and no aggregation
// identity; both must persist as absent rather than as empty strings.
func TestMongoManualRequirementPersistsNilWorkItemAndNoSourceIdentity(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	manual := mr.MaterialRequirement{
		CompanyID: "company_a", ProjectID: "project_1",
		WorkItemID: nil,
		MaterialID: "material_1", MaterialName: "Sand",
		RequiredQuantity: qty(t, "5", "m3"), CatalogUnit: "m3",
		Status: mr.RequirementStatusDraft, SourceType: mr.SourceTypeManual,
		SourceSyncState: mr.SourceSyncStateClean,
		CreatedByUserID: "user_1", CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}
	created, err := repo.Create(ctx, manual)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkItemID != nil {
		t.Errorf("WorkItemID = %v, want nil", *got.WorkItemID)
	}
	if got.SourceAggregationKey != "" || got.SourceAggregationUnit != "" {
		t.Errorf("a manual requirement must carry no aggregation identity, got %q/%q",
			got.SourceAggregationKey, got.SourceAggregationUnit)
	}
	if got.SourceQuantity != nil {
		t.Errorf("SourceQuantity = %v, want nil", got.SourceQuantity)
	}
}

// --- Tenant isolation (design spec §14) ---

func TestMongoFindByIDIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.FindByID(ctx, "company_b", created.ID); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("expected ErrMaterialRequirementNotFound for a foreign company, got %v", err)
	}
}

// A malformed ID is reported as not-found, never as a decode error, so a
// handler maps it to 404 rather than 500.
func TestMongoFindByIDMalformedIDIsNotFound(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)

	if _, err := repo.FindByID(context.Background(), "company_a", "not-an-object-id"); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("expected ErrMaterialRequirementNotFound, got %v", err)
	}
}

func TestMongoListByProjectIsTenantAndProjectScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_2", "work_2", "material_1")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, generatedAnchor(t, "company_b", "project_1", "work_3", "material_1")); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(got))
	}
	if got[0].ProjectID != "project_1" || got[0].CompanyID != "company_a" {
		t.Errorf("wrong requirement returned: %+v", got[0])
	}
}

// ListGeneratedAnchorsByProject backs the §3.4 union algorithm and must return
// cost_item anchors in EVERY status, including terminal ones — that is what
// prevents a split or archived anchor from being silently recreated.
func TestMongoListGeneratedAnchorsIncludesTerminalStatuses(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	for i, status := range []mr.RequirementStatus{
		mr.RequirementStatusDraft, mr.RequirementStatusReviewed,
		mr.RequirementStatusSplit, mr.RequirementStatusArchived,
	} {
		anchor := generatedAnchor(t, "company_a", "project_1", fmt.Sprintf("work_%d", i), "material_1")
		anchor.Status = status
		anchor.SourceAggregationKey = mr.ComputeSourceAggregationKey("company_a", "project_1",
			mr.WorkItemRef(fmt.Sprintf("work_%d", i)), "material_1", "m2")
		if _, err := repo.Create(ctx, anchor); err != nil {
			t.Fatalf("failed creating %s anchor: %v", status, err)
		}
	}
	// A manual requirement must NOT appear: it has no aggregation identity.
	manual := generatedAnchor(t, "company_a", "project_1", "work_manual", "material_1")
	manual.SourceType = mr.SourceTypeManual
	manual.SourceAggregationKey = ""
	manual.SourceAggregationUnit = ""
	if _, err := repo.Create(ctx, manual); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListGeneratedAnchorsByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 cost_item anchors across all statuses, got %d", len(got))
	}
	seen := map[mr.RequirementStatus]bool{}
	for _, a := range got {
		seen[a.Status] = true
		if a.SourceType != mr.SourceTypeCostItem {
			t.Errorf("non-cost_item anchor leaked in: %q", a.SourceType)
		}
	}
	for _, status := range []mr.RequirementStatus{
		mr.RequirementStatusSplit, mr.RequirementStatusArchived,
	} {
		if !seen[status] {
			t.Errorf("terminal anchor with status %q was omitted — it would be silently recreated", status)
		}
	}
}

// --- Duplicate-key classification (design spec §12.4) ---

func TestMongoDuplicateSourceAggregationKeyIsClassified(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	anchor := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	if _, err := repo.Create(ctx, anchor); err != nil {
		t.Fatal(err)
	}

	_, err := repo.Create(ctx, anchor)
	if !errors.Is(err, mr.ErrSourceAggregationKeyExists) {
		t.Fatalf("expected ErrSourceAggregationKeyExists, got %v", err)
	}
}

// The same aggregation key in a DIFFERENT company must not collide: the key
// itself embeds companyID, and the index leads with companyId.
func TestMongoSourceAggregationKeyIsScopedPerCompany(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, generatedAnchor(t, "company_b", "project_1", "work_1", "material_1")); err != nil {
		t.Fatalf("a different company must be able to hold the same logical key: %v", err)
	}
}

// Manual requirements carry no aggregation key, so many of them must coexist
// without colliding on a missing field — this is what the partial filter buys.
func TestMongoManyManualRequirementsDoNotCollide(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		manual := mr.MaterialRequirement{
			CompanyID: "company_a", ProjectID: "project_1",
			MaterialID: "material_1", MaterialName: "Sand",
			RequiredQuantity: qty(t, "5", "m3"), CatalogUnit: "m3",
			Status: mr.RequirementStatusDraft, SourceType: mr.SourceTypeManual,
			SourceSyncState: mr.SourceSyncStateClean,
			CreatedAt:       time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
		}
		if _, err := repo.Create(ctx, manual); err != nil {
			t.Fatalf("manual requirement %d must not collide: %v", i, err)
		}
	}
}

func TestMongoDuplicateResolutionOperationIDIsClassified(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	opID := "op_1"
	child := mr.MaterialRequirement{
		CompanyID: "company_a", ProjectID: "project_1",
		MaterialID: "material_1", MaterialName: "Sand",
		RequiredQuantity: qty(t, "5", "m3"), CatalogUnit: "m3",
		Status: mr.RequirementStatusDraft, SourceType: mr.SourceTypeManual,
		SourceSyncState:       mr.SourceSyncStateClean,
		ResolutionOperationID: &opID,
		CreatedAt:             time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}
	if _, err := repo.Create(ctx, child); err != nil {
		t.Fatal(err)
	}

	_, err := repo.Create(ctx, child)
	if !errors.Is(err, mr.ErrResolutionOperationAlreadyApplied) {
		t.Fatalf("expected ErrResolutionOperationAlreadyApplied, got %v", err)
	}
}

// An unclassifiable duplicate key must NOT be reported as a known sentinel: an
// error the code cannot positively identify is not safe to treat as retryable
// (design spec §12.4).
func TestMongoUnclassifiedDuplicateKeyIsReportedAsSuch(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	// Add an out-of-band unique index the production code knows nothing about.
	if _, err := db.Collection("material_requirements").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "materialName", Value: 1}},
		Options: options.Index().SetUnique(true).SetName("uq_test_only_material_name"),
	}); err != nil {
		t.Fatalf("failed to create the out-of-band index: %v", err)
	}

	first := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}

	// Same MaterialName, different aggregation key: only the unknown index trips.
	second := generatedAnchor(t, "company_a", "project_1", "work_2", "material_1")
	second.SourceAggregationKey = mr.ComputeSourceAggregationKey("company_a", "project_1",
		mr.WorkItemRef("work_2"), "material_1", "m2")

	_, err := repo.Create(ctx, second)
	if !errors.Is(err, mr.ErrUnclassifiedDuplicateKey) {
		t.Fatalf("expected ErrUnclassifiedDuplicateKey, got %v", err)
	}
	if errors.Is(err, mr.ErrSourceAggregationKeyExists) || errors.Is(err, mr.ErrResolutionOperationAlreadyApplied) {
		t.Fatal("an unknown duplicate key must never be reported as a known sentinel")
	}
}

// --- Conditional update / Revision guard (design spec §10) ---

func TestMongoUpdateContractorFieldsIncrementsRevision(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 0 {
		t.Fatalf("a new requirement must start at Revision 0, got %d", created.Revision)
	}

	updated := created
	updated.RequiredQuantity = qty(t, "40", "m2")
	updated.Status = mr.RequirementStatusDraft
	got, err := repo.UpdateContractorFields(ctx, "company_a", created.ID, 0, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Revision != 1 {
		t.Errorf("Revision = %d, want 1", got.Revision)
	}
	if !got.RequiredQuantity.Value.Equal(decimal.RequireFromString("40")) {
		t.Errorf("RequiredQuantity = %s, want 40", got.RequiredQuantity.Value.String())
	}
}

func TestMongoUpdateContractorFieldsRejectsStaleRevision(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateContractorFields(ctx, "company_a", created.ID, 0, created); err != nil {
		t.Fatal(err)
	}

	// Revision is now 1; replaying the original expectation must fail.
	_, err = repo.UpdateContractorFields(ctx, "company_a", created.ID, 0, created)
	if !errors.Is(err, mr.ErrRevisionMismatch) {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

// A 0-match must be distinguished: a genuinely missing document is
// ErrMaterialRequirementNotFound, not a revision conflict.
func TestMongoUpdateContractorFieldsUnknownIDIsNotFound(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	anchor := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	anchor.ID = bson.NewObjectID().Hex()
	_, err := repo.UpdateContractorFields(ctx, "company_a", anchor.ID, 0, anchor)
	if !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("expected ErrMaterialRequirementNotFound, got %v", err)
	}
}

func TestMongoUpdateContractorFieldsIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.UpdateContractorFields(ctx, "company_b", created.ID, 0, created)
	if !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("company_b must not be able to update company_a's requirement, got %v", err)
	}
}

func TestMongoDeleteRemovesTheDocument(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "company_a", created.ID, created.Revision); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := repo.FindByID(ctx, "company_a", created.ID); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("FindByID after delete: error = %v, want ErrMaterialRequirementNotFound", err)
	}
}

func TestMongoDeleteRejectsStaleRevision(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "company_a", created.ID, created.Revision+1); !errors.Is(err, mr.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
	if _, err := repo.FindByID(ctx, "company_a", created.ID); err != nil {
		t.Fatalf("document must survive a rejected delete: %v", err)
	}
}

// A 0-match must be distinguished: a genuinely missing document is
// ErrMaterialRequirementNotFound, not a revision conflict.
func TestMongoDeleteUnknownIDIsNotFound(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	if err := repo.Delete(ctx, "company_a", bson.NewObjectID().Hex(), 0); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("expected ErrMaterialRequirementNotFound, got %v", err)
	}
}

func TestMongoDeleteIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "company_b", created.ID, created.Revision); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("company_b must not be able to delete company_a's requirement, got %v", err)
	}
	if _, err := repo.FindByID(ctx, "company_a", created.ID); err != nil {
		t.Fatalf("document must survive a foreign-tenant delete attempt: %v", err)
	}
}

// Terminal requirements are read-only for contractor fields (design spec §2.1),
// enforced in the conditional filter rather than only in the service.
func TestMongoUpdateContractorFieldsRejectsTerminalStatus(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	for _, status := range []mr.RequirementStatus{mr.RequirementStatusSplit, mr.RequirementStatusArchived} {
		anchor := generatedAnchor(t, "company_a", "project_1", "work_"+string(status), "material_1")
		anchor.Status = status
		anchor.SourceAggregationKey = mr.ComputeSourceAggregationKey("company_a", "project_1",
			mr.WorkItemRef("work_"+string(status)), "material_1", "m2")
		created, err := repo.Create(ctx, anchor)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := repo.UpdateContractorFields(ctx, "company_a", created.ID, 0, created); err == nil {
			t.Errorf("status %q: a terminal requirement must reject a contractor update", status)
		}
	}
}

// --- Detection-only convergence (design spec §5.8) ---

// UpdateSyncState may write ONLY the two detection fields. The accepted
// snapshot must survive untouched, because merge computes proposed − accepted.
func TestMongoUpdateSyncStateLeavesAcceptedSnapshotIntact(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}
	acceptedFingerprint := created.SourceFingerprint
	acceptedQty := created.SourceQuantity.Value
	acceptedIDs := append([]string{}, created.SourceCostItemIDs...)
	acceptedSyncedAt := *created.SourceSyncedAt

	checkedAt := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	got, err := repo.UpdateSyncState(ctx, "company_a", created.ID, created.Revision,
		mr.SourceSyncStateChangeDetected, checkedAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.SourceSyncState != mr.SourceSyncStateChangeDetected {
		t.Errorf("SourceSyncState = %q, want change_detected", got.SourceSyncState)
	}
	if got.SourceCheckedAt == nil || !got.SourceCheckedAt.Equal(checkedAt) {
		t.Errorf("SourceCheckedAt = %v, want %v", got.SourceCheckedAt, checkedAt)
	}
	// The four accepted-snapshot fields must be byte-identical.
	if got.SourceFingerprint != acceptedFingerprint {
		t.Error("detection must not rewrite SourceFingerprint")
	}
	if !got.SourceQuantity.Value.Equal(acceptedQty) {
		t.Errorf("detection must not rewrite SourceQuantity: %s != %s",
			got.SourceQuantity.Value.String(), acceptedQty.String())
	}
	if len(got.SourceCostItemIDs) != len(acceptedIDs) || got.SourceCostItemIDs[0] != acceptedIDs[0] {
		t.Error("detection must not rewrite SourceCostItemIDs")
	}
	if !got.SourceSyncedAt.Equal(acceptedSyncedAt) {
		t.Error("detection must not rewrite SourceSyncedAt")
	}
	if !got.RequiredQuantity.Value.Equal(created.RequiredQuantity.Value) {
		t.Error("detection must not rewrite RequiredQuantity")
	}
	if got.Status != created.Status {
		t.Error("detection must not rewrite Status")
	}
}

// Detection may converge sync state even on a TERMINAL requirement — split and
// archived anchors remain sync anchors (design spec §3.4, §5.8).
func TestMongoUpdateSyncStateWorksOnTerminalRequirement(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	anchor := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	anchor.Status = mr.RequirementStatusSplit
	created, err := repo.Create(ctx, anchor)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.UpdateSyncState(ctx, "company_a", created.ID, created.Revision,
		mr.SourceSyncStateSourceRemoved, time.Now())
	if err != nil {
		t.Fatalf("a split anchor must still accept sync-state convergence: %v", err)
	}
	if got.SourceSyncState != mr.SourceSyncStateSourceRemoved {
		t.Errorf("SourceSyncState = %q, want source_removed", got.SourceSyncState)
	}
	if got.Status != mr.RequirementStatusSplit {
		t.Errorf("Status = %q, want split (unchanged)", got.Status)
	}
}

// --- helpers ---

func indexSpec(t *testing.T, db *mongo.Database, name string) bson.M {
	t.Helper()
	cursor, err := db.Collection("material_requirements").Indexes().List(context.Background())
	if err != nil {
		t.Fatalf("failed to list indexes: %v", err)
	}
	var specs []bson.M
	if err := cursor.All(context.Background(), &specs); err != nil {
		t.Fatalf("failed to decode indexes: %v", err)
	}
	for _, s := range specs {
		if s["name"] == name {
			return s
		}
	}
	t.Fatalf("index %q not found", name)
	return nil
}

// subdocument normalizes a decoded BSON subdocument into a map. The driver may
// hand back either bson.M or bson.D depending on the decode path — index specs
// arrive as bson.D, so asserting bson.M alone would fail on a correct document.
// Returns nil when the value is neither.
func subdocument(t *testing.T, v any) map[string]any {
	t.Helper()
	switch typed := v.(type) {
	case bson.M:
		return typed
	case bson.D:
		out := make(map[string]any, len(typed))
		for _, e := range typed {
			out[e.Key] = e.Value
		}
		return out
	case map[string]any:
		return typed
	default:
		return nil
	}
}

func keysOf(m map[string]bson.M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ApplyDiscrepancyResolution is the counterpart to UpdateSyncState: it is the
// ONLY write permitted to advance the accepted source snapshot (design spec
// §5.8). This proves it actually persists all four snapshot fields together
// with the contractor fields a resolution may move.
func TestMongoApplyDiscrepancyResolutionAdvancesAcceptedSnapshot(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1"))
	if err != nil {
		t.Fatal(err)
	}

	newQty := qty(t, "41", "m2")
	syncedAt := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	resolved := created
	resolved.SourceCostItemIDs = []string{"c1", "c2", "c3"}
	resolved.SourceQuantity = &newQty
	resolved.SourceFingerprint = "resolved-fingerprint"
	resolved.SourceSyncedAt = &syncedAt
	resolved.SourceCheckedAt = &syncedAt
	resolved.SourceSyncState = mr.SourceSyncStateClean
	resolved.RequiredQuantity = qty(t, "43", "m2")
	resolved.Status = mr.RequirementStatusDraft

	got, err := repo.ApplyDiscrepancyResolution(ctx, "company_a", created.ID, created.Revision, resolved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.SourceFingerprint != "resolved-fingerprint" {
		t.Errorf("SourceFingerprint = %q, want the resolved value", got.SourceFingerprint)
	}
	if !got.SourceQuantity.Value.Equal(newQty.Value) {
		t.Errorf("SourceQuantity = %s, want 41", got.SourceQuantity.Value.String())
	}
	if len(got.SourceCostItemIDs) != 3 {
		t.Errorf("SourceCostItemIDs = %v, want three ids", got.SourceCostItemIDs)
	}
	if got.SourceSyncedAt == nil || !got.SourceSyncedAt.Equal(syncedAt) {
		t.Errorf("SourceSyncedAt = %v, want %v", got.SourceSyncedAt, syncedAt)
	}
	if got.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean", got.SourceSyncState)
	}
	// Decimals round-trip through their string encoding without precision loss.
	if !got.RequiredQuantity.Value.Equal(decimal.RequireFromString("43")) {
		t.Errorf("RequiredQuantity = %s, want 43", got.RequiredQuantity.Value.String())
	}
	if got.Revision != created.Revision+1 {
		t.Errorf("Revision = %d, want %d", got.Revision, created.Revision+1)
	}
}

// A claimed requirement is rejected by the filter itself, not merely by the
// service — §2.3 freezes every contractor field while a claim exists.
func TestMongoApplyDiscrepancyResolutionRejectsClaimed(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	anchor := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	chainID := "rfq_chain_1"
	anchor.ActiveRFQChainID = &chainID
	created, err := repo.Create(ctx, anchor)
	if err != nil {
		t.Fatal(err)
	}

	resolved := created
	resolved.SourceFingerprint = "should-not-persist"
	if _, err := repo.ApplyDiscrepancyResolution(ctx, "company_a", created.ID,
		created.Revision, resolved); !errors.Is(err, mr.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch for a claimed requirement", err)
	}

	unchanged, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.SourceFingerprint != created.SourceFingerprint {
		t.Error("a rejected resolution must not persist any field")
	}
}

// keep_current is valid on a terminal anchor, so unlike UpdateContractorFields
// this write must NOT filter terminal status out (design spec §5.6).
func TestMongoApplyDiscrepancyResolutionWorksOnTerminalAnchor(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	anchor := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	anchor.Status = mr.RequirementStatusSplit
	created, err := repo.Create(ctx, anchor)
	if err != nil {
		t.Fatal(err)
	}

	resolved := created
	resolved.SourceFingerprint = "resolved-on-terminal"
	resolved.SourceSyncState = mr.SourceSyncStateClean

	got, err := repo.ApplyDiscrepancyResolution(ctx, "company_a", created.ID, created.Revision, resolved)
	if err != nil {
		t.Fatalf("keep_current on a terminal anchor must succeed, got %v", err)
	}
	if got.SourceFingerprint != "resolved-on-terminal" {
		t.Error("the accepted snapshot was not advanced on a terminal anchor")
	}
	if got.Status != mr.RequirementStatusSplit {
		t.Errorf("Status = %q, want split preserved — a resolution must not revive a terminal anchor",
			got.Status)
	}
}

// FindByResolutionOperationID backs §5.7's VERIFIED retry, and is scoped by
// company so a foreign tenant's operation ID can never resolve here.
func TestMongoFindByResolutionOperationIDIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	opID := "op_1"
	child := generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")
	child.SourceType = mr.SourceTypeManual
	child.SourceAggregationKey = ""
	child.ResolutionOperationID = &opID
	created, err := repo.Create(ctx, child)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByResolutionOperationID(ctx, "company_a", opID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("found %q, want %q", got.ID, created.ID)
	}

	if _, err := repo.FindByResolutionOperationID(ctx, "company_b", opID); !errors.Is(err,
		mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want not-found for a foreign company", err)
	}
}

// The unique partial index on {companyId, resolutionOperationId} is what makes
// create_separate non-duplicating. Without it a retry would create a second
// child and duplicate procurement demand (design spec §5.7, §12.1).
func TestMongoResolutionOperationIDIsUniquePerCompany(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	opID := "op_dup"
	makeChild := func(companyID string) mr.MaterialRequirement {
		c := generatedAnchor(t, companyID, "project_1", "work_1", "material_1")
		c.SourceType = mr.SourceTypeManual
		c.SourceAggregationKey = ""
		c.ResolutionOperationID = &opID
		return c
	}

	if _, err := repo.Create(ctx, makeChild("company_a")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, makeChild("company_a")); !errors.Is(err,
		mr.ErrResolutionOperationAlreadyApplied) {
		t.Fatalf("error = %v, want ErrResolutionOperationAlreadyApplied", err)
	}
	// The index is per company, so another tenant may reuse the same string.
	if _, err := repo.Create(ctx, makeChild("company_b")); err != nil {
		t.Errorf("a different company must be able to reuse an operation id: %v", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the repository directly (matching
// every sibling test in this file) using the generatedAnchor helper.
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	svc := mr.NewService(repo, nil, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, generatedAnchor(t, "company_a", "project_1", "work_1", "material_1")); err != nil {
		t.Fatalf("create company_a requirement: %v", err)
	}
	if _, err := repo.Create(ctx, generatedAnchor(t, "company_b", "project_2", "work_2", "material_2")); err != nil {
		t.Fatalf("create company_b requirement: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	listA, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 remaining company_a requirements, got %d", len(listA))
	}

	listB, err := repo.ListByProject(ctx, "company_b", "project_2")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected company_b's requirement to be untouched, got %d", len(listB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	svc := mr.NewService(repo, nil, nil, nil, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
