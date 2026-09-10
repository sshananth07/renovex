package materialrequirements_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// B4 proves the detection-vs-resolution write split (design spec §5.8) — the
// rule that makes merge correct.
//
// B1 already proves the REPOSITORY half
// (TestMongoUpdateSyncStateLeavesAcceptedSnapshotIntact). These tests prove the
// SERVICE half and the arithmetic that depends on it: if detection had advanced
// SourceQuantity to the proposed value, merge's proposed − accepted delta would
// collapse to zero and the contractor's adjustment would be silently discarded.

// seedDiscrepancy generates one anchor from a single CostItem row, then moves
// the source so a second generation run finds a discrepancy.
//
// It returns the service, the repo, and the anchor as it stood BEFORE the
// discrepancy-producing run, so a test can compare the accepted snapshot
// field-by-field across detection.
func seedDiscrepancy(t *testing.T, acceptedQty, proposedQty string) (
	*mr.Service, *fakeRequirementRepo, mr.MaterialRequirement) {
	t.Helper()

	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", acceptedQty, "bag"},
	})

	first := generate(t, svc)
	if first.CreatedCount != 1 {
		t.Fatalf("setup: CreatedCount = %d, want 1", first.CreatedCount)
	}
	anchor := first.Created[0]
	if anchor.SourceSyncState != mr.SourceSyncStateClean {
		t.Fatalf("setup: a fresh anchor must be clean, got %q", anchor.SourceSyncState)
	}

	// Move the source: a second contributing CostItem raises the aggregate.
	delta := decimal.RequireFromString(proposedQty).Sub(decimal.RequireFromString(acceptedQty))
	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", acceptedQty, "bag"},
		{"c2", "work_1", "material_1", delta.String(), "bag"},
	}

	return svc, repo, anchor
}

// Acceptance tests 22 and 23. Detection writes SourceSyncState and
// SourceCheckedAt — and NOTHING else. Asserted field-by-field rather than by
// comparing whole structs, so a failure names the exact field that leaked.
func TestDetectionWritesOnlySyncStateAndCheckedAt(t *testing.T) {
	svc, repo, before := seedDiscrepancy(t, "33", "41")

	got := generate(t, svc)
	if got.DiscrepancyCount != 1 {
		t.Fatalf("DiscrepancyCount = %d, want 1 — the run must find the moved source", got.DiscrepancyCount)
	}

	after, err := repo.FindByID(context.Background(), "company_a", before.ID)
	if err != nil {
		t.Fatal(err)
	}

	// --- The accepted snapshot must be byte-identical (§5.8, AT22). ---
	if after.SourceFingerprint != before.SourceFingerprint {
		t.Errorf("SourceFingerprint = %q, want the accepted %q unchanged — detection must not re-baseline",
			after.SourceFingerprint, before.SourceFingerprint)
	}
	if !equalStrings(after.SourceCostItemIDs, before.SourceCostItemIDs) {
		t.Errorf("SourceCostItemIDs = %v, want the accepted %v unchanged",
			after.SourceCostItemIDs, before.SourceCostItemIDs)
	}
	if after.SourceQuantity == nil || before.SourceQuantity == nil {
		t.Fatalf("SourceQuantity must be present on a generated anchor before and after detection")
	}
	if !after.SourceQuantity.Value.Equal(before.SourceQuantity.Value) {
		t.Errorf("SourceQuantity.Value = %s, want the accepted %s unchanged — merge computes proposed − accepted",
			after.SourceQuantity.Value.String(), before.SourceQuantity.Value.String())
	}
	if after.SourceQuantity.Unit != before.SourceQuantity.Unit {
		t.Errorf("SourceQuantity.Unit = %q, want the accepted %q unchanged",
			after.SourceQuantity.Unit, before.SourceQuantity.Unit)
	}
	if after.SourceSyncedAt == nil || before.SourceSyncedAt == nil {
		t.Fatalf("SourceSyncedAt must be present before and after detection")
	}
	if !after.SourceSyncedAt.Equal(*before.SourceSyncedAt) {
		t.Errorf("SourceSyncedAt = %v, want the accepted %v unchanged — it timestamps the ACCEPTED snapshot",
			*after.SourceSyncedAt, *before.SourceSyncedAt)
	}

	// --- Contractor fields must be untouched too (§5.8 final row). ---
	if !after.RequiredQuantity.Value.Equal(before.RequiredQuantity.Value) {
		t.Errorf("RequiredQuantity.Value = %s, want %s — detection must never write a contractor field",
			after.RequiredQuantity.Value.String(), before.RequiredQuantity.Value.String())
	}
	if after.Status != before.Status {
		t.Errorf("Status = %q, want %q unchanged", after.Status, before.Status)
	}

	// --- Detection output DID advance (AT23). ---
	if after.SourceSyncState != mr.SourceSyncStateChangeDetected {
		t.Errorf("SourceSyncState = %q, want change_detected", after.SourceSyncState)
	}
	if after.SourceCheckedAt == nil {
		t.Fatalf("SourceCheckedAt must be set by detection")
	}
	// Not asserted as strictly-after: two runs in the same test can land on one
	// clock tick, which would make this flaky for a reason unrelated to §5.8.
	// What must hold is that detection re-stamped the field rather than leaving
	// it frozen at the accepted snapshot's time — and, critically, that it did
	// NOT drag SourceSyncedAt along with it, which is asserted above.
	// Not asserted as strictly-after: two runs in the same test can land on one
	// clock tick, which would make this flaky for a reason unrelated to §5.8.
	// The load-bearing assertion is SourceSyncedAt's equality above — detection
	// re-stamping the ACCEPTED snapshot's timestamp is the actual violation.
	if before.SourceCheckedAt != nil && after.SourceCheckedAt.Before(*before.SourceCheckedAt) {
		t.Errorf("SourceCheckedAt = %v, went backwards from %v",
			*after.SourceCheckedAt, *before.SourceCheckedAt)
	}
	if repo.syncStateCalls != 1 {
		t.Errorf("UpdateSyncState called %d times, want exactly 1 — detection converges through the narrow method only",
			repo.syncStateCalls)
	}
}

