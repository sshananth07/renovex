package materialrequirements_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// B6 covers the split workflow (design spec §8): conservation, the manifest
// written in step 1, recursion rejection, and idempotent reconciliation.

// splitInputs is the happy-path 100 -> 60 + 40 split.
func splitInputs(t *testing.T, values ...string) []mr.SplitChildInput {
	t.Helper()
	out := make([]mr.SplitChildInput, 0, len(values))
	for _, v := range values {
		out = append(out, mr.SplitChildInput{QuantityValue: v, QuantityUnit: "bag"})
	}
	return out
}

// seedSplittable creates a reviewed manual requirement of 100 bag.
func seedSplittable(t *testing.T) (*mr.Service, *fakeRequirementRepo, mr.MaterialRequirement) {
	t.Helper()
	svc, repo, _ := newService(t)
	ctx := context.Background()
	workItemID := "work_1"

	created, err := svc.CreateManualRequirement(ctx, "company_a", "user_1",
		mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: &workItemID, MaterialID: "material_1",
			QuantityValue: "100", QuantityUnit: "bag",
			Specification: strPtr("OPC 50kg"), ProcurementNotes: "site gate",
		})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	reviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("setup review: %v", err)
	}
	return svc, repo, reviewed
}

// Acceptance tests 45, 52: 100 -> 60 + 40, source terminal, two draft children,
// sum exactly 100, children carry split provenance and NO source claims.
func TestSplitConservesQuantityAndMarksSourceTerminal(t *testing.T) {
	svc, repo, source := seedSplittable(t)
	ctx := context.Background()

	result, err := svc.SplitRequirement(ctx, "company_a", "user_1", source.ID,
		source.Revision, splitInputs(t, "60", "40"))
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	if len(result.Children) != 2 {
		t.Fatalf("got %d children, want 2", len(result.Children))
	}

	// Conservation, asserted with decimal.Equal — no tolerance (§8.1).
	total := decimal.Zero
	for _, c := range result.Children {
		total = total.Add(c.RequiredQuantity.Value)
	}
	if !total.Equal(decimal.RequireFromString("100")) {
		t.Errorf("child quantities sum to %s, want exactly 100", total.String())
	}

	after, err := repo.FindByID(ctx, "company_a", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != mr.RequirementStatusSplit {
		t.Errorf("source Status = %q, want split (terminal)", after.Status)
	}
	if after.SplitState != mr.SplitStateCompleted {
		t.Errorf("source SplitState = %q, want completed", after.SplitState)
	}
	if len(after.SplitChildren) != 2 {
		t.Errorf("source manifest has %d entries, want 2", len(after.SplitChildren))
	}
	if after.SplitAt == nil || after.SplitByUserID == nil || *after.SplitByUserID != "user_1" {
		t.Errorf("source split provenance incomplete: at=%v by=%v", after.SplitAt, after.SplitByUserID)
	}
	// The source keeps its own quantity: a split records intent, it does not
	// zero the original demand.
	if !after.RequiredQuantity.Value.Equal(source.RequiredQuantity.Value) {
		t.Errorf("source RequiredQuantity = %s, want %s unchanged",
			after.RequiredQuantity.Value.String(), source.RequiredQuantity.Value.String())
	}

	var groupID string
	for i, c := range result.Children {
		if c.SourceType != mr.SourceTypeSplit {
			t.Errorf("child %d SourceType = %q, want split", i, c.SourceType)
		}
		if c.Status != mr.RequirementStatusDraft {
			t.Errorf("child %d Status = %q, want draft — the contractor must confirm each batch",
				i, c.Status)
		}
		if c.SplitFromRequirementID == nil || *c.SplitFromRequirementID != source.ID {
			t.Errorf("child %d SplitFromRequirementID = %v, want %q",
				i, c.SplitFromRequirementID, source.ID)
		}
		if c.SplitGroupID == nil {
			t.Fatalf("child %d has no SplitGroupID", i)
		}
		if groupID == "" {
			groupID = *c.SplitGroupID
		} else if *c.SplitGroupID != groupID {
			t.Errorf("child %d SplitGroupID = %q, want the shared %q", i, *c.SplitGroupID, groupID)
		}
		if c.SplitSequence == nil || *c.SplitSequence != i {
			t.Errorf("child %d SplitSequence = %v, want %d", i, c.SplitSequence, i)
		}

		// Children must NOT copy the source's aggregate claims (§8.2).
		if len(c.SourceCostItemIDs) != 0 {
			t.Errorf("child %d carries SourceCostItemIDs %v, want none", i, c.SourceCostItemIDs)
		}
		if c.SourceQuantity != nil {
			t.Errorf("child %d carries SourceQuantity, want none", i)
		}
		if c.SourceFingerprint != "" || c.SourceAggregationKey != "" || c.SourceAggregationUnit != "" {
			t.Errorf("child %d carries source aggregation identity, want none", i)
		}
		if c.SourceSyncState != mr.SourceSyncStateClean {
			t.Errorf("child %d SourceSyncState = %q, want permanently clean", i, c.SourceSyncState)
		}

		// Inheritance (§8.1, §8.2).
		if c.CompanyID != source.CompanyID || c.ProjectID != source.ProjectID ||
			c.MaterialID != source.MaterialID {
			t.Errorf("child %d did not inherit company/project/material", i)
		}
		if !samePtrTest(c.WorkItemID, source.WorkItemID) {
			t.Errorf("child %d WorkItemID = %v, want %v", i, c.WorkItemID, source.WorkItemID)
		}
		if c.RequiredQuantity.Unit != source.RequiredQuantity.Unit {
			t.Errorf("child %d unit = %q, want the source unit %q — M7 never converts",
				i, c.RequiredQuantity.Unit, source.RequiredQuantity.Unit)
		}
		if c.MaterialName != source.MaterialName || c.CatalogUnit != source.CatalogUnit ||
			c.Specification != source.Specification {
			t.Errorf("child %d did not inherit the descriptive snapshot fields", i)
		}
	}
}

// Acceptance test 46: quantities that do not sum exactly are rejected and
// NOTHING is created.
func TestSplitRejectsQuantityMismatchAndCreatesNothing(t *testing.T) {
	svc, repo, source := seedSplittable(t)
	ctx := context.Background()
	createsBefore := repo.createCalls

	_, err := svc.SplitRequirement(ctx, "company_a", "user_1", source.ID,
		source.Revision, splitInputs(t, "60", "41"))
	if !errors.Is(err, mr.ErrSplitQuantityMismatch) {
		t.Fatalf("error = %v, want ErrSplitQuantityMismatch", err)
	}

	if repo.createCalls != createsBefore {
		t.Errorf("Create attempted %d times, want 0 — a rejected split creates nothing",
			repo.createCalls-createsBefore)
	}
	after, err := repo.FindByID(ctx, "company_a", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != mr.RequirementStatusReviewed {
		t.Errorf("source Status = %q, want reviewed — a rejected split must not move it",
			after.Status)
	}
	if after.SplitState != mr.SplitStateNone {
		t.Errorf("source SplitState = %q, want none", after.SplitState)
	}
}

// A tiny decimal excess is still a mismatch: conservation uses decimal.Equal
// with no tolerance (§8.1).
func TestSplitRejectsSubUnitQuantityDrift(t *testing.T) {
	svc, _, source := seedSplittable(t)

	_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
		source.Revision, splitInputs(t, "60", "40.0001"))
	if !errors.Is(err, mr.ErrSplitQuantityMismatch) {
		t.Fatalf("error = %v, want ErrSplitQuantityMismatch — conservation has no tolerance", err)
	}
}

// Acceptance test 47: a single child, or any non-positive child, is rejected.
func TestSplitRejectsTooFewChildrenAndNonPositiveQuantities(t *testing.T) {
	t.Run("single child", func(t *testing.T) {
		svc, _, source := seedSplittable(t)
		_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
			source.Revision, splitInputs(t, "100"))
		if !errors.Is(err, mr.ErrSplitTooFewChildren) {
			t.Fatalf("error = %v, want ErrSplitTooFewChildren", err)
		}
	})

	t.Run("zero quantity child", func(t *testing.T) {
		svc, _, source := seedSplittable(t)
		_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
			source.Revision, splitInputs(t, "100", "0"))
		if !errors.Is(err, mr.ErrInvalidQuantity) {
			t.Fatalf("error = %v, want ErrInvalidQuantity", err)
		}
	})

	t.Run("negative quantity child", func(t *testing.T) {
		svc, _, source := seedSplittable(t)
		_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
			source.Revision, splitInputs(t, "110", "-10"))
		if !errors.Is(err, mr.ErrInvalidQuantity) {
			t.Fatalf("error = %v, want ErrInvalidQuantity", err)
		}
	})
}

