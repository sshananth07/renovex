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

// The one-draft-per-chain invariant IS a unique index, so it is proven against
// real MongoDB (design spec §11.2, §16.4).

func newDraftRepo(t *testing.T, db *mongo.Database) *rfqissuance.MongoAmendmentDraftRepository {
	t.Helper()
	repo := rfqissuance.NewMongoAmendmentDraftRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure indexes: %v", err)
	}
	return repo
}

func draftFixture(t *testing.T, companyID, rfqChainID string) rfqissuance.RFQAmendmentDraft {
	t.Helper()
	deadline := time.Now().Add(21 * 24 * time.Hour)

	line, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("building line: %v", err)
	}

	return rfqissuance.RFQAmendmentDraft{
		CompanyID: companyID, RFQChainID: rfqChainID,
		BaseIssuedVersionID: "version-1", BaseVersionNumber: 1,
		Currency: "MYR", Title: "Cement and aggregate",
		DeliveryAddress: "12 Site Road", ResponseDeadline: &deadline,
		Lines:           []rfqissuance.IssuedRFQLine{line},
		CreatedByUserID: "user-1",
		SchemaVersion:   rfqissuance.RFQAmendmentDraftSchemaVersion,
	}
}

func TestCreateDraftRoundTripsEveryField(t *testing.T) {
	db := setupDB(t)
	repo := newDraftRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("the created draft must carry its persisted ID")
	}

	loaded, err := repo.FindDraft(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}

	if loaded.BaseIssuedVersionID != "version-1" || loaded.BaseVersionNumber != 1 {
		t.Errorf("base round-trip = %q/%d", loaded.BaseIssuedVersionID,
			loaded.BaseVersionNumber)
	}
	if loaded.Currency != "MYR" || loaded.Title != "Cement and aggregate" {
		t.Errorf("header round-trip = %q/%q", loaded.Currency, loaded.Title)
	}
	if loaded.ResponseDeadline == nil {
		t.Error("ResponseDeadline must round-trip")
	}
	if len(loaded.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(loaded.Lines))
	}
	if loaded.Lines[0].SourceMaterialRequirementID == nil ||
		*loaded.Lines[0].SourceMaterialRequirementID != "mr-1" {
		t.Error("line provenance must round-trip so copy-forward still matches")
	}
	if got := loaded.Lines[0].Quantity.Value.String(); got != "100" {
		t.Errorf("Quantity = %q, want the exact decimal 100", got)
	}
}

// One draft per chain (§3.3): two would let two contractors prepare divergent
// next versions.
func TestCreateDraftRejectsASecondDraftOnTheSameChain(t *testing.T) {
	db := setupDB(t)
	repo := newDraftRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1"))

	if !errors.Is(err, rfqissuance.ErrAmendmentDraftAlreadyExists) {
		t.Errorf("error = %v, want ErrAmendmentDraftAlreadyExists", err)
	}
}

func TestCreateDraftPermitsOneDraftPerChainAndCompany(t *testing.T) {
	db := setupDB(t)
	repo := newDraftRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-2")); err != nil {
		t.Errorf("another chain's draft must be permitted, got %v", err)
	}
	if _, err := repo.CreateDraft(ctx, draftFixture(t, "company-2", "chain-1")); err != nil {
		t.Errorf("another company's draft must be permitted, got %v", err)
	}
}

func TestFindDraftIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newDraftRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.FindDraft(ctx, "company-2", "chain-1")

	if !errors.Is(err, rfqissuance.ErrAmendmentDraftNotFound) {
		t.Errorf("error = %v, want ErrAmendmentDraftNotFound for a foreign company", err)
	}
}

func TestUpdateDraftRequiresTheExpectedRevisionAndIncrementsIt(t *testing.T) {
	db := setupDB(t)
	repo := newDraftRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edited := created
	edited.Title = "Revised scope"

	updated, err := repo.UpdateDraft(ctx, "company-1", "chain-1", created.Revision, edited)
	if err != nil {
		t.Fatalf("updating under the current revision failed: %v", err)
	}
	if updated.Title != "Revised scope" {
		t.Errorf("Title = %q, want the edit applied", updated.Title)
	}
	if updated.Revision == created.Revision {
		t.Error("Revision must increment so a stale editor cannot match again")
	}

	if _, err := repo.UpdateDraft(ctx, "company-1", "chain-1", created.Revision,
		edited); !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch for a stale revision", err)
	}
}

func TestDeleteDraftRequiresTheExpectedRevision(t *testing.T) {
	db := setupDB(t)
	repo := newDraftRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := repo.DeleteDraft(ctx, "company-1", "chain-1",
		created.Revision+99); !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch", err)
	}

	if err := repo.DeleteDraft(ctx, "company-1", "chain-1", created.Revision); err != nil {
		t.Fatalf("deleting under the current revision failed: %v", err)
	}
	if _, err := repo.FindDraft(ctx, "company-1",
		"chain-1"); !errors.Is(err, rfqissuance.ErrAmendmentDraftNotFound) {
		t.Errorf("error = %v, want the draft to be gone", err)
	}
}

// Concurrent draft creation must produce exactly one draft.
func TestConcurrentCreateDraftHasExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := newDraftRepo(t, db)
	ctx := context.Background()

	const concurrency = 12
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.CreateDraft(ctx, draftFixture(t, "company-1", "chain-1"))
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, rfqissuance.ErrAmendmentDraftAlreadyExists):
			// The expected loss.
		default:
			t.Fatalf("goroutine %d failed unexpectedly: %v", i, err)
		}
	}

	if winners != 1 {
		t.Errorf("got %d winners, want exactly 1", winners)
	}
}
