package materialrequirements_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// --- fake cost source ---

// costRow is one eligible material CostItem row as the costs capability yields
// it (design spec §1.2). Ineligible rows never appear here: costs filters them
// and reports them only through the five intrinsic counters.
type costRow struct {
	costItemID string
	workItemID string
	materialID string
	quantity   string
	unit       string
}

type fakeCostSource struct {
	// key: companyID|projectID
	rows map[string][]costRow

	// intrinsic counters costs reports for rows it filtered out itself
	nonMaterial, missingMaterial, missingWorkItem, missingQty, blankUnit int

	// visitErr, when set, is returned from the visitor to prove the walk aborts
	visitErr error
	calls    int
}

func (f *fakeCostSource) VisitEligibleMaterialCostItems(_ context.Context, companyID, projectID string,
	visit func(costItemID, workItemID, materialID string, quantity decimal.Decimal, quantityUnit string) error,
) (int, int, int, int, int, error) {
	for _, r := range f.rows[companyID+"|"+projectID] {
		f.calls++
		if f.visitErr != nil {
			return 0, 0, 0, 0, 0, f.visitErr
		}
		if err := visit(r.costItemID, r.workItemID, r.materialID,
			decimal.RequireFromString(r.quantity), r.unit); err != nil {
			return 0, 0, 0, 0, 0, err
		}
	}
	return f.nonMaterial, f.missingMaterial, f.missingWorkItem, f.missingQty, f.blankUnit, nil
}

// newGenService wires a service with a controllable cost source.
func newGenService(t *testing.T, rows []costRow) (*mr.Service, *fakeRequirementRepo, *fakeCostSource, *recordedAudit) {
	t.Helper()
	repo := newFakeRepo()
	rec := &recordedAudit{}
	source := &fakeCostSource{rows: map[string][]costRow{"company_a|project_1": rows}}
	svc := mr.NewService(repo,
		fakeProjectLookup{projects: map[string]string{"project_1": "company_a", "project_2": "company_a"}},
		fakeWorkItemLookup{
			found: map[string]bool{
				"company_a|work_1|project_1": true,
				"company_a|work_2|project_1": true,
				"company_a|work_x|project_1": true,
			},
			cancelled: map[string]bool{"company_a|work_x|project_1": true},
		},
		fakeMaterialLookup{materials: map[string]fakeMaterial{
			"company_a|material_1": {name: "Portland Cement", unit: "bag", specification: "OPC 50kg"},
			"company_a|material_2": {name: "River Sand", unit: "m3", specification: "washed"},
		}},
		source,
		fakeAudit{rec: rec},
	)
	return svc, repo, source, rec
}

func generate(t *testing.T, svc *mr.Service) mr.GenerationResult {
	t.Helper()
	got, err := svc.GenerateFromCostItems(context.Background(), "company_a", "user_1", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return got
}

// --- Aggregation (design spec §3.2) ---

func TestGenerateAggregatesByProjectWorkItemMaterialUnit(t *testing.T) {
	svc, _, _, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "25.5", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
		{"c3", "work_2", "material_1", "10", "bag"},
	})

	got := generate(t, svc)
	if got.CreatedCount != 2 {
		t.Fatalf("CreatedCount = %d, want 2 (one per work item)", got.CreatedCount)
	}

	byWorkItem := map[string]decimal.Decimal{}
	for _, c := range got.Created {
		byWorkItem[*c.WorkItemID] = c.RequiredQuantity.Value
	}
	if want := decimal.RequireFromString("33.5"); !byWorkItem["work_1"].Equal(want) {
		t.Errorf("work_1 quantity = %s, want 33.5 (25.5 + 8 summed exactly)", byWorkItem["work_1"].String())
	}
	if want := decimal.RequireFromString("10"); !byWorkItem["work_2"].Equal(want) {
		t.Errorf("work_2 quantity = %s, want 10", byWorkItem["work_2"].String())
	}
}

