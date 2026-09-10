// Command dbbootstrap provisions a clean Atlas/Mongo database for the
// tester deployment (M8.5C plan): creates the exact checked-in collection
// manifest, runs every repository's EnsureIndexes via the same
// composition.BuildServices call cmd/api/main.go uses (never a duplicated
// index definition), verifies multi-document transaction support, and
// records one migration ledger entry. It inserts no business or demo data
// — internal/demoseed remains the ONLY source of seed data, and stays
// explicitly guarded against running outside development.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	"github.com/shananth/renovation-platform/backend/internal/platform/logging"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// migrationID/applicationVersion identify this bootstrap's ledger entry —
// bumping migrationID is only ever done deliberately, when the manifest or
// index set genuinely changes in a way worth recording as a new applied
// migration.
const migrationID = "20260909_001_m85c_tester_bootstrap"
const applicationVersion = "m85c-rp4e2-rp4e3"

// migrationsCollection is schema_migrations — part of CollectionManifest,
// but dbbootstrap is the only writer (no domain repository owns it).
const migrationsCollection = "schema_migrations"

// forbiddenDatabaseNames are MongoDB/Atlas-reserved database names — the
// plan's explicit "refuse admin, local, or config" requirement, since a
// misconfigured MONGO_DATABASE pointed at one of these would corrupt the
// deployment's actual administrative state.
var forbiddenDatabaseNames = map[string]bool{"admin": true, "local": true, "config": true}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dbbootstrap:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 || (os.Args[1] != "apply" && os.Args[1] != "verify") {
		return fmt.Errorf("usage: dbbootstrap <apply|verify> [--allow-unexpected-collections]")
	}
	subcommand := os.Args[1]
	allowUnexpected := false
	for _, arg := range os.Args[2:] {
		if arg == "--allow-unexpected-collections" {
			allowUnexpected = true
		}
	}

	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("../.env")
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if forbiddenDatabaseNames[cfg.MongoDatabase] {
		return fmt.Errorf("refusing to bootstrap reserved database name %q", cfg.MongoDatabase)
	}

	mongoClient, err := platformmongo.Connect(cfg.MongoURI)
	if err != nil {
		return fmt.Errorf("connect to mongodb: %w", err)
	}
	defer func() {
		_ = platformmongo.Disconnect(context.Background(), mongoClient)
	}()

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelPing()
	if err := platformmongo.Ping(pingCtx, mongoClient); err != nil {
		return fmt.Errorf("ping mongodb: %w", err)
	}

	db := platformmongo.Database(mongoClient, cfg.MongoDatabase)

	switch subcommand {
	case "apply":
		return apply(context.Background(), cfg, mongoClient, db, allowUnexpected)
	case "verify":
		return verify(context.Background(), db)
	}
	return nil // unreachable — subcommand validated above
}

// migrationLedgerEntry is schema_migrations's one durable document shape —
// deliberately minimal: which migration, what manifest checksum, which
// application version, and when.
type migrationLedgerEntry struct {
	MigrationID        string    `bson:"_id"`
	ManifestChecksum   string    `bson:"manifestChecksum"`
	ApplicationVersion string    `bson:"applicationVersion"`
	AppliedAt          time.Time `bson:"appliedAt"`
}

