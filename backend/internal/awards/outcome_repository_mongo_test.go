package awards_test

import (
	"context"
	"sync"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// F7 persistence (§8H). One outcome per Supplier + Award Revision, enforced by
// a named unique index so concurrent generation cannot tell one Supplier twice.

func outcomeRepository(t *testing.T) *awards.MongoAwardOutcomeRepository {
	t.Helper()
	repository := awards.NewMongoAwardOutcomeRepository(setupDB(t))
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repository
}

func candidateOutcome(supplierID string) awards.AwardOutcome {
	return awards.AwardOutcome{
		CompanyID: "company-1", AwardChainID: "chain-1",
		AwardRevisionID: "revision-1", RFQChainID: "rfqchain-1",
		IssuedRFQVersionID: "issued-1",
		SupplierID:         supplierID, InvitationID: "invitation-" + supplierID,
		Result: awards.OutcomeSelected,
		Projection: awards.OutcomeProjection{
			Result:     awards.OutcomeSelected,
			AwardTotal: money.New(10_000, "MYR"),
		},
	}
}

// Generation is recoverable post-publication work, so it must be safe to run
// twice: the second call adopts rather than duplicating.
func TestEnsureOutcomeIsIdempotentPerSupplierAndRevision(t *testing.T) {
	repository := outcomeRepository(t)
	ctx := context.Background()

	first, created, err := repository.EnsureOutcome(ctx,
		candidateOutcome("supplier-a"))
	if err != nil {
		t.Fatalf("EnsureOutcome: %v", err)
	}
	if !created {
		t.Fatal("the first EnsureOutcome must report creation")
	}

	second, created, err := repository.EnsureOutcome(ctx,
		candidateOutcome("supplier-a"))
	if err != nil {
		t.Fatalf("EnsureOutcome (repeat): %v", err)
	}
	if created {
		t.Error("a repeat EnsureOutcome must adopt, not create")
	}
	if first.ID != second.ID {
		t.Fatalf("two outcomes exist for one Supplier: %s and %s",
			first.ID, second.ID)
	}
}

// Concurrent generation converges: exactly one outcome per Supplier + revision.
func TestConcurrentOutcomeGenerationProducesOneOutcomePerSupplier(t *testing.T) {
	repository := outcomeRepository(t)
	ctx := context.Background()

	const attempts = 8
	ids := make([]string, attempts)
	errs := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			outcome, _, err := repository.EnsureOutcome(ctx,
				candidateOutcome("supplier-a"))
			ids[index], errs[index] = outcome.ID, err
		}(index)
	}
	wait.Wait()

	unique := map[string]bool{}
	for index, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: %v", index, err)
		}
		unique[ids[index]] = true
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent generation produced %d outcomes, want 1",
			len(unique))
	}
}

// A correction generates NEW outcomes; it never edits one already sent. Two
// revisions therefore legitimately hold an outcome for the same Supplier.
func TestEachRevisionOwnsItsOwnOutcomes(t *testing.T) {
	repository := outcomeRepository(t)
	ctx := context.Background()

	first, _, err := repository.EnsureOutcome(ctx, candidateOutcome("supplier-a"))
	if err != nil {
		t.Fatalf("EnsureOutcome: %v", err)
	}

	corrected := candidateOutcome("supplier-a")
	corrected.AwardRevisionID = "revision-2"
	second, created, err := repository.EnsureOutcome(ctx, corrected)
	if err != nil {
		t.Fatalf("EnsureOutcome for the correction: %v", err)
	}
	if !created || first.ID == second.ID {
		t.Fatal("a correction must generate a NEW outcome, not reuse the old one")
	}
}

// Listing is by revision, which is what the contractor outcome route reads.
func TestListOutcomesReturnsEveryOutcomeForARevision(t *testing.T) {
	repository := outcomeRepository(t)
	ctx := context.Background()

	for _, supplier := range []string{"supplier-a", "supplier-b"} {
		if _, _, err := repository.EnsureOutcome(
			ctx, candidateOutcome(supplier)); err != nil {
			t.Fatalf("EnsureOutcome(%s): %v", supplier, err)
		}
	}

	outcomes, err := repository.ListOutcomes(ctx, "company-1", "revision-1")
	if err != nil {
		t.Fatalf("ListOutcomes: %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2", len(outcomes))
	}
}