// The same material in two different units yields TWO requirements. Quantities
// are never summed across units (design spec §3.2).
func TestGenerateNeverSumsAcrossUnits(t *testing.T) {
	svc, _, _, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "60", "bag"},
		{"c2", "work_1", "material_1", "25", "kg"},
	})

	got := generate(t, svc)
	if got.CreatedCount != 2 {
		t.Fatalf("CreatedCount = %d, want 2 — one per unit", got.CreatedCount)
	}
	units := map[string]decimal.Decimal{}
	for _, c := range got.Created {
		units[c.RequiredQuantity.Unit] = c.RequiredQuantity.Value
	}
	if !units["bag"].Equal(decimal.RequireFromString("60")) || !units["kg"].Equal(decimal.RequireFromString("25")) {
		t.Errorf("quantities = %v, want bag:60 and kg:25 kept separate", units)
	}
}

// Generated anchors start as draft with a clean accepted snapshot: review is
// the confirmation gate (design spec §3.6).
func TestGenerateCreatesDraftAnchorsWithAcceptedSnapshot(t *testing.T) {
	svc, _, _, rec := newGenService(t, []costRow{
		{"c2", "work_1", "material_1", "8", "bag"},
		{"c1", "work_1", "material_1", "25.5", "bag"},
	})

	got := generate(t, svc)
	if len(got.Created) != 1 {
		t.Fatalf("expected 1 created anchor, got %d", len(got.Created))
	}
	a := got.Created[0]

	if a.Status != mr.RequirementStatusDraft {
		t.Errorf("Status = %q, want draft", a.Status)
	}
	if a.SourceType != mr.SourceTypeCostItem {
		t.Errorf("SourceType = %q, want cost_item", a.SourceType)
	}
	if a.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean", a.SourceSyncState)
	}
	if a.SourceAggregationUnit != "bag" || a.SourceAggregationKey == "" {
		t.Errorf("aggregation identity wrong: unit=%q key=%q", a.SourceAggregationUnit, a.SourceAggregationKey)
	}
	// Contributing IDs are stored sorted, so the accepted snapshot is canonical.
	if len(a.SourceCostItemIDs) != 2 || a.SourceCostItemIDs[0] != "c1" || a.SourceCostItemIDs[1] != "c2" {
		t.Errorf("SourceCostItemIDs = %v, want sorted [c1 c2]", a.SourceCostItemIDs)
	}
	if a.SourceQuantity == nil || !a.SourceQuantity.Value.Equal(decimal.RequireFromString("33.5")) {
		t.Errorf("SourceQuantity = %v, want 33.5", a.SourceQuantity)
	}
	// RequiredQuantity starts equal to the source aggregate.
	if !a.RequiredQuantity.Value.Equal(a.SourceQuantity.Value) {
		t.Error("a new anchor's RequiredQuantity must equal its SourceQuantity")
	}
	if a.SourceFingerprint == "" || a.SourceSyncedAt == nil || a.SourceCheckedAt == nil {
		t.Error("a new anchor must carry a fingerprint and both timestamps")
	}
	if a.MaterialName != "Portland Cement" || a.Specification != "OPC 50kg" {
		t.Errorf("catalog snapshot wrong: %q / %q", a.MaterialName, a.Specification)
	}
	// A generated anchor is NOT RFQ-eligible until reviewed.
	if a.IsRFQEligible() {
		t.Error("a freshly generated draft must not be RFQ-eligible")
	}
	if rec.generated != 1 {
		t.Errorf("audit generated = %d, want 1", rec.generated)
	}
}

// --- Idempotency (design spec §3.7) ---

func TestGenerateIsIdempotentWithUnchangedCostItems(t *testing.T) {
	svc, repo, _, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})

	first := generate(t, svc)
	if first.CreatedCount != 1 {
		t.Fatalf("first run CreatedCount = %d, want 1", first.CreatedCount)
	}

	second := generate(t, svc)
	if second.CreatedCount != 0 {
		t.Errorf("second run CreatedCount = %d, want 0", second.CreatedCount)
	}
	if second.UnchangedCount != 1 {
		t.Errorf("second run UnchangedCount = %d, want 1", second.UnchangedCount)
	}
	if len(repo.byID) != 1 {
		t.Errorf("expected exactly 1 stored requirement, got %d", len(repo.byID))
	}
}

