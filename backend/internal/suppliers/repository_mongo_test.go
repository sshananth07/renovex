package suppliers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// D2 covers the three supplier repositories against a real MongoDB
// (design spec §4, §12.3).

// setupDB follows the M6 bootstrap convention: raw mongo.Connect, a uniquely
// named database per test, TerminateContainer in cleanup.
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

	return client.Database(fmt.Sprintf("suppliers_test_%d", time.Now().UnixNano()))
}

func newSupplierRepo(t *testing.T, db *mongo.Database) *suppliers.MongoSupplierRepository {
	t.Helper()
	repo := suppliers.NewMongoSupplierRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure supplier indexes: %v", err)
	}
	return repo
}

func newOfferingRepo(t *testing.T, db *mongo.Database) *suppliers.MongoSupplierOfferingRepository {
	t.Helper()
	repo := suppliers.NewMongoSupplierOfferingRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure offering indexes: %v", err)
	}
	return repo
}

func newPreferenceRepo(t *testing.T, db *mongo.Database) *suppliers.MongoPreferenceRepository {
	t.Helper()
	repo := suppliers.NewMongoPreferenceRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure preference indexes: %v", err)
	}
	return repo
}

func newSupplier(t *testing.T, companyID, name string) suppliers.Supplier {
	t.Helper()
	normalized, err := suppliers.NormalizeSupplierName(name)
	if err != nil {
		t.Fatalf("bad supplier name: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	return suppliers.Supplier{
		CompanyID: companyID, Name: name, NameNormalized: normalized,
		ContactPerson: "Aminah", Email: "sales@abc.example", Phone: "+60 3-1234 5678",
		Address:            "12 Jalan Industri",
		MaterialCategories: []string{"Cement", "Tiles"},
		Notes:              "opens 8am",
		Active:             true,
		CreatedByUserID:    "user_1",
		CreatedAt:          now, UpdatedAt: now, SchemaVersion: 1,
	}
}

// --- Supplier round-trip ---

func TestMongoSupplierRoundTripsEveryField(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials"))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("Create did not assign an id")
	}

	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "ABC Materials" || got.NameNormalized != "abc materials" {
		t.Errorf("name round-trip failed: %+v", got)
	}
	if got.ContactPerson != "Aminah" || got.Email != "sales@abc.example" ||
		got.Phone != "+60 3-1234 5678" || got.Address != "12 Jalan Industri" {
		t.Errorf("contact fields lost: %+v", got)
	}
	if len(got.MaterialCategories) != 2 || got.MaterialCategories[0] != "Cement" {
		t.Errorf("MaterialCategories = %v, want [Cement Tiles] in order", got.MaterialCategories)
	}
	if !got.Active {
		t.Error("Active was not preserved")
	}
	// Phone is a trimmed STRING, never numeric — a leading + must survive.
	if got.Phone[0] != '+' {
		t.Errorf("Phone = %q, want a leading + preserved (never stored numerically)", got.Phone)
	}
}

// A supplier with ONLY a name is creatable: contact details are optional
// because M7 contacts no one (acceptance test 81).
func TestMongoSupplierWithOnlyANameIsCreatable(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	minimal := suppliers.Supplier{
		CompanyID: "company_a", Name: "Solo", NameNormalized: "solo",
		Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}
	created, err := repo.Create(ctx, minimal)
	if err != nil {
		t.Fatalf("a name-only supplier must be creatable: %v", err)
	}
	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "" || got.Phone != "" || got.ContactPerson != "" {
		t.Errorf("empty contact fields were not preserved as empty: %+v", got)
	}
}

// --- Uniqueness INCLUDING inactive (design spec §4.1, acceptance test 79) ---