// Acceptance test 25 — the arithmetic §5.8 exists to protect.
//
//	accepted 33, contractor 35, proposed 41  ->  delta 8  ->  merged 43
//
// Verified AFTER an intervening detection run. That ordering is the whole
// point: if detection had advanced the accepted snapshot to 41, the delta would
// be 0 and the result 35 — the contractor's +2 adjustment silently discarded.
func TestMergeAddsProposedMinusAcceptedDeltaAfterDetectionRun(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()

	// The contractor adjusts 33 -> 35 before the source moves are detected.
	adjusted, err := svc.UpdateRequirement(ctx, "company_a", "user_1", anchor.ID, anchor.Revision,
		mr.UpdateRequirementInput{QuantityValue: strPtr("35")})
	if err != nil {
		t.Fatalf("contractor adjustment failed: %v", err)
	}
	if !adjusted.RequiredQuantity.Value.Equal(decimal.RequireFromString("35")) {
		t.Fatalf("setup: RequiredQuantity = %s, want 35", adjusted.RequiredQuantity.Value.String())
	}

	// The intervening detection run. This is what would corrupt the delta if
	// detection were allowed to write the accepted snapshot.
	got := generate(t, svc)
	if got.DiscrepancyCount != 1 {
		t.Fatalf("DiscrepancyCount = %d, want 1", got.DiscrepancyCount)
	}

	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatalf("GetSourceDiscrepancy: %v", err)
	}
	if !proposal.ProposedQuantity.Value.Equal(decimal.RequireFromString("41")) {
		t.Fatalf("proposed quantity = %s, want 41", proposal.ProposedQuantity.Value.String())
	}
	if !proposal.AcceptedQuantity.Value.Equal(decimal.RequireFromString("33")) {
		t.Fatalf("accepted quantity = %s, want 33 — the snapshot must have survived detection",
			proposal.AcceptedQuantity.Value.String())
	}

	merged, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionMerge,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	if want := decimal.RequireFromString("43"); !merged.RequiredQuantity.Value.Equal(want) {
		t.Fatalf("merged RequiredQuantity = %s, want 43 (35 + (41 − 33)) — "+
			"a result of 35 means detection overwrote the accepted snapshot",
			merged.RequiredQuantity.Value.String())
	}

	// The snapshot refreshes to proposed and the anchor returns to clean/draft.
	if merged.SourceQuantity == nil || !merged.SourceQuantity.Value.Equal(decimal.RequireFromString("41")) {
		t.Errorf("post-merge SourceQuantity = %v, want the proposed 41", merged.SourceQuantity)
	}
	if merged.SourceFingerprint != proposal.ProposedFingerprint {
		t.Errorf("post-merge SourceFingerprint = %q, want the proposed %q",
			merged.SourceFingerprint, proposal.ProposedFingerprint)
	}
	if merged.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("post-merge SourceSyncState = %q, want clean", merged.SourceSyncState)
	}
	if merged.Status != mr.RequirementStatusDraft {
		t.Errorf("post-merge Status = %q, want draft — a quantity change re-opens review", merged.Status)
	}

	// A second detection run must now find nothing: the accepted snapshot
	// matches the current source, so the discrepancy is genuinely resolved
	// rather than merely reported once.
	after := generate(t, svc)
	if after.DiscrepancyCount != 0 {
		t.Errorf("post-merge DiscrepancyCount = %d, want 0", after.DiscrepancyCount)
	}
	if after.UnchangedCount != 1 {
		t.Errorf("post-merge UnchangedCount = %d, want 1", after.UnchangedCount)
	}
}