// A contractor edit to RequiredQuantity survives re-generation untouched
// (design spec §3.4, §5.8).
func TestGenerateNeverOverwritesContractorQuantity(t *testing.T) {
	svc, _, _, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	edited, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{QuantityValue: strPtr("75")})
	if err != nil {
		t.Fatal(err)
	}

	got := generate(t, svc)
	if got.CreatedCount != 0 {
		t.Errorf("CreatedCount = %d, want 0", got.CreatedCount)
	}
	after, err := svc.GetRequirement(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.RequiredQuantity.Value.Equal(decimal.RequireFromString("75")) {
		t.Errorf("RequiredQuantity = %s, want the contractor's 75 preserved",
			after.RequiredQuantity.Value.String())
	}
	_ = edited
}

// Editing the procurement UNIT must not orphan the anchor: the aggregation key
// uses the immutable SourceAggregationUnit, so re-generation still matches
// (design spec §3.3).
func TestGenerateStillMatchesAnchorAfterUnitEdit(t *testing.T) {
	svc, repo, _, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	if _, err := svc.UpdateRequirement(context.Background(), "company_a", "user_1",
		created.ID, created.Revision, mr.UpdateRequirementInput{QuantityUnit: strPtr("tonne")}); err != nil {
		t.Fatal(err)
	}

	got := generate(t, svc)
	if got.CreatedCount != 0 {
		t.Errorf("CreatedCount = %d, want 0 — a unit edit must not create a duplicate anchor", got.CreatedCount)
	}
	if len(repo.byID) != 1 {
		t.Errorf("stored requirements = %d, want 1", len(repo.byID))
	}
}

// A duplicate-key loser reloads the winner and reports it as unchanged rather
// than failing the whole command (design spec §3.7).
func TestGenerateTreatsDuplicateKeyAsUnchanged(t *testing.T) {
	svc, repo, _, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})

	// Pre-insert an anchor with exactly the key generation will compute,
	// simulating a concurrent winner.
	key := mr.ComputeSourceAggregationKey("company_a", "project_1", mr.WorkItemRef("work_1"), "material_1", "bag")
	preexisting := mr.MaterialRequirement{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: strPtr("work_1"),
		MaterialID: "material_1", MaterialName: "Portland Cement",
		RequiredQuantity: mustQty(t, "60", "bag"), CatalogUnit: "bag",
		Status: mr.RequirementStatusDraft, SourceType: mr.SourceTypeCostItem,
		SourceAggregationUnit: "bag", SourceAggregationKey: key,
		SourceFingerprint: mr.ComputeSourceFingerprint("project_1", mr.WorkItemRef("work_1"), "material_1", "bag",
			[]mr.SourceRow{{CostItemID: "c1", Quantity: decimal.RequireFromString("60")}}),
		SourceSyncState: mr.SourceSyncStateClean, SchemaVersion: 1,
	}
	if _, err := repo.Create(context.Background(), preexisting); err != nil {
		t.Fatal(err)
	}

	got := generate(t, svc)
	if got.CreatedCount != 0 {
		t.Errorf("CreatedCount = %d, want 0", got.CreatedCount)
	}
	if got.UnchangedCount != 1 {
		t.Errorf("UnchangedCount = %d, want 1 — the loser must reload the winner", got.UnchangedCount)
	}
	if len(repo.byID) != 1 {
		t.Errorf("stored requirements = %d, want exactly 1", len(repo.byID))
	}
}

// --- The union algorithm (design spec §3.4) ---