// The unique index is NOT partial on Active. This prevents retirement followed
// by accidental duplication: one Supplier record is one commercial entity.
func TestMongoSupplierNameIsUniqueIncludingInactiveRecords(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	first, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials"))
	if err != nil {
		t.Fatal(err)
	}

	// An ACTIVE collision is refused.
	if _, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials")); !errors.Is(
		err, suppliers.ErrSupplierNameTaken) {
		t.Fatalf("active collision: error = %v, want ErrSupplierNameTaken", err)
	}

	// Retire the original.
	retired, err := repo.SetActive(ctx, "company_a", first.ID, first.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Active {
		t.Fatal("SetActive(false) did not retire the supplier")
	}

	// An INACTIVE collision is refused too — the whole point of the rule.
	if _, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials")); !errors.Is(
		err, suppliers.ErrSupplierNameTaken) {
		t.Fatalf("inactive collision: error = %v, want ErrSupplierNameTaken — the index must "+
			"NOT be partial on Active, or retirement enables duplication", err)
	}
}

// Differently-spelled but identically-normalized names collide
// (acceptance test 80).
func TestMongoSupplierUniquenessUsesTheNormalizedKey(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, newSupplier(t, "company_a", "  ABC   Materials ")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, newSupplier(t, "company_a", "abc materials")); !errors.Is(
		err, suppliers.ErrSupplierNameTaken) {
		t.Fatalf("error = %v, want ErrSupplierNameTaken", err)
	}
}

// Uniqueness is per company.
func TestMongoSupplierNameUniquenessIsPerCompany(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, newSupplier(t, "company_b", "ABC Materials")); err != nil {
		t.Errorf("another company must be able to use the same name: %v", err)
	}
}

// The unique index must be enforced by MongoDB, not merely by the repository.
func TestMongoSupplierUniqueIndexIsEnforcedAtTheDatabase(t *testing.T) {
	db := setupDB(t)
	newSupplierRepo(t, db)
	ctx := context.Background()

	doc := bson.M{
		"companyId": "company_a", "name": "ABC Materials", "nameNormalized": "abc materials",
		"active": true, "revision": int64(0), "schemaVersion": 1,
		"createdAt": time.Now(), "updatedAt": time.Now(),
	}
	if _, err := db.Collection("suppliers").InsertOne(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection("suppliers").InsertOne(ctx, doc); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("error = %v, want a duplicate-key error from the database itself", err)
	}
}

// --- Tenant scoping and listing ---

func TestMongoSupplierFindByIDIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(ctx, "company_b", created.ID); !errors.Is(err,
		suppliers.ErrSupplierNotFound) {
		t.Fatalf("error = %v, want ErrSupplierNotFound", err)
	}
}