// Acceptance test 48: a child unit differing from the source unit is rejected.
// M7 never converts, so a differing unit would silently break conservation.
func TestSplitRejectsChildUnitChange(t *testing.T) {
	svc, _, source := seedSplittable(t)

	children := []mr.SplitChildInput{
		{QuantityValue: "60", QuantityUnit: "bag"},
		{QuantityValue: "40", QuantityUnit: "kg"},
	}
	_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
		source.Revision, children)
	if !errors.Is(err, mr.ErrSplitUnitChanged) {
		t.Fatalf("error = %v, want ErrSplitUnitChanged", err)
	}
}

// Acceptance tests 49, 50, 51: the preconditions of §8.3, each with its own
// sentinel so the contractor learns the actionable reason.
func TestSplitPreconditions(t *testing.T) {
	t.Run("claimed", func(t *testing.T) {
		svc, repo, source := seedSplittable(t)
		claimed := repo.byID[source.ID]
		chainID := "rfq_chain_1"
		claimed.ActiveRFQChainID = &chainID
		repo.byID[source.ID] = claimed

		_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
			claimed.Revision, splitInputs(t, "60", "40"))
		if !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
			t.Fatalf("error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
		}
	})

	for _, status := range []mr.RequirementStatus{
		mr.RequirementStatusSplit, mr.RequirementStatusArchived,
	} {
		t.Run(string(status), func(t *testing.T) {
			svc, repo, source := seedSplittable(t)
			terminal := repo.byID[source.ID]
			terminal.Status = status
			repo.byID[source.ID] = terminal

			_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
				terminal.Revision, splitInputs(t, "60", "40"))
			if !errors.Is(err, mr.ErrRequirementTerminal) {
				t.Fatalf("error = %v, want ErrRequirementTerminal", err)
			}
		})
	}

	// Acceptance test 51 — §20 defers multi-level splitting.
	t.Run("recursive split of a split child", func(t *testing.T) {
		svc, repo, source := seedSplittable(t)
		ctx := context.Background()

		result, err := svc.SplitRequirement(ctx, "company_a", "user_1", source.ID,
			source.Revision, splitInputs(t, "60", "40"))
		if err != nil {
			t.Fatal(err)
		}
		child := result.Children[0]

		// Review the child so only the recursion rule can reject it.
		reviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", child.ID, child.Revision)
		if err != nil {
			t.Fatal(err)
		}

		_, err = svc.SplitRequirement(ctx, "company_a", "user_1", reviewed.ID,
			reviewed.Revision, splitInputs(t, "30", "30"))
		if !errors.Is(err, mr.ErrRecursiveSplitNotSupported) {
			t.Fatalf("error = %v, want ErrRecursiveSplitNotSupported", err)
		}
		_ = repo
	})

	t.Run("stale revision", func(t *testing.T) {
		svc, _, source := seedSplittable(t)
		_, err := svc.SplitRequirement(context.Background(), "company_a", "user_1", source.ID,
			source.Revision-1, splitInputs(t, "60", "40"))
		if !errors.Is(err, mr.ErrRevisionMismatch) {
			t.Fatalf("error = %v, want ErrRevisionMismatch", err)
		}
	})

	t.Run("foreign company", func(t *testing.T) {
		svc, _, source := seedSplittable(t)
		_, err := svc.SplitRequirement(context.Background(), "company_b", "user_1", source.ID,
			source.Revision, splitInputs(t, "60", "40"))
		if !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
			t.Fatalf("error = %v, want ErrMaterialRequirementNotFound", err)
		}
	})
}