// The load-bearing case from correction 4: an anchor whose CostItems have ALL
// been deleted produces no computed key, so only the union can discover it.
func TestGenerateDiscoversSourceRemovedForVanishedCostItems(t *testing.T) {
	svc, _, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	// Every contributing CostItem is deleted.
	source.rows["company_a|project_1"] = nil

	got := generate(t, svc)
	if got.SourceRemovedCount != 1 {
		t.Fatalf("SourceRemovedCount = %d, want 1 — the union must visit existing-only keys", got.SourceRemovedCount)
	}
	if got.CreatedCount != 0 {
		t.Errorf("CreatedCount = %d, want 0", got.CreatedCount)
	}

	after, err := svc.GetRequirement(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceSyncState != mr.SourceSyncStateSourceRemoved {
		t.Errorf("SourceSyncState = %q, want source_removed", after.SourceSyncState)
	}
	// An unresolved discrepancy blocks RFQ eligibility (design spec §8.5).
	if after.IsRFQEligible() {
		t.Error("a source_removed requirement must not be RFQ-eligible")
	}
}

// Once the EMPTY aggregate is the accepted snapshot, later runs with still-zero
// rows must compare clean rather than re-reporting source_removed forever.
//
// This is the §5.3 rule that both populated and empty aggregates go through the
// SAME fingerprint comparison. A shortcut returning source_removed whenever rows
// are empty would warn on every run after keep_current had accepted the removal.
// B5 exercises this through keep_current; here it is pinned at the detection
// level, where the shortcut would live.
func TestGenerateStaysCleanWhenEmptyAggregateIsAlreadyAccepted(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	// The CostItems vanish and the contractor accepts that reality: the accepted
	// snapshot becomes the empty aggregate. (keep_current lands in B5; setting the
	// accepted snapshot directly is what that action will do.)
	source.rows["company_a|project_1"] = nil
	if got := generate(t, svc); got.SourceRemovedCount != 1 {
		t.Fatalf("expected source_removed first, got %+v", got)
	}

	stored := repo.byID[created.ID]
	stored.SourceFingerprint = mr.ComputeSourceFingerprint("project_1",
		mr.WorkItemRefFromPointer(stored.WorkItemID), stored.MaterialID,
		stored.SourceAggregationUnit, nil)
	emptyQty := mustQty(t, "0", stored.SourceAggregationUnit)
	stored.SourceQuantity = &emptyQty
	stored.SourceCostItemIDs = nil
	stored.SourceSyncState = mr.SourceSyncStateClean
	repo.byID[created.ID] = stored

	got := generate(t, svc)
	if got.SourceRemovedCount != 0 {
		t.Errorf("SourceRemovedCount = %d, want 0 — an accepted empty aggregate must not warn again",
			got.SourceRemovedCount)
	}
	if got.UnchangedCount != 1 {
		t.Errorf("UnchangedCount = %d, want 1", got.UnchangedCount)
	}
	after := repo.byID[created.ID]
	if after.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean — the empty aggregate is the accepted state",
			after.SourceSyncState)
	}
}

func TestGenerateDetectsQuantityChange(t *testing.T) {
	svc, _, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	source.rows["company_a|project_1"] = []costRow{{"c1", "work_1", "material_1", "75", "bag"}}

	got := generate(t, svc)
	if got.DiscrepancyCount != 1 {
		t.Fatalf("DiscrepancyCount = %d, want 1", got.DiscrepancyCount)
	}
	after, err := svc.GetRequirement(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceSyncState != mr.SourceSyncStateChangeDetected {
		t.Errorf("SourceSyncState = %q, want change_detected", after.SourceSyncState)
	}
}

// A membership change at the SAME total still counts as a discrepancy: the
// fingerprint covers the contributing set, not merely the sum (design spec §5.2).
func TestGenerateDetectsMembershipChangeAtSameTotal(t *testing.T) {
	svc, _, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	// Same total 60, but now contributed by two CostItems.
	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "20", "bag"},
		{"c2", "work_1", "material_1", "40", "bag"},
	}

	got := generate(t, svc)
	if got.DiscrepancyCount != 1 {
		t.Fatalf("DiscrepancyCount = %d, want 1 — membership changed", got.DiscrepancyCount)
	}
	after, _ := svc.GetRequirement(context.Background(), "company_a", created.ID)
	if after.SourceSyncState != mr.SourceSyncStateChangeDetected {
		t.Errorf("SourceSyncState = %q, want change_detected", after.SourceSyncState)
	}
}