// A foreign tenant reads nothing, even holding the exact outcome ID.
func TestOutcomesAreTenantScoped(t *testing.T) {
	repository := outcomeRepository(t)
	ctx := context.Background()

	outcome, _, err := repository.EnsureOutcome(ctx, candidateOutcome("supplier-a"))
	if err != nil {
		t.Fatalf("EnsureOutcome: %v", err)
	}

	if _, found, err := repository.FindOutcome(
		ctx, "company-2", outcome.ID); err != nil {
		t.Fatalf("FindOutcome: %v", err)
	} else if found {
		t.Fatal("a foreign company must not read another tenant's outcome")
	}
	if outcomes, err := repository.ListOutcomes(
		ctx, "company-2", "revision-1"); err != nil {
		t.Fatalf("ListOutcomes: %v", err)
	} else if len(outcomes) != 0 {
		t.Fatal("a foreign company must not list another tenant's outcomes")
	}
}

// The Supplier read is scoped to Supplier + Invitation (D3): another Supplier's
// outcome is simply absent, the same non-disclosing not-found Phase D uses.
func TestSupplierOutcomeReadIsScopedToSupplierAndInvitation(t *testing.T) {
	repository := outcomeRepository(t)
	ctx := context.Background()

	outcome, _, err := repository.EnsureOutcome(ctx, candidateOutcome("supplier-a"))
	if err != nil {
		t.Fatalf("EnsureOutcome: %v", err)
	}

	// The rightful Supplier resolves it.
	if _, found, err := repository.FindOutcomeForSupplier(ctx,
		"company-1", outcome.ID, "supplier-a", "invitation-supplier-a"); err != nil {
		t.Fatalf("FindOutcomeForSupplier: %v", err)
	} else if !found {
		t.Fatal("the owning Supplier must resolve its own outcome")
	}

	// Another Supplier does not, even with the exact outcome ID.
	if _, found, err := repository.FindOutcomeForSupplier(ctx,
		"company-1", outcome.ID, "supplier-b", "invitation-supplier-b"); err != nil {
		t.Fatalf("FindOutcomeForSupplier: %v", err)
	} else if found {
		t.Fatal("another Supplier must never resolve this outcome (D3)")
	}

	// The right Supplier under the WRONG invitation also does not: the scope is
	// Supplier AND Invitation, not either alone.
	if _, found, err := repository.FindOutcomeForSupplier(ctx,
		"company-1", outcome.ID, "supplier-a", "invitation-other"); err != nil {
		t.Fatalf("FindOutcomeForSupplier: %v", err)
	} else if found {
		t.Fatal("a mismatched invitation must not resolve the outcome (D3)")
	}
}

// The frozen projection survives the round trip: what a Supplier was told must
// not change because it was written to and read from MongoDB.
func TestOutcomeProjectionRoundTripsUnchanged(t *testing.T) {
	repository := outcomeRepository(t)
	ctx := context.Background()

	candidate := candidateOutcome("supplier-a")
	candidate.Projection.AwardedLines = []awards.OutcomeAwardedLine{{
		IssuedRFQLineID: "line-1", MaterialName: "Tile",
		UnitPrice:     money.New(1_000, "MYR"),
		LineSubtotal:  money.New(10_000, "MYR"),
		LineTaxAmount: money.New(600, "MYR"),
		Brand:         "Acme", SKU: "TIL-1", LeadTime: "2 weeks",
	}}
	candidate.Projection.LineSubtotal = money.New(10_000, "MYR")
	candidate.Projection.TaxTotal = money.New(600, "MYR")
	candidate.Projection.DeliveryCharge = money.New(3_000, "MYR")
	candidate.Projection.AwardTotal = money.New(13_600, "MYR")
	candidate.Projection.ContractorMessage = "Please confirm receipt."

	stored, _, err := repository.EnsureOutcome(ctx, candidate)
	if err != nil {
		t.Fatalf("EnsureOutcome: %v", err)
	}
	reloaded, found, err := repository.FindOutcome(ctx, "company-1", stored.ID)
	if err != nil || !found {
		t.Fatalf("FindOutcome: found=%v err=%v", found, err)
	}

	if reloaded.Projection.AwardTotal != money.New(13_600, "MYR") {
		t.Errorf("award total = %+v, want 13600 MYR",
			reloaded.Projection.AwardTotal)
	}
	if len(reloaded.Projection.AwardedLines) != 1 {
		t.Fatalf("awarded lines = %d, want 1",
			len(reloaded.Projection.AwardedLines))
	}
	line := reloaded.Projection.AwardedLines[0]
	if line.Brand != "Acme" || line.SKU != "TIL-1" ||
		line.LineTaxAmount != money.New(600, "MYR") {
		t.Errorf("line round-tripped as %+v", line)
	}
	if reloaded.Projection.ContractorMessage != "Please confirm receipt." {
		t.Error("the contractor message did not survive the round trip")
	}
}