func TestMongoSupplierListFiltersByQueryCategoryAndActive(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	cement := newSupplier(t, "company_a", "Cement Depot")
	cement.MaterialCategories = []string{"Cement"}
	if _, err := repo.Create(ctx, cement); err != nil {
		t.Fatal(err)
	}

	tiles := newSupplier(t, "company_a", "Tile World")
	tiles.MaterialCategories = []string{"Tiles"}
	created, err := repo.Create(ctx, tiles)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetActive(ctx, "company_a", created.ID, created.Revision, false); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Create(ctx, newSupplier(t, "company_b", "Other Tenant")); err != nil {
		t.Fatal(err)
	}

	t.Run("no filters lists the company's suppliers only", func(t *testing.T) {
		got, err := repo.List(ctx, "company_a", suppliers.SupplierFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d suppliers, want 2", len(got))
		}
		for _, s := range got {
			if s.CompanyID != "company_a" {
				t.Errorf("listing leaked %+v", s)
			}
		}
	})

	t.Run("name query", func(t *testing.T) {
		got, err := repo.List(ctx, "company_a", suppliers.SupplierFilter{Query: "cement"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "Cement Depot" {
			t.Fatalf("got %+v, want just Cement Depot", got)
		}
	})

	t.Run("category filter", func(t *testing.T) {
		got, err := repo.List(ctx, "company_a", suppliers.SupplierFilter{Category: "Tiles"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "Tile World" {
			t.Fatalf("got %+v, want just Tile World", got)
		}
	})

	t.Run("active filter", func(t *testing.T) {
		active := true
		got, err := repo.List(ctx, "company_a", suppliers.SupplierFilter{Active: &active})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "Cement Depot" {
			t.Fatalf("got %+v, want just the active supplier", got)
		}

		inactive := false
		got, err = repo.List(ctx, "company_a", suppliers.SupplierFilter{Active: &inactive})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Name != "Tile World" {
			t.Fatalf("got %+v, want just the retired supplier", got)
		}
	})
}

// --- Supplier conditional updates ---

func TestMongoSupplierUpdateIsRevisionGuarded(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials"))
	if err != nil {
		t.Fatal(err)
	}

	updated := created
	updated.ContactPerson = "Siti"
	updated.Notes = "closed Sundays"

	got, err := repo.Update(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContactPerson != "Siti" || got.Notes != "closed Sundays" {
		t.Errorf("update did not apply: %+v", got)
	}
	if got.Revision != created.Revision+1 {
		t.Errorf("Revision = %d, want an increment", got.Revision)
	}

	if _, err := repo.Update(ctx, "company_a", created.ID, created.Revision,
		updated); !errors.Is(err, suppliers.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// Renaming into an existing normalized name collides, active or not.
func TestMongoSupplierRenameIntoAnExistingNameIsRefused(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials")); err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(ctx, newSupplier(t, "company_a", "XYZ Supplies"))
	if err != nil {
		t.Fatal(err)
	}

	renamed := second
	renamed.Name = "abc materials"
	renamed.NameNormalized = "abc materials"

	if _, err := repo.Update(ctx, "company_a", second.ID, second.Revision,
		renamed); !errors.Is(err, suppliers.ErrSupplierNameTaken) {
		t.Fatalf("error = %v, want ErrSupplierNameTaken", err)
	}
}

// Retirement is soft: SetActive flips the flag and nothing is deleted.
func TestMongoSupplierSetActiveIsSoftAndRevisionGuarded(t *testing.T) {
	db := setupDB(t)
	repo := newSupplierRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newSupplier(t, "company_a", "ABC Materials"))
	if err != nil {
		t.Fatal(err)
	}

	retired, err := repo.SetActive(ctx, "company_a", created.ID, created.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Active {
		t.Error("the supplier was not retired")
	}
	// The record still exists — no hard delete.
	if _, err := repo.FindByID(ctx, "company_a", created.ID); err != nil {
		t.Errorf("retirement removed the record: %v", err)
	}

	reactivated, err := repo.SetActive(ctx, "company_a", retired.ID, retired.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reactivated.Active {
		t.Error("the supplier was not reactivated")
	}

	if _, err := repo.SetActive(ctx, "company_a", created.ID, created.Revision,
		false); !errors.Is(err, suppliers.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// --- Offerings ---

func newOffering(t *testing.T, companyID, supplierID string) suppliers.SupplierOffering {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Millisecond)
	price := money.New(2550, "MYR")
	materialID := "material_1"
	image := "https://cdn.example.com/cement.png"
	product := "https://example.com/products/cement"
	return suppliers.SupplierOffering{
		CompanyID: companyID, SupplierID: supplierID, MaterialID: &materialID,
		ProductName: "OPC Cement 50kg", Brand: "BrandCo", SKU: "SKU-1",
		Description: "ordinary portland cement", Category: "Cement", Unit: "bag",
		ImageURL: &image, ProductURL: &product,
		IndicativePrice: &price, IndicativePriceAsOf: &now,
		Active:          true,
		CreatedByUserID: "user_1",
		CreatedAt:       now, UpdatedAt: now, SchemaVersion: 1,
	}
}

func TestMongoOfferingRoundTripsIncludingMoney(t *testing.T) {
	db := setupDB(t)
	repo := newOfferingRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newOffering(t, "company_a", "supplier_1"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.ProductName != "OPC Cement 50kg" || got.SKU != "SKU-1" || got.Unit != "bag" {
		t.Errorf("descriptive fields lost: %+v", got)
	}
	// Money is int64 minor units + currency, never a float (ADR 0001).
	if got.IndicativePrice == nil || got.IndicativePrice.Amount != 2550 ||
		got.IndicativePrice.Currency != "MYR" {
		t.Errorf("IndicativePrice = %+v, want 2550 MYR", got.IndicativePrice)
	}
	if got.IndicativePriceAsOf == nil {
		t.Error("IndicativePriceAsOf was not preserved")
	}
	if got.MaterialID == nil || *got.MaterialID != "material_1" {
		t.Errorf("MaterialID = %v, want material_1", got.MaterialID)
	}
	if got.ImageURL == nil || got.ProductURL == nil {
		t.Errorf("urls lost: %+v / %+v", got.ImageURL, got.ProductURL)
	}
}

// An offering with no price and no material link round-trips as nil, not as a
// zero value — nil is what expresses "unknown" (design spec §4.2).
func TestMongoOfferingPreservesNilPriceAndNilMaterial(t *testing.T) {
	db := setupDB(t)
	repo := newOfferingRepo(t, db)
	ctx := context.Background()

	o := newOffering(t, "company_a", "supplier_1")
	o.MaterialID = nil
	o.IndicativePrice = nil
	o.IndicativePriceAsOf = nil
	o.ImageURL = nil
	o.ProductURL = nil

	created, err := repo.Create(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.IndicativePrice != nil {
		t.Errorf("IndicativePrice = %+v, want nil — zero never means unknown", got.IndicativePrice)
	}
	if got.IndicativePriceAsOf != nil || got.MaterialID != nil ||
		got.ImageURL != nil || got.ProductURL != nil {
		t.Errorf("a nil optional round-tripped as non-nil: %+v", got)
	}
}

func TestMongoOfferingListFiltersBySupplierMaterialAndActive(t *testing.T) {
	db := setupDB(t)
	repo := newOfferingRepo(t, db)
	ctx := context.Background()

	first, err := repo.Create(ctx, newOffering(t, "company_a", "supplier_1"))
	if err != nil {
		t.Fatal(err)
	}

	second := newOffering(t, "company_a", "supplier_2")
	otherMaterial := "material_2"
	second.MaterialID = &otherMaterial
	created2, err := repo.Create(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetActive(ctx, "company_a", created2.ID, created2.Revision, false); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Create(ctx, newOffering(t, "company_b", "supplier_1")); err != nil {
		t.Fatal(err)
	}

	got, err := repo.List(ctx, "company_a", suppliers.OfferingFilter{SupplierID: "supplier_1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("supplier filter: got %d, want just the first offering", len(got))
	}

	got, err = repo.List(ctx, "company_a", suppliers.OfferingFilter{MaterialID: "material_2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != created2.ID {
		t.Fatalf("material filter: got %d, want just the second offering", len(got))
	}

	active := true
	got, err = repo.List(ctx, "company_a", suppliers.OfferingFilter{Active: &active})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("active filter: got %d, want just the active offering", len(got))
	}
}

func TestMongoOfferingUpdateIsRevisionGuarded(t *testing.T) {
	db := setupDB(t)
	repo := newOfferingRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newOffering(t, "company_a", "supplier_1"))
	if err != nil {
		t.Fatal(err)
	}

	updated := created
	updated.ProductName = "OPC Cement 40kg"
	got, err := repo.Update(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProductName != "OPC Cement 40kg" {
		t.Errorf("update did not apply: %+v", got)
	}
	// SupplierID is immutable, so the repository never writes it.
	if got.SupplierID != created.SupplierID {
		t.Errorf("SupplierID = %q, want %q — it is immutable after creation",
			got.SupplierID, created.SupplierID)
	}

	if _, err := repo.Update(ctx, "company_a", created.ID, created.Revision,
		updated); !errors.Is(err, suppliers.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// Even when a caller passes a different SupplierID, the write must not move the
// offering between suppliers (design spec §4.2, acceptance test 85).
func TestMongoOfferingUpdateNeverWritesSupplierID(t *testing.T) {
	db := setupDB(t)
	repo := newOfferingRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newOffering(t, "company_a", "supplier_1"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := created
	tampered.SupplierID = "supplier_hijack"

	got, err := repo.Update(ctx, "company_a", created.ID, created.Revision, tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got.SupplierID != "supplier_1" {
		t.Errorf("SupplierID = %q, want supplier_1 — the repository must not write it at all",
			got.SupplierID)
	}
}

func TestMongoOfferingIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newOfferingRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, newOffering(t, "company_a", "supplier_1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(ctx, "company_b", created.ID); !errors.Is(err,
		suppliers.ErrSupplierOfferingNotFound) {
		t.Fatalf("error = %v, want ErrSupplierOfferingNotFound", err)
	}
}

// --- Preference (design spec §4.3) ---

func TestMongoPreferenceUpsertReplacesUnderRevisionGuard(t *testing.T) {
	db := setupDB(t)
	repo := newPreferenceRepo(t, db)
	ctx := context.Background()

	first, err := repo.Upsert(ctx, suppliers.MaterialSupplierPreference{
		CompanyID: "company_a", MaterialID: "material_1", SupplierID: "supplier_1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	// A second PUT replaces the first; one preference per material persists.
	second, err := repo.Upsert(ctx, suppliers.MaterialSupplierPreference{
		CompanyID: "company_a", MaterialID: "material_1", SupplierID: "supplier_2",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}, first.Revision)
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	if second.SupplierID != "supplier_2" {
		t.Errorf("SupplierID = %q, want supplier_2", second.SupplierID)
	}
	if second.Revision != first.Revision+1 {
		t.Errorf("Revision = %d, want an increment", second.Revision)
	}

	got, err := repo.FindByMaterial(ctx, "company_a", "material_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SupplierID != "supplier_2" {
		t.Errorf("stored preference = %q, want supplier_2", got.SupplierID)
	}

	// A stale revision is refused.
	if _, err := repo.Upsert(ctx, suppliers.MaterialSupplierPreference{
		CompanyID: "company_a", MaterialID: "material_1", SupplierID: "supplier_3",
	}, first.Revision); !errors.Is(err, suppliers.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// Unique {companyId, materialId}: at most one preferred supplier per material.
func TestMongoPreferenceUniquePerMaterial(t *testing.T) {
	db := setupDB(t)
	newPreferenceRepo(t, db)
	ctx := context.Background()

	doc := bson.M{
		"companyId": "company_a", "materialId": "material_1", "supplierId": "supplier_1",
		"revision": int64(0), "schemaVersion": 1,
		"createdAt": time.Now(), "updatedAt": time.Now(),
	}
	if _, err := db.Collection("material_supplier_preferences").InsertOne(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection("material_supplier_preferences").InsertOne(ctx,
		doc); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("error = %v, want a duplicate-key error from the database", err)
	}
}

// One supplier may be preferred for MANY materials.
func TestMongoPreferenceAllowsOneSupplierAcrossManyMaterials(t *testing.T) {
	db := setupDB(t)
	repo := newPreferenceRepo(t, db)
	ctx := context.Background()

	for _, materialID := range []string{"material_1", "material_2", "material_3"} {
		if _, err := repo.Upsert(ctx, suppliers.MaterialSupplierPreference{
			CompanyID: "company_a", MaterialID: materialID, SupplierID: "supplier_1",
			CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
		}, 0); err != nil {
			t.Fatalf("%s: %v", materialID, err)
		}
	}

	got, err := repo.ListBySupplier(ctx, "company_a", "supplier_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d preferences, want 3", len(got))
	}
}

// DELETE may physically remove the relationship — it is advisory only, and
// audit preserves the history (design spec §4.3, acceptance test 96).
func TestMongoPreferenceDeleteRemovesTheRecord(t *testing.T) {
	db := setupDB(t)
	repo := newPreferenceRepo(t, db)
	ctx := context.Background()

	created, err := repo.Upsert(ctx, suppliers.MaterialSupplierPreference{
		CompanyID: "company_a", MaterialID: "material_1", SupplierID: "supplier_1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}, 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, "company_a", "material_1", created.Revision); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := repo.FindByMaterial(ctx, "company_a", "material_1"); !errors.Is(err,
		suppliers.ErrPreferenceNotFound) {
		t.Fatalf("error = %v, want ErrPreferenceNotFound after deletion", err)
	}
}

func TestMongoPreferenceIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newPreferenceRepo(t, db)
	ctx := context.Background()

	if _, err := repo.Upsert(ctx, suppliers.MaterialSupplierPreference{
		CompanyID: "company_a", MaterialID: "material_1", SupplierID: "supplier_1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}, 0); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.FindByMaterial(ctx, "company_b", "material_1"); !errors.Is(err,
		suppliers.ErrPreferenceNotFound) {
		t.Fatalf("error = %v, want ErrPreferenceNotFound for a foreign company", err)
	}
}

// --- Indexes (design spec §12.3) ---

func TestMongoEnsureIndexesCreatesTheNamedIndexes(t *testing.T) {
	db := setupDB(t)
	newSupplierRepo(t, db)
	newOfferingRepo(t, db)
	newPreferenceRepo(t, db)
	ctx := context.Background()

	for _, spec := range []struct {
		collection string
		want       []string
	}{
		{"suppliers", []string{
			"idx_suppliers_company",
			"uq_suppliers_company_name",
			"idx_suppliers_company_active",
			"idx_suppliers_company_categories",
		}},
		{"supplier_offerings", []string{
			"idx_supplier_offerings_company",
			"idx_supplier_offerings_company_supplier",
			"idx_supplier_offerings_company_material",
			"idx_supplier_offerings_company_active",
		}},
		{"material_supplier_preferences", []string{
			"uq_material_supplier_preferences_material",
			"idx_material_supplier_preferences_supplier",
		}},
	} {
		cursor, err := db.Collection(spec.collection).Indexes().List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var idx []struct {
			Name string `bson:"name"`
		}
		if err := cursor.All(ctx, &idx); err != nil {
			t.Fatal(err)
		}
		present := map[string]bool{}
		for _, i := range idx {
			present[i.Name] = true
		}
		for _, want := range spec.want {
			if !present[want] {
				t.Errorf("%s is missing index %q", spec.collection, want)
			}
		}
	}
}

// The name index must NOT be partial: a partial index on Active would let a
// retired name be reused, which §4.1 forbids.
func TestMongoSupplierNameIndexIsNotPartial(t *testing.T) {
	db := setupDB(t)
	newSupplierRepo(t, db)
	ctx := context.Background()

	cursor, err := db.Collection("suppliers").Indexes().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var idx []struct {
		Name                    string `bson:"name"`
		PartialFilterExpression bson.M `bson:"partialFilterExpression,omitempty"`
	}
	if err := cursor.All(ctx, &idx); err != nil {
		t.Fatal(err)
	}
	for _, i := range idx {
		if i.Name == "uq_suppliers_company_name" && len(i.PartialFilterExpression) != 0 {
			t.Errorf("uq_suppliers_company_name is partial (%v); it must cover INACTIVE "+
				"records too, or retirement enables duplication", i.PartialFilterExpression)
		}
	}
}

func TestMongoEnsureIndexesIsIdempotent(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := suppliers.NewMongoSupplierRepository(db).EnsureIndexes(ctx); err != nil {
			t.Fatalf("supplier EnsureIndexes run %d: %v", i+1, err)
		}
		if err := suppliers.NewMongoSupplierOfferingRepository(db).EnsureIndexes(ctx); err != nil {
			t.Fatalf("offering EnsureIndexes run %d: %v", i+1, err)
		}
		if err := suppliers.NewMongoPreferenceRepository(db).EnsureIndexes(ctx); err != nil {
			t.Fatalf("preference EnsureIndexes run %d: %v", i+1, err)
		}
	}
}

// TestService_DeleteAllForCompany proves Task 1a's multi-collection
// guidance (spec §6.6): ONE Service.DeleteAllForCompany call removes
// Supplier, SupplierOffering, AND MaterialSupplierPreference records for
// companyID. Seeds via all three repositories directly (matching every
// sibling test in this file).
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	supplierRepo := newSupplierRepo(t, db)
	offeringRepo := newOfferingRepo(t, db)
	preferenceRepo := newPreferenceRepo(t, db)
	svc := suppliers.NewService(supplierRepo, offeringRepo, preferenceRepo, nil, nil)
	ctx := context.Background()

	supplierA, err := supplierRepo.Create(ctx, newSupplier(t, "company_a", "Supplier A"))
	if err != nil {
		t.Fatalf("create company_a supplier: %v", err)
	}
	supplierB, err := supplierRepo.Create(ctx, newSupplier(t, "company_b", "Supplier B"))
	if err != nil {
		t.Fatalf("create company_b supplier: %v", err)
	}
	if _, err := offeringRepo.Create(ctx, newOffering(t, "company_a", supplierA.ID)); err != nil {
		t.Fatalf("create company_a offering: %v", err)
	}
	if _, err := offeringRepo.Create(ctx, newOffering(t, "company_b", supplierB.ID)); err != nil {
		t.Fatalf("create company_b offering: %v", err)
	}
	if _, err := preferenceRepo.Upsert(ctx, suppliers.MaterialSupplierPreference{
		CompanyID: "company_a", MaterialID: "material_1", SupplierID: supplierA.ID,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}, 0); err != nil {
		t.Fatalf("create company_a preference: %v", err)
	}
	if _, err := preferenceRepo.Upsert(ctx, suppliers.MaterialSupplierPreference{
		CompanyID: "company_b", MaterialID: "material_2", SupplierID: supplierB.ID,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	}, 0); err != nil {
		t.Fatalf("create company_b preference: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	suppliersA, err := supplierRepo.List(ctx, "company_a", suppliers.SupplierFilter{})
	if err != nil {
		t.Fatalf("List suppliers company_a: %v", err)
	}
	if len(suppliersA) != 0 {
		t.Fatalf("expected 0 remaining company_a suppliers, got %d", len(suppliersA))
	}
	offeringsA, err := offeringRepo.ListBySupplier(ctx, "company_a", supplierA.ID)
	if err != nil {
		t.Fatalf("ListBySupplier offerings company_a: %v", err)
	}
	if len(offeringsA) != 0 {
		t.Fatalf("expected 0 remaining company_a offerings, got %d", len(offeringsA))
	}
	preferencesA, err := preferenceRepo.ListBySupplier(ctx, "company_a", supplierA.ID)
	if err != nil {
		t.Fatalf("ListBySupplier preferences company_a: %v", err)
	}
	if len(preferencesA) != 0 {
		t.Fatalf("expected 0 remaining company_a preferences, got %d", len(preferencesA))
	}

	suppliersB, err := supplierRepo.List(ctx, "company_b", suppliers.SupplierFilter{})
	if err != nil {
		t.Fatalf("List suppliers company_b: %v", err)
	}
	if len(suppliersB) != 1 {
		t.Fatalf("expected company_b's supplier to be untouched, got %d", len(suppliersB))
	}
	offeringsB, err := offeringRepo.ListBySupplier(ctx, "company_b", supplierB.ID)
	if err != nil {
		t.Fatalf("ListBySupplier offerings company_b: %v", err)
	}
	if len(offeringsB) != 1 {
		t.Fatalf("expected company_b's offering to be untouched, got %d", len(offeringsB))
	}
	preferencesB, err := preferenceRepo.ListBySupplier(ctx, "company_b", supplierB.ID)
	if err != nil {
		t.Fatalf("ListBySupplier preferences company_b: %v", err)
	}
	if len(preferencesB) != 1 {
		t.Fatalf("expected company_b's preference to be untouched, got %d", len(preferencesB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupDB(t)
	supplierRepo := newSupplierRepo(t, db)
	offeringRepo := newOfferingRepo(t, db)
	preferenceRepo := newPreferenceRepo(t, db)
	svc := suppliers.NewService(supplierRepo, offeringRepo, preferenceRepo, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