// Reversion is handled by the same comparison: restoring the original CostItems
// returns the anchor to clean automatically (design spec §5.3).
func TestGenerateReturnsToCleanWhenSourceReverts(t *testing.T) {
	original := []costRow{{"c1", "work_1", "material_1", "60", "bag"}}
	svc, _, source, _ := newGenService(t, original)
	created := generate(t, svc).Created[0]

	source.rows["company_a|project_1"] = []costRow{{"c1", "work_1", "material_1", "75", "bag"}}
	if got := generate(t, svc); got.DiscrepancyCount != 1 {
		t.Fatalf("expected a discrepancy first, got %+v", got)
	}

	source.rows["company_a|project_1"] = original
	got := generate(t, svc)
	if got.UnchangedCount != 1 {
		t.Errorf("UnchangedCount = %d, want 1 after reversion", got.UnchangedCount)
	}
	after, _ := svc.GetRequirement(context.Background(), "company_a", created.ID)
	if after.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean — reversion must self-heal", after.SourceSyncState)
	}
}

// --- Terminal anchors are classified, never recreated (design spec §3.4) ---

func TestGenerateNeverRecreatesTerminalAnchors(t *testing.T) {
	for _, status := range []mr.RequirementStatus{mr.RequirementStatusSplit, mr.RequirementStatusArchived} {
		t.Run(string(status), func(t *testing.T) {
			svc, repo, _, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
			created := generate(t, svc).Created[0]

			stored := repo.byID[created.ID]
			stored.Status = status
			repo.byID[created.ID] = stored
			createsBefore := repo.createCalls

			got := generate(t, svc)
			if got.CreatedCount != 0 {
				t.Errorf("CreatedCount = %d, want 0 — a %s anchor must never be recreated",
					got.CreatedCount, status)
			}
			if len(repo.byID) != 1 {
				t.Errorf("stored requirements = %d, want 1", len(repo.byID))
			}
			// It is classified, not skipped.
			if got.SkippedCount != 0 {
				t.Errorf("SkippedCount = %d, want 0 — a terminal anchor is classified, not skipped", got.SkippedCount)
			}
			if got.UnchangedCount != 1 {
				t.Errorf("UnchangedCount = %d, want 1", got.UnchangedCount)
			}
			// The MECHANISM matters, not just the counts: the terminal anchor must
			// be recognised from existingAnchors and classified directly. Relying
			// on the unique index to reject a duplicate insert would produce the
			// same counts while attempting a pointless write and — worse — never
			// converging the anchor's sync state (design spec §3.4, §5.8).
			if repo.createCalls != createsBefore {
				t.Errorf("Create was attempted %d time(s): a terminal anchor must be "+
					"recognised from existingAnchors, not rediscovered via a duplicate-key rejection",
					repo.createCalls-createsBefore)
			}
		})
	}
}

// A terminal anchor whose source has drifted must still have its detection state
// converged. If terminal anchors were excluded from the anchor map, generation
// would attempt a doomed insert and the drift would go unrecorded — the counts
// alone would not reveal it.
func TestGenerateConvergesTerminalAnchorFromExistingAnchorsNotByInsertFailure(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	stored := repo.byID[created.ID]
	stored.Status = mr.RequirementStatusSplit
	repo.byID[created.ID] = stored
	createsBefore := repo.createCalls

	source.rows["company_a|project_1"] = []costRow{{"c1", "work_1", "material_1", "90", "bag"}}
	got := generate(t, svc)

	if got.DiscrepancyCount != 1 {
		t.Fatalf("DiscrepancyCount = %d, want 1", got.DiscrepancyCount)
	}
	if repo.createCalls != createsBefore {
		t.Errorf("Create was attempted for an existing terminal anchor (%d extra call(s))",
			repo.createCalls-createsBefore)
	}
	after := repo.byID[created.ID]
	if after.SourceSyncState != mr.SourceSyncStateChangeDetected {
		t.Errorf("SourceSyncState = %q, want change_detected — a terminal anchor is still a sync anchor",
			after.SourceSyncState)
	}
}