// Acceptance test 54: after a split the source is neither RFQ-eligible nor
// splittable again.
func TestSplitSourceIsNeitherEligibleNorSplittableAfterwards(t *testing.T) {
	svc, repo, source := seedSplittable(t)
	ctx := context.Background()

	if _, err := svc.SplitRequirement(ctx, "company_a", "user_1", source.ID,
		source.Revision, splitInputs(t, "60", "40")); err != nil {
		t.Fatal(err)
	}

	after, err := repo.FindByID(ctx, "company_a", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.IsRFQEligible() {
		t.Error("a split source must never be RFQ-eligible")
	}
	if err := after.SplittableErr(); !errors.Is(err, mr.ErrRequirementTerminal) {
		t.Errorf("SplittableErr = %v, want ErrRequirementTerminal", err)
	}
}

// --- Idempotent recovery (acceptance test 53, design spec §8.4) ---

// The manifest is written in STEP 1, before any child exists. That is what lets
// a retry create exactly the same children: without it, a crash would leave no
// record of what the split was meant to produce.
func TestSplitWritesManifestBeforeCreatingChildren(t *testing.T) {
	svc, repo, source := seedSplittable(t)
	ctx := context.Background()

	// Fail every child insert, so the run stops immediately after step 1.
	repo.failCreate = errors.New("boom")

	_, err := svc.SplitRequirement(ctx, "company_a", "user_1", source.ID,
		source.Revision, splitInputs(t, "60", "40"))
	if err == nil {
		t.Fatal("expected the split to fail when child creation fails")
	}

	after, err := repo.FindByID(ctx, "company_a", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != mr.RequirementStatusSplit {
		t.Errorf("source Status = %q, want split — step 1 committed and is NOT rolled back",
			after.Status)
	}
	if after.SplitState != mr.SplitStateCreating {
		t.Errorf("source SplitState = %q, want creating — the interruption must be visible",
			after.SplitState)
	}
	if len(after.SplitChildren) != 2 {
		t.Fatalf("manifest has %d entries, want 2 written before any child existed",
			len(after.SplitChildren))
	}
	for i, ref := range after.SplitChildren {
		if ref.RequirementID == "" {
			t.Errorf("manifest entry %d has no pre-generated ID — a retry could not be idempotent", i)
		}
		if ref.Sequence != i {
			t.Errorf("manifest entry %d Sequence = %d, want %d", i, ref.Sequence, i)
		}
	}
}

// Acceptance test 53: reconcile creates exactly the MISSING children and never
// duplicates the ones that already exist.
func TestReconcileSplitCreatesOnlyMissingChildren(t *testing.T) {
	svc, repo, source := seedSplittable(t)
	ctx := context.Background()

	// Fail only the SECOND child, so the split is genuinely half-applied.
	repo.failCreateAfter = 1
	repo.failCreate = errors.New("boom")

	if _, err := svc.SplitRequirement(ctx, "company_a", "user_1", source.ID,
		source.Revision, splitInputs(t, "60", "40")); err == nil {
		t.Fatal("expected the split to fail on the second child")
	}

	interrupted, err := repo.FindByID(ctx, "company_a", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.SplitState != mr.SplitStateCreating {
		t.Fatalf("SplitState = %q, want creating", interrupted.SplitState)
	}
	if countSplitChildren(repo, source.ID) != 1 {
		t.Fatalf("got %d children after the interrupted split, want 1",
			countSplitChildren(repo, source.ID))
	}

	// Recovery.
	repo.failCreate = nil
	repo.failCreateAfter = 0
	createsBefore := repo.createCalls

	result, err := svc.ReconcileSplit(ctx, "company_a", "user_1", source.ID, interrupted.Revision)
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}

	if got := repo.createCalls - createsBefore; got != 1 {
		t.Errorf("reconcile attempted %d creates, want exactly 1 (only the missing child)", got)
	}
	if countSplitChildren(repo, source.ID) != 2 {
		t.Errorf("got %d children after reconcile, want 2", countSplitChildren(repo, source.ID))
	}
	if len(result.Children) != 2 {
		t.Errorf("reconcile returned %d children, want the full set of 2", len(result.Children))
	}

	after, err := repo.FindByID(ctx, "company_a", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SplitState != mr.SplitStateCompleted {
		t.Errorf("SplitState = %q, want completed after reconcile", after.SplitState)
	}

	// Conservation still holds across the recovered set.
	total := decimal.Zero
	for _, c := range result.Children {
		total = total.Add(c.RequiredQuantity.Value)
	}
	if !total.Equal(decimal.RequireFromString("100")) {
		t.Errorf("recovered children sum to %s, want 100", total.String())
	}
}

// Reconciling an already-complete split is a no-op, so a duplicated recovery
// request cannot create anything.
func TestReconcileSplitIsIdempotentOnACompletedSplit(t *testing.T) {
	svc, repo, source := seedSplittable(t)
	ctx := context.Background()

	if _, err := svc.SplitRequirement(ctx, "company_a", "user_1", source.ID,
		source.Revision, splitInputs(t, "60", "40")); err != nil {
		t.Fatal(err)
	}
	completed, err := repo.FindByID(ctx, "company_a", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	createsBefore := repo.createCalls

	result, err := svc.ReconcileSplit(ctx, "company_a", "user_1", source.ID, completed.Revision)
	if err != nil {
		t.Fatalf("reconcile on a completed split must be a no-op, got %v", err)
	}
	if repo.createCalls != createsBefore {
		t.Errorf("reconcile attempted %d creates on a completed split, want 0",
			repo.createCalls-createsBefore)
	}
	if len(result.Children) != 2 {
		t.Errorf("reconcile returned %d children, want the existing 2", len(result.Children))
	}
}

// Reconciling a requirement that was never split is refused — there is no
// manifest to work from.
func TestReconcileSplitRejectsARequirementWithNoManifest(t *testing.T) {
	svc, _, source := seedSplittable(t)

	_, err := svc.ReconcileSplit(context.Background(), "company_a", "user_1",
		source.ID, source.Revision)
	if !errors.Is(err, mr.ErrNoSplitToReconcile) {
		t.Fatalf("error = %v, want ErrNoSplitToReconcile", err)
	}
}

// --- helpers ---

func countSplitChildren(repo *fakeRequirementRepo, sourceID string) int {
	n := 0
	for _, r := range repo.byID {
		if r.SplitFromRequirementID != nil && *r.SplitFromRequirementID == sourceID {
			n++
		}
	}
	return n
}

// §8.2's "children do not copy the source's aggregate claims" rule, tested
// against a GENERATED anchor.
//
// TestSplitConservesQuantityAndMarksSourceTerminal splits a MANUAL requirement,
// which has no SourceCostItemIDs, fingerprint or aggregation key to leak — so
// its §8.2 assertions hold vacuously. Mutation testing exposed that: making
// children inherit the source's aggregate identity left every test green.
//
// Only a cost_item anchor has those fields populated, so only this test can
// actually catch the leak.
func TestSplitOfGeneratedAnchorDoesNotCopyAggregateIdentityToChildren(t *testing.T) {
	svc, repo, _, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "60", "bag"},
		{"c2", "work_1", "material_1", "40", "bag"},
	})
	ctx := context.Background()

	anchor := generate(t, svc).Created[0]
	// Confirm the source really does carry an aggregate identity, so a failure
	// below means a leak rather than an empty-vs-empty comparison.
	if len(anchor.SourceCostItemIDs) != 2 || anchor.SourceFingerprint == "" ||
		anchor.SourceAggregationKey == "" || anchor.SourceQuantity == nil {
		t.Fatalf("setup: the generated anchor must carry a full aggregate identity, got %+v", anchor)
	}

	reviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", anchor.ID, anchor.Revision)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.SplitRequirement(ctx, "company_a", "user_1", reviewed.ID,
		reviewed.Revision, splitInputs(t, "60", "40"))
	if err != nil {
		t.Fatalf("split failed: %v", err)
	}

	for i, c := range result.Children {
		if len(c.SourceCostItemIDs) != 0 {
			t.Errorf("child %d copied SourceCostItemIDs %v — each child would appear "+
				"independently backed by the FULL original aggregate", i, c.SourceCostItemIDs)
		}
		if c.SourceAggregationKey != "" {
			t.Errorf("child %d copied SourceAggregationKey %q — it would collide with the "+
				"source anchor on the unique index", i, c.SourceAggregationKey)
		}
		if c.SourceAggregationUnit != "" {
			t.Errorf("child %d copied SourceAggregationUnit %q", i, c.SourceAggregationUnit)
		}
		if c.SourceFingerprint != "" {
			t.Errorf("child %d copied SourceFingerprint", i)
		}
		if c.SourceQuantity != nil {
			t.Errorf("child %d copied SourceQuantity %v", i, c.SourceQuantity)
		}
		if c.SourceSyncedAt != nil {
			t.Errorf("child %d copied SourceSyncedAt", i)
		}
		// Provenance resolves CostItems -> source -> children (§8.2).
		if c.SplitFromRequirementID == nil || *c.SplitFromRequirementID != anchor.ID {
			t.Errorf("child %d does not point back at the source anchor", i)
		}
	}

	// The source anchor keeps its aggregate identity: re-sync continues against
	// it, and the discrepancy surfaces at the split-group level (§3.4, §8.2).
	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceAggregationKey != anchor.SourceAggregationKey ||
		after.SourceFingerprint != anchor.SourceFingerprint {
		t.Error("the split source must retain its own aggregate identity for re-sync")
	}
}
