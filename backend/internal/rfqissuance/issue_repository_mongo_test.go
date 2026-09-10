package rfqissuance_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Immutable version creation and operation-ID idempotency (design spec §10.1,
// §11.2). Real MongoDB: both invariants ARE unique indexes.

func issuableVersion(t *testing.T, versionNumber int, operationID string) rfqissuance.IssuedRFQVersion {
	t.Helper()
	deadline := time.Now().Add(14 * 24 * time.Hour)

	line, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("building line: %v", err)
	}

	version, err := rfqissuance.NewIssuedVersion(rfqissuance.NewIssuedVersionInput{
		CompanyID: "company-1", ProjectID: "project-1", RFQChainID: "chain-1",
		RFQNumber: "RFQ-000001", VersionNumber: versionNumber, Currency: "MYR",
		Title: "Cement and aggregate", DeliveryAddress: "12 Site Road",
		ResponseDeadline:    &deadline,
		Lines:               []rfqissuance.IssuedRFQLine{line},
		IssuedByUserID:      "user-1",
		IssuanceOperationID: operationID,
	})
	if err != nil {
		t.Fatalf("building version: %v", err)
	}
	return version
}

func TestCreateVersionPersistsEveryFieldIncludingLineProvenance(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateVersion(ctx, issuableVersion(t, 1, "op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("the created version must carry its persisted ID")
	}

	loaded, err := repo.FindVersion(ctx, "company-1", created.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	if loaded.VersionNumber != 1 || loaded.RFQNumber != "RFQ-000001" {
		t.Errorf("header round-trip = %d/%q", loaded.VersionNumber, loaded.RFQNumber)
	}
	if loaded.Currency != "MYR" || loaded.Title != "Cement and aggregate" {
		t.Errorf("currency/title round-trip = %q/%q", loaded.Currency, loaded.Title)
	}
	if loaded.ResponseDeadline.IsZero() {
		t.Error("ResponseDeadline must round-trip; it is the commercial response window")
	}

	if len(loaded.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(loaded.Lines))
	}
	line := loaded.Lines[0]
	if line.ID == "" || line.LineageID == "" {
		t.Error("both the per-version ID and the lineage ID must round-trip")
	}
	if line.SourceMaterialRequirementID == nil || *line.SourceMaterialRequirementID != "mr-1" {
		t.Errorf("SourceMaterialRequirementID = %v, want mr-1; copy-forward matches on it",
			line.SourceMaterialRequirementID)
	}
	if line.SourceM7RFQLineID == nil || *line.SourceM7RFQLineID != "m7-line-1" {
		t.Errorf("SourceM7RFQLineID = %v, want m7-line-1", line.SourceM7RFQLineID)
	}
	// Quantities are stored as canonical decimal STRINGS, never floats (ADR 0001).
	if got := line.Quantity.Value.String(); got != "100" {
		t.Errorf("Quantity = %q, want the exact decimal 100", got)
	}
}

// An M8-native line's absent provenance must round-trip as nil, not as "".
// Persisting "" would turn "no origin" into "an origin whose ID is empty".
func TestCreateVersionRoundTripsAbsentProvenanceAsNil(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	native, err := rfqissuance.NewM8NativeLine(rfqissuance.M8NativeLineInput{
		MaterialID: "material-9", MaterialName: "Rebar",
		QuantityValue: "40", QuantityUnit: "length",
	})
	if err != nil {
		t.Fatalf("building native line: %v", err)
	}
	version := issuableVersion(t, 1, "op-1")
	version.Lines = []rfqissuance.IssuedRFQLine{native}

	created, err := repo.CreateVersion(ctx, version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	loaded, err := repo.FindVersion(ctx, "company-1", created.ID)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	line := loaded.Lines[0]
	if line.SourceM7RFQLineID != nil {
		t.Errorf("SourceM7RFQLineID = %v, want nil for an M8-native line",
			line.SourceM7RFQLineID)
	}
	if line.SourceMaterialRequirementID != nil {
		t.Errorf("SourceMaterialRequirementID = %v, want nil for an M8-native line",
			line.SourceMaterialRequirementID)
	}
}

// The immutability invariant: one version per chain per number.
func TestCreateVersionRejectsADuplicateVersionNumber(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateVersion(ctx, issuableVersion(t, 1, "op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.CreateVersion(ctx, issuableVersion(t, 1, "op-2"))

	if !errors.Is(err, rfqissuance.ErrVersionAlreadyExists) {
		t.Errorf("error = %v, want ErrVersionAlreadyExists. The unique index is what "+
			"makes a concurrent double-issue impossible rather than unlikely", err)
	}
}

// Operation-ID idempotency (design spec §10.1): retrying the SAME logical
// issuance must not create a second version.
func TestCreateVersionRejectsAReusedOperationID(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateVersion(ctx, issuableVersion(t, 1, "op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A different version number under the SAME operation ID: the caller
	// retried, so this must be refused rather than creating version 2.
	_, err := repo.CreateVersion(ctx, issuableVersion(t, 2, "op-1"))

	if !errors.Is(err, rfqissuance.ErrOperationAlreadyUsed) {
		t.Errorf("error = %v, want ErrOperationAlreadyUsed", err)
	}
}

// The idempotency index must be tenant-scoped: two companies may legitimately
// generate the same operation ID.
func TestOperationIDUniquenessIsScopedToTheCompany(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateVersion(ctx, issuableVersion(t, 1, "op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	other := issuableVersion(t, 1, "op-1")
	other.CompanyID = "company-2"

	if _, err := repo.CreateVersion(ctx, other); err != nil {
		t.Errorf("another company's identical operation ID must be permitted, got %v", err)
	}
}

// FindByOperationID is what makes a retry RESOLVE rather than merely fail: an
// interrupted caller re-presents its operation ID and recovers the version its
// first attempt created (design spec §10.1).
func TestFindByOperationIDRecoversAnInterruptedIssuance(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateVersion(ctx, issuableVersion(t, 1, "op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, ok, err := repo.FindByOperationID(ctx, "company-1", "op-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("an issuance created under this operation ID must be recoverable")
	}
	if found.ID != created.ID {
		t.Errorf("recovered version %s, want %s", found.ID, created.ID)
	}

	if _, ok, err := repo.FindByOperationID(ctx, "company-1", "op-unknown"); err != nil || ok {
		t.Errorf("an unused operation ID must report ok=false: ok=%v err=%v", ok, err)
	}
	// Tenant scoping.
	if _, ok, err := repo.FindByOperationID(ctx, "company-2", "op-1"); err != nil || ok {
		t.Errorf("a foreign company must not resolve this operation ID: ok=%v err=%v", ok, err)
	}
}

// Listing is the read side for history and bounded reconciliation. The query
// must scope company and chain in Mongo itself, and the order must reflect the
// immutable business version sequence rather than insertion timing.
func TestListVersionsIsTenantScopedAndOrderedByVersionNumber(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	for _, versionNumber := range []int{2, 1} {
		if _, err := repo.CreateVersion(ctx,
			issuableVersion(t, versionNumber, "op-"+itoa(versionNumber))); err != nil {
			t.Fatalf("create chain-1 Version %d: %v", versionNumber, err)
		}
	}
	otherChain := issuableVersion(t, 1, "op-other-chain")
	otherChain.RFQChainID = "chain-2"
	if _, err := repo.CreateVersion(ctx, otherChain); err != nil {
		t.Fatalf("create other chain: %v", err)
	}
	otherCompany := issuableVersion(t, 1, "op-other-company")
	otherCompany.CompanyID = "company-2"
	if _, err := repo.CreateVersion(ctx, otherCompany); err != nil {
		t.Fatalf("create other company: %v", err)
	}

	versions, err := repo.ListVersions(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want only company-1/chain-1's 2", len(versions))
	}
	if versions[0].VersionNumber != 1 || versions[1].VersionNumber != 2 {
		t.Errorf("version order = %d,%d, want 1,2",
			versions[0].VersionNumber, versions[1].VersionNumber)
	}
}

// The one-winner guarantee at version creation: N concurrent attempts to create
// the SAME version number must produce exactly one persisted version.
func TestConcurrentCreateVersionHasExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := newIssuedVersionRepo(t, db)
	ctx := context.Background()

	const concurrency = 12
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			// Distinct operation IDs, so the ONLY thing stopping a duplicate is
			// the version-number index.
			_, errs[i] = repo.CreateVersion(ctx, issuableVersion(t, 1, "op-"+itoa(i)))
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, rfqissuance.ErrVersionAlreadyExists):
			// The expected loss.
		default:
			t.Fatalf("goroutine %d failed unexpectedly: %v", i, err)
		}
	}

	if winners != 1 {
		t.Errorf("got %d winners, want exactly 1", winners)
	}

	count, err := db.Collection("issued_rfq_versions").CountDocuments(ctx,
		countFilter("company-1", "chain-1", 1))
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != 1 {
		t.Errorf("persisted %d copies of version 1, want exactly 1", count)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func countFilter(companyID, rfqChainID string, version int) any {
	return map[string]any{
		"companyId": companyID, "rfqChainId": rfqChainID, "versionNumber": version,
	}
}

var _ = mongo.ErrNoDocuments