// A discrepancy on a terminal anchor is reported WITH its status, so the
// contractor sees it concerns retired or superseded demand (design spec §3.4).
func TestGenerateReportsAnchorStatusOnTerminalDiscrepancy(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	stored := repo.byID[created.ID]
	stored.Status = mr.RequirementStatusSplit
	repo.byID[created.ID] = stored

	source.rows["company_a|project_1"] = []costRow{{"c1", "work_1", "material_1", "90", "bag"}}

	got := generate(t, svc)
	if got.DiscrepancyCount != 1 {
		t.Fatalf("DiscrepancyCount = %d, want 1", got.DiscrepancyCount)
	}
	if len(got.Discrepancies) != 1 {
		t.Fatalf("expected 1 discrepancy entry, got %d", len(got.Discrepancies))
	}
	d := got.Discrepancies[0]
	if d.AnchorStatus != string(mr.RequirementStatusSplit) {
		t.Errorf("AnchorStatus = %q, want split", d.AnchorStatus)
	}
	if d.SyncState != string(mr.SourceSyncStateChangeDetected) {
		t.Errorf("SyncState = %q, want change_detected", d.SyncState)
	}
}

// Detection converges sync state on a terminal anchor but freezes every
// contractor field (design spec §5.8).
func TestGenerateConvergesSyncStateOnTerminalAnchorWithoutTouchingContractorFields(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "60", "bag"}})
	created := generate(t, svc).Created[0]

	stored := repo.byID[created.ID]
	stored.Status = mr.RequirementStatusArchived
	repo.byID[created.ID] = stored
	quantityBefore := stored.RequiredQuantity.Value
	acceptedBefore := stored.SourceFingerprint
	acceptedQtyBefore := stored.SourceQuantity.Value

	source.rows["company_a|project_1"] = []costRow{{"c1", "work_1", "material_1", "90", "bag"}}
	generate(t, svc)

	after := repo.byID[created.ID]
	if after.SourceSyncState != mr.SourceSyncStateChangeDetected {
		t.Errorf("SourceSyncState = %q, want change_detected", after.SourceSyncState)
	}
	if after.Status != mr.RequirementStatusArchived {
		t.Errorf("Status = %q, want archived unchanged", after.Status)
	}
	if !after.RequiredQuantity.Value.Equal(quantityBefore) {
		t.Error("detection must not touch RequiredQuantity")
	}
	if after.SourceFingerprint != acceptedBefore {
		t.Error("detection must not rewrite the accepted SourceFingerprint")
	}
	if !after.SourceQuantity.Value.Equal(acceptedQtyBefore) {
		t.Error("detection must not rewrite the accepted SourceQuantity")
	}
}

// --- Unit mismatch (design spec §3.5) ---

func TestGenerateFlagsUnitMismatchWithoutConverting(t *testing.T) {
	svc, _, _, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "500", "kg"}, // catalog unit is "bag"
	})

	got := generate(t, svc)
	if len(got.Created) != 1 {
		t.Fatalf("expected 1 created anchor, got %d", len(got.Created))
	}
	a := got.Created[0]
	if !a.UnitMismatch {
		t.Error("UnitMismatch = false, want true")
	}
	if a.UnitMismatchAcknowledged {
		t.Error("a fresh mismatch must be unacknowledged")
	}
	// The CostItem unit is preserved; no conversion is attempted.
	if a.RequiredQuantity.Unit != "kg" || a.SourceAggregationUnit != "kg" {
		t.Errorf("units = %q/%q, want kg preserved", a.RequiredQuantity.Unit, a.SourceAggregationUnit)
	}
	if a.CatalogUnit != "bag" {
		t.Errorf("CatalogUnit = %q, want bag recorded for comparison", a.CatalogUnit)
	}
	if a.IsRFQEligible() {
		t.Error("an unresolved mismatch must block RFQ eligibility")
	}
}

// --- Skips: contextual vs intrinsic (design spec §3.7, §3.8) ---