func apply(ctx context.Context, cfg config.Config, mongoClient *mongo.Client, db *mongo.Database, allowUnexpected bool) error {
	existing, err := platformmongo.ListCollectionNames(ctx, db)
	if err != nil {
		return fmt.Errorf("listing existing collections: %w", err)
	}

	unexpected := platformmongo.UnexpectedCollections(existing)
	if len(unexpected) > 0 && !allowUnexpected {
		return fmt.Errorf("unexpected application collections present: %v (pass --allow-unexpected-collections to proceed anyway)", unexpected)
	}

	// Create every missing manifest collection explicitly — Mongo also
	// creates a collection implicitly on first write, but an explicit
	// CreateCollection makes "the manifest collections exist" a real,
	// independently verifiable fact rather than an accidental side effect
	// of whichever repository happens to write first.
	missing := platformmongo.MissingCollections(existing)
	for _, name := range missing {
		if name == migrationsCollection {
			continue // created implicitly by the ledger insert below
		}
		if err := db.CreateCollection(ctx, name); err != nil {
			return fmt.Errorf("creating collection %q: %w", name, err)
		}
	}

	// composition.BuildServices constructs every repository and calls its
	// EnsureIndexes — the SAME 60+ calls cmd/api/main.go's own startup
	// makes, never a duplicated index definition (plan's explicit
	// requirement: "Call the same repository EnsureIndexes sequence used by
	// BuildServices; do not duplicate index definitions in the command").
	// dbbootstrap inserts no business data itself, so a nil-safe logger
	// (BuildServices only logs informational startup messages) is fine here.
	realLogger := logging.New(os.Stdout, "info")
	if _, err := composition.BuildServices(ctx, cfg, realLogger, db); err != nil {
		return fmt.Errorf("building service graph (EnsureIndexes sequence): %w", err)
	}

	if err := platformmongo.VerifyTransactionSupport(ctx, mongoClient, cfg.MongoDatabase); err != nil {
		return fmt.Errorf("verifying multi-document transaction support: %w", err)
	}

	checksum := manifestChecksum()
	ledgerEntry := migrationLedgerEntry{
		MigrationID: migrationID, ManifestChecksum: checksum,
		ApplicationVersion: applicationVersion, AppliedAt: time.Now(),
	}
	migrations := db.Collection(migrationsCollection)
	_, err = migrations.UpdateOne(ctx,
		bson.M{"_id": migrationID},
		bson.M{"$set": ledgerEntry},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("recording migration ledger entry: %w", err)
	}

	fmt.Printf("dbbootstrap apply: database=%q collections=%d migration=%s checksum=%s\n",
		cfg.MongoDatabase, len(platformmongo.CollectionManifest), migrationID, checksum)
	return nil
}

func verify(ctx context.Context, db *mongo.Database) error {
	existing, err := platformmongo.ListCollectionNames(ctx, db)
	if err != nil {
		return fmt.Errorf("listing existing collections: %w", err)
	}

	missing := platformmongo.MissingCollections(existing)
	if len(missing) > 0 {
		return fmt.Errorf("missing required collections: %v", missing)
	}

	var ledgerEntry migrationLedgerEntry
	migrations := db.Collection(migrationsCollection)
	if err := migrations.FindOne(ctx, bson.M{"_id": migrationID}).Decode(&ledgerEntry); err != nil {
		if err == mongo.ErrNoDocuments {
			return fmt.Errorf("migration %q has not been applied — run `dbbootstrap apply` first", migrationID)
		}
		return fmt.Errorf("reading migration ledger entry: %w", err)
	}

	expectedChecksum := manifestChecksum()
	if ledgerEntry.ManifestChecksum != expectedChecksum {
		return fmt.Errorf("manifest checksum mismatch: ledger has %q, current manifest is %q — the schema drifted since apply",
			ledgerEntry.ManifestChecksum, expectedChecksum)
	}

	fmt.Printf("dbbootstrap verify: OK — %d collections present, migration %s applied at %s\n",
		len(platformmongo.CollectionManifest), migrationID, ledgerEntry.AppliedAt.Format(time.RFC3339))
	return nil
}

// manifestChecksum is a deterministic SHA-256 hex digest of the sorted
// collection manifest — recorded in the migration ledger so `verify` can
// detect drift between what was applied and what the currently checked-out
// code expects, without needing a second migration entry for every
// non-schema-changing deploy.
func manifestChecksum() string {
	sorted := platformmongo.SortedCollectionManifest()
	sum := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return hex.EncodeToString(sum[:])
}