// A stale expectedProposedFingerprint applies nothing (design spec §5.5,
// acceptance test 32). Without this the contractor could merge against a
// proposal that no longer describes the source.
func TestMergeRejectsStaleProposedFingerprint(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()
	generate(t, svc)

	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionMerge,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: "stale-fingerprint",
		})
	if !errors.Is(err, mr.ErrMaterialRequirementDiscrepancyChanged) {
		t.Fatalf("error = %v, want ErrMaterialRequirementDiscrepancyChanged", err)
	}

	unchanged, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.RequiredQuantity.Value.Equal(current.RequiredQuantity.Value) {
		t.Errorf("RequiredQuantity = %s, want %s — a stale proposal must apply NOTHING",
			unchanged.RequiredQuantity.Value.String(), current.RequiredQuantity.Value.String())
	}
	if unchanged.SourceFingerprint != current.SourceFingerprint {
		t.Errorf("SourceFingerprint changed on a rejected resolution")
	}
}

// A stale expectedRevision applies nothing (acceptance test 33).
func TestMergeRejectsStaleRevision(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()
	generate(t, svc)

	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionMerge,
			ExpectedRevision:            current.Revision - 1,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if !errors.Is(err, mr.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}

	unchanged, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.RequiredQuantity.Value.Equal(current.RequiredQuantity.Value) {
		t.Errorf("RequiredQuantity changed despite a stale revision")
	}
}

// merge is withheld when the contractor has converted to a different unit
// (acceptance test 26). M7 never converts units, so adding a source-unit delta
// to a differently-united quantity would be arithmetically meaningless.
func TestMergeWithheldWhenContractorUnitDiffersFromSourceUnit(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()

	// The contractor converts bag -> kg. SourceAggregationUnit stays "bag".
	converted, err := svc.UpdateRequirement(ctx, "company_a", "user_1", anchor.ID, anchor.Revision,
		mr.UpdateRequirementInput{QuantityUnit: strPtr("kg")})
	if err != nil {
		t.Fatalf("unit conversion failed: %v", err)
	}
	if converted.SourceAggregationUnit != "bag" {
		t.Fatalf("setup: SourceAggregationUnit = %q, want bag — it is immutable",
			converted.SourceAggregationUnit)
	}

	generate(t, svc)
	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	if containsAction(proposal.AvailableActions, mr.ResolutionActionMerge) {
		t.Errorf("availableActions = %v, must not offer merge across differing units",
			proposal.AvailableActions)
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionMerge,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if !errors.Is(err, mr.ErrResolutionActionNotAvailable) {
		t.Fatalf("error = %v, want ErrResolutionActionNotAvailable", err)
	}
}

// merge is rejected when the result would be <= 0 (acceptance test 27). A
// source reduction larger than the contractor's quantity cannot produce a
// non-positive requirement.
func TestMergeRejectedWhenResultWouldBeNonPositive(t *testing.T) {
	// accepted 33, source falls to 10, contractor holds 20 -> 20 + (10 − 33) < 0.
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	ctx := context.Background()

	first := generate(t, svc)
	anchor := first.Created[0]

	adjusted, err := svc.UpdateRequirement(ctx, "company_a", "user_1", anchor.ID, anchor.Revision,
		mr.UpdateRequirementInput{QuantityValue: strPtr("20")})
	if err != nil {
		t.Fatal(err)
	}

	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "10", "bag"},
	}
	generate(t, svc)

	current, err := repo.FindByID(ctx, "company_a", adjusted.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	if containsAction(proposal.AvailableActions, mr.ResolutionActionMerge) {
		t.Errorf("availableActions = %v, must not offer merge when the result would be <= 0",
			proposal.AvailableActions)
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionMerge,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if !errors.Is(err, mr.ErrResolutionActionNotAvailable) {
		t.Fatalf("error = %v, want ErrResolutionActionNotAvailable", err)
	}

	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.RequiredQuantity.Value.Equal(decimal.RequireFromString("20")) {
		t.Errorf("RequiredQuantity = %s, want 20 unchanged", after.RequiredQuantity.Value.String())
	}
}

// A claimed requirement refuses every resolution action (design spec §2.3).
func TestMergeRejectedOnClaimedRequirement(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()
	generate(t, svc)

	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Claim it directly in the fake, as internal/rfqs would.
	claimed, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	chainID := "rfq_chain_1"
	claimed.ActiveRFQChainID = &chainID
	repo.byID[claimed.ID] = claimed

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionMerge,
			ExpectedRevision:            claimed.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
		t.Fatalf("error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
}

// --- helpers ---

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsAction(actions []mr.ResolutionAction, want mr.ResolutionAction) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}