func TestGenerateReportsContextualSkips(t *testing.T) {
	svc, repo, _, _ := newGenService(t, []costRow{
		{"c1", "work_x", "material_1", "10", "bag"},   // cancelled work item
		{"c2", "work_zzz", "material_1", "10", "bag"}, // unknown work item
		{"c3", "work_1", "material_zzz", "10", "bag"}, // unknown material
		{"c4", "work_1", "material_1", "10", "bag"},   // eligible
	})

	got := generate(t, svc)
	if got.CreatedCount != 1 {
		t.Fatalf("CreatedCount = %d, want 1", got.CreatedCount)
	}
	if got.SkippedCount != 3 {
		t.Fatalf("SkippedCount = %d, want 3", got.SkippedCount)
	}

	reasons := map[string]bool{}
	for _, s := range got.Skipped {
		reasons[s.Reason] = true
	}
	for _, want := range []string{"work_item_cancelled", "work_item_not_found", "material_not_found"} {
		if !reasons[want] {
			t.Errorf("missing skip reason %q (got %v)", want, reasons)
		}
	}
	// No inactive-Material reason exists: M7 has no such concept (§0.3 conflict D).
	if reasons["material_inactive"] {
		t.Error("material_inactive must not exist as a skip reason")
	}
	if len(repo.byID) != 1 {
		t.Errorf("stored requirements = %d, want 1 — skipped rows create nothing", len(repo.byID))
	}
}

// Intrinsic counters come from costs and are reported SEPARATELY from
// contextual skips: they mean different things (design spec §3.8).
func TestGenerateReportsIntrinsicIneligibleCountsSeparately(t *testing.T) {
	svc, _, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "10", "bag"}})
	source.nonMaterial = 12
	source.missingMaterial = 3
	source.missingWorkItem = 1
	source.missingQty = 2
	source.blankUnit = 4

	got := generate(t, svc)
	if got.IntrinsicallyIneligible.NonMaterialCategory != 12 ||
		got.IntrinsicallyIneligible.MissingMaterialID != 3 ||
		got.IntrinsicallyIneligible.MissingWorkItemID != 1 ||
		got.IntrinsicallyIneligible.MissingOrZeroQuantity != 2 ||
		got.IntrinsicallyIneligible.BlankUnit != 4 {
		t.Errorf("intrinsic counters = %+v, want 12/3/1/2/4", got.IntrinsicallyIneligible)
	}
	// Contextual skips are a different bucket and must stay zero here.
	if got.SkippedCount != 0 {
		t.Errorf("SkippedCount = %d, want 0 — intrinsic rows are not contextual skips", got.SkippedCount)
	}
}

// --- Tenancy and error propagation ---

func TestGenerateRejectsForeignProject(t *testing.T) {
	svc, repo, _, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "10", "bag"}})

	_, err := svc.GenerateFromCostItems(context.Background(), "company_a", "user_1", "project_zzz")
	if !errors.Is(err, mr.ErrProjectNotFound) {
		t.Fatalf("error = %v, want ErrProjectNotFound", err)
	}
	if len(repo.byID) != 0 {
		t.Error("a rejected generation must create nothing")
	}
}

func TestGeneratePropagatesCostSourceError(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "10", "bag"}})
	sentinel := errors.New("cost source unavailable")
	source.visitErr = sentinel

	_, err := svc.GenerateFromCostItems(context.Background(), "company_a", "user_1", "project_1")
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the cost-source error propagated", err)
	}
	if len(repo.byID) != 0 {
		t.Error("a failed generation must create nothing")
	}
}

// Generation is scoped to one project: another project's CostItems are never
// aggregated into this project's anchors.
func TestGenerateIsProjectScoped(t *testing.T) {
	svc, _, source, _ := newGenService(t, []costRow{{"c1", "work_1", "material_1", "10", "bag"}})
	source.rows["company_a|project_2"] = []costRow{{"c9", "work_1", "material_1", "99", "bag"}}

	got := generate(t, svc)
	if got.CreatedCount != 1 {
		t.Fatalf("CreatedCount = %d, want 1", got.CreatedCount)
	}
	if !got.Created[0].RequiredQuantity.Value.Equal(decimal.RequireFromString("10")) {
		t.Errorf("quantity = %s, want 10 — project_2's rows must not leak in",
			got.Created[0].RequiredQuantity.Value.String())
	}
}
