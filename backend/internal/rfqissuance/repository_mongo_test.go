package rfqissuance_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Phase A's Docker-backed repository coverage: the issued-version uniqueness
// invariant and the tenant-scoped issuance-status lookup (design spec §11.2).
//
// A real MongoDB is mandatory here because the invariant IS the unique index —
// an in-memory fake would assert the test's own logic rather than the
// constraint the database enforces.

// setupDB follows the established convention: raw mongo.Connect, a uniquely
// named database per test, TerminateContainer in cleanup. A unique database
// matters because these tests exercise unique indexes.
func setupDB(t *testing.T) *mongo.Database {
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

	return client.Database(fmt.Sprintf("rfqissuance_test_%d", time.Now().UnixNano()))
}

func newIssuedVersionRepo(t *testing.T, db *mongo.Database) *rfqissuance.MongoIssuedRFQVersionRepository {
	t.Helper()
	repo := rfqissuance.NewMongoIssuedRFQVersionRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure indexes: %v", err)
	}
	return repo
}

// insertVersion writes a minimal issued-version document directly. Phase A has
// no issuance workflow yet, so the lookup is exercised against the shape the
// repository reads rather than through a service that does not exist.
func insertVersion(t *testing.T, db *mongo.Database, companyID, rfqChainID string, version int) error {
	t.Helper()
	_, err := db.Collection("issued_rfq_versions").InsertOne(context.Background(), bson.M{
		"companyId":     companyID,
		"rfqChainId":    rfqChainID,
		"versionNumber": version,
		"issuedAt":      time.Now(),
		"schemaVersion": 1,
	})
	return err
}

func TestHasIssuedVersionReportsFalseBeforeAnyIssuance(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)

	issued, err := repo.HasIssuedVersion(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued {
		t.Error("expected issued = false with no issued versions present")
	}
}

func TestHasIssuedVersionReportsTrueOnceAVersionExists(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)

	if err := insertVersion(t, db, "company-1", "chain-1", 1); err != nil {
		t.Fatalf("failed to insert version: %v", err)
	}

	issued, err := repo.HasIssuedVersion(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !issued {
		t.Error("expected issued = true once an issued version exists")
	}
}

// Tenant isolation at the query level: another company's issued version must
// not make this company's chain look issued.
func TestHasIssuedVersionIsScopedToTheCompany(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)

	if err := insertVersion(t, db, "company-2", "chain-1", 1); err != nil {
		t.Fatalf("failed to insert version: %v", err)
	}

	issued, err := repo.HasIssuedVersion(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued {
		t.Error("another company's issued version must not answer this company's query")
	}
}

func TestHasIssuedVersionIsScopedToTheChain(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)

	if err := insertVersion(t, db, "company-1", "chain-2", 1); err != nil {
		t.Fatalf("failed to insert version: %v", err)
	}

	issued, err := repo.HasIssuedVersion(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued {
		t.Error("another chain's issued version must not answer this chain's query")
	}
}

// The immutability invariant of design spec §11.2: one immutable version per
// companyId + rfqChainId + versionNumber. The unique index is what makes a
// concurrent double-issue impossible rather than merely unlikely.
func TestDuplicateVersionNumberIsRejectedByTheUniqueIndex(t *testing.T) {
	db := setupDB(t)
	_ = newIssuedVersionRepo(t, db)

	if err := insertVersion(t, db, "company-1", "chain-1", 1); err != nil {
		t.Fatalf("failed to insert the first version: %v", err)
	}

	err := insertVersion(t, db, "company-1", "chain-1", 1)

	if err == nil {
		t.Fatal("expected a duplicate version number to be rejected by the unique index")
	}
	if !mongo.IsDuplicateKeyError(err) {
		t.Errorf("expected a duplicate-key error, got %v", err)
	}
}

// The same version number under a different chain or company is legitimate:
// every chain numbers its own versions from 1.
func TestTheSameVersionNumberIsPermittedOnAnotherChainOrCompany(t *testing.T) {
	db := setupDB(t)
	_ = newIssuedVersionRepo(t, db)

	if err := insertVersion(t, db, "company-1", "chain-1", 1); err != nil {
		t.Fatalf("failed to insert version: %v", err)
	}

	if err := insertVersion(t, db, "company-1", "chain-2", 1); err != nil {
		t.Errorf("version 1 of a different chain must be permitted, got %v", err)
	}
	if err := insertVersion(t, db, "company-2", "chain-1", 1); err != nil {
		t.Errorf("version 1 of a different company must be permitted, got %v", err)
	}
}
