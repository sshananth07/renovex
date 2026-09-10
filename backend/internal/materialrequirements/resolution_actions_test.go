package materialrequirements_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// B5 covers the remaining three resolution actions and the §5.6 anchor
// availability matrix. B4's merge tests live in resolution_test.go.

// --- update (design spec §5.6, acceptance test 24) ---

// update ADOPTS the proposed quantity and unit — it does not add. Review resets
// because a quantity change alters what a supplier would be asked to quote.
func TestUpdateAdoptsProposedQuantityAndUnit(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()

	// The contractor adjusts 33 -> 35 BEFORE the update. This is what makes the
	// test able to tell update from merge: adopting yields 41, adding the
	// delta would yield 43. Without the adjustment both produce 41 and the
	// assertion proves nothing — mutation testing caught exactly that.
	adjusted, err := svc.UpdateRequirement(ctx, "company_a", "user_1", anchor.ID, anchor.Revision,
		mr.UpdateRequirementInput{QuantityValue: strPtr("35")})
	if err != nil {
		t.Fatal(err)
	}

	// Review it, so the reset to draft is observable rather than vacuous.
	reviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", anchor.ID, adjusted.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Status != mr.RequirementStatusReviewed {
		t.Fatalf("setup: status = %q, want reviewed", reviewed.Status)
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

	got, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionUpdate,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if want := decimal.RequireFromString("41"); !got.RequiredQuantity.Value.Equal(want) {
		t.Errorf("RequiredQuantity = %s, want the proposed 41 — update ADOPTS the proposal; "+
			"a result of 43 would mean it added the delta like merge",
			got.RequiredQuantity.Value.String())
	}
	if got.RequiredQuantity.Unit != "bag" {
		t.Errorf("RequiredQuantity.Unit = %q, want the proposed source unit bag", got.RequiredQuantity.Unit)
	}
	if got.Status != mr.RequirementStatusDraft {
		t.Errorf("Status = %q, want draft — a quantity change re-opens review", got.Status)
	}
	if got.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean", got.SourceSyncState)
	}
	if got.SourceFingerprint != proposal.ProposedFingerprint {
		t.Errorf("SourceFingerprint was not refreshed to the proposed value")
	}
}

// --- keep_current (acceptance test 28) ---

// keep_current refreshes the accepted snapshot and nothing else. Status must
// survive: a deliberate no-op is not a procurement edit (design spec §5.6).
func TestKeepCurrentRefreshesSnapshotWithoutTouchingQuantityOrStatus(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()

	reviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", anchor.ID, anchor.Revision)
	if err != nil {
		t.Fatal(err)
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

	got, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionKeepCurrent,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if err != nil {
		t.Fatalf("keep_current failed: %v", err)
	}

	if !got.RequiredQuantity.Value.Equal(reviewed.RequiredQuantity.Value) {
		t.Errorf("RequiredQuantity = %s, want %s untouched",
			got.RequiredQuantity.Value.String(), reviewed.RequiredQuantity.Value.String())
	}
	if got.Status != mr.RequirementStatusReviewed {
		t.Errorf("Status = %q, want reviewed — keep_current must NOT reset review", got.Status)
	}
	if got.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean", got.SourceSyncState)
	}
	if got.SourceQuantity == nil || !got.SourceQuantity.Value.Equal(decimal.RequireFromString("41")) {
		t.Errorf("SourceQuantity = %v, want the proposed 41 accepted", got.SourceQuantity)
	}
}

// keep_current on source_removed accepts the EMPTY aggregate, and the next run
// stays clean rather than warning forever (acceptance tests 19, 20).
func TestKeepCurrentAcceptsEmptyAggregateAndStopsWarning(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	ctx := context.Background()

	anchor := generate(t, svc).Created[0]

	// Every contributing CostItem is deleted.
	source.rows["company_a|project_1"] = nil

	removed := generate(t, svc)
	if removed.SourceRemovedCount != 1 {
		t.Fatalf("SourceRemovedCount = %d, want 1", removed.SourceRemovedCount)
	}

	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if containsAction(proposal.AvailableActions, mr.ResolutionActionCreateSeparate) {
		t.Errorf("availableActions = %v, must not offer create_separate on source_removed",
			proposal.AvailableActions)
	}

	got, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionKeepCurrent,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if err != nil {
		t.Fatalf("keep_current on source_removed failed: %v", err)
	}
	if len(got.SourceCostItemIDs) != 0 {
		t.Errorf("SourceCostItemIDs = %v, want empty — the empty aggregate was accepted",
			got.SourceCostItemIDs)
	}

	// The whole point: the next run must NOT report the removal again.
	next := generate(t, svc)
	if next.SourceRemovedCount != 0 {
		t.Errorf("SourceRemovedCount = %d on the run after keep_current, want 0 — "+
			"an accepted removal must not warn forever", next.SourceRemovedCount)
	}
	if next.UnchangedCount != 1 {
		t.Errorf("UnchangedCount = %d, want 1 (clean)", next.UnchangedCount)
	}
}

// --- create_separate (acceptance tests 29, 30, 31c) ---

// The child carries the positive delta only, in the proposed source unit, with
// the discrepancy back-reference and no CostItem aggregate of its own.
func TestCreateSeparateChildCarriesDeltaAndProvenance(t *testing.T) {
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

	child, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionCreateSeparate,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
			ResolutionOperationID:       "op_1",
		})
	if err != nil {
		t.Fatalf("create_separate failed: %v", err)
	}

	if want := decimal.RequireFromString("8"); !child.RequiredQuantity.Value.Equal(want) {
		t.Errorf("child quantity = %s, want the delta 8 (41 − 33)", child.RequiredQuantity.Value.String())
	}
	if child.RequiredQuantity.Unit != "bag" {
		t.Errorf("child unit = %q, want the proposed source unit bag", child.RequiredQuantity.Unit)
	}
	if child.SourceType != mr.SourceTypeManual {
		t.Errorf("child SourceType = %q, want manual", child.SourceType)
	}
	if child.Status != mr.RequirementStatusDraft {
		t.Errorf("child Status = %q, want draft", child.Status)
	}
	if len(child.SourceCostItemIDs) != 0 {
		t.Errorf("child SourceCostItemIDs = %v, want none — it must not claim the same aggregate",
			child.SourceCostItemIDs)
	}
	if child.SourceAggregationKey != "" {
		t.Errorf("child SourceAggregationKey = %q, want empty", child.SourceAggregationKey)
	}
	if child.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("child SourceSyncState = %q, want permanently clean", child.SourceSyncState)
	}

	// Provenance (acceptance test 30).
	if child.CreatedFromDiscrepancyRequirementID == nil ||
		*child.CreatedFromDiscrepancyRequirementID != anchor.ID {
		t.Errorf("child CreatedFromDiscrepancyRequirementID = %v, want %q",
			child.CreatedFromDiscrepancyRequirementID, anchor.ID)
	}
	if child.CreatedFromSourceFingerprint == nil ||
		*child.CreatedFromSourceFingerprint != proposal.ProposedFingerprint {
		t.Errorf("child CreatedFromSourceFingerprint = %v, want the proposed fingerprint",
			child.CreatedFromSourceFingerprint)
	}

	// Inheritance (acceptance test 31c).
	if child.ProjectID != anchor.ProjectID {
		t.Errorf("child ProjectID = %q, want %q inherited — §7.2 filters claims by project",
			child.ProjectID, anchor.ProjectID)
	}
	if !samePtrTest(child.WorkItemID, anchor.WorkItemID) {
		t.Errorf("child WorkItemID = %v, want %v inherited", child.WorkItemID, anchor.WorkItemID)
	}
	if child.MaterialID != anchor.MaterialID || child.MaterialName != anchor.MaterialName ||
		child.CatalogUnit != anchor.CatalogUnit || child.Specification != anchor.Specification {
		t.Errorf("child did not inherit the material identity fields")
	}
	if child.UnitMismatchAcknowledged {
		t.Errorf("child UnitMismatchAcknowledged = true, want false — it never transfers")
	}

	// The source anchor's snapshot was refreshed to proposed and is now clean,
	// and its own quantity was NOT touched.
	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("source anchor SourceSyncState = %q, want clean", after.SourceSyncState)
	}
	if !after.RequiredQuantity.Value.Equal(current.RequiredQuantity.Value) {
		t.Errorf("source anchor quantity moved; create_separate must not touch it")
	}
}

// create_separate is unavailable at a zero delta — a membership-only change
// creates nothing (acceptance test 29).
func TestCreateSeparateUnavailableAtZeroDelta(t *testing.T) {
	// Same total (33), different membership: c1 alone -> c1 + c2 splitting it.
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	ctx := context.Background()
	anchor := generate(t, svc).Created[0]

	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "20", "bag"},
		{"c2", "work_1", "material_1", "13", "bag"},
	}
	got := generate(t, svc)
	if got.DiscrepancyCount != 1 {
		t.Fatalf("DiscrepancyCount = %d, want 1 — membership changed even at an equal total",
			got.DiscrepancyCount)
	}

	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if containsAction(proposal.AvailableActions, mr.ResolutionActionCreateSeparate) {
		t.Errorf("availableActions = %v, must not offer create_separate at a zero delta",
			proposal.AvailableActions)
	}

	createsBefore := repo.createCalls
	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionCreateSeparate,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
			ResolutionOperationID:       "op_zero",
		})
	if !errors.Is(err, mr.ErrResolutionActionNotAvailable) {
		t.Fatalf("error = %v, want ErrResolutionActionNotAvailable", err)
	}
	// Asserted as a Create ATTEMPT count, not a document count: a rejected
	// insert and a correctly-refused action are otherwise indistinguishable.
	if repo.createCalls != createsBefore {
		t.Errorf("Create was attempted %d times, want 0 — no child may be created",
			repo.createCalls-createsBefore)
	}
}

// --- verified idempotency (acceptance tests 31, 31a, 31b) ---

// Retrying one operation ID against the SAME discrepancy yields exactly one
// child, verified field-by-field before reuse.
func TestCreateSeparateRetryReusesVerifiedChild(t *testing.T) {
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
	input := mr.ResolveDiscrepancyInput{
		Action:                      mr.ResolutionActionCreateSeparate,
		ExpectedRevision:            current.Revision,
		ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		ResolutionOperationID:       "op_retry",
	}

	first, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, input)
	if err != nil {
		t.Fatalf("first create_separate failed: %v", err)
	}

	// The retry sees a refreshed anchor, so it carries the new revision. The
	// source has not moved, so the proposed fingerprint still matches.
	refreshed, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	retry := input
	retry.ExpectedRevision = refreshed.Revision

	second, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, retry)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("retry returned child %q, want the existing %q reused", second.ID, first.ID)
	}
	if countResolutionChildren(repo) != 1 {
		t.Errorf("found %d children for one operation ID, want exactly 1",
			countResolutionChildren(repo))
	}
}

// The §5.7 failure window: step 2 created the child but step 3 never ran, so
// the anchor still shows an unresolved discrepancy. The retry must NOT create a
// second child, and must complete the snapshot refresh the crash skipped.
func TestCreateSeparateRetryCompletesAnInterruptedSnapshotRefresh(t *testing.T) {
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
	input := mr.ResolveDiscrepancyInput{
		Action:                      mr.ResolutionActionCreateSeparate,
		ExpectedRevision:            current.Revision,
		ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		ResolutionOperationID:       "op_crash",
	}

	first, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, input)
	if err != nil {
		t.Fatalf("first create_separate failed: %v", err)
	}

	// Simulate the crash: rewind the anchor to its pre-step-3 state, leaving the
	// child in place. This is exactly the orphan §5.7 describes.
	rewound := repo.byID[anchor.ID]
	rewound.SourceSyncState = current.SourceSyncState
	rewound.SourceFingerprint = current.SourceFingerprint
	rewound.SourceQuantity = current.SourceQuantity
	rewound.SourceCostItemIDs = current.SourceCostItemIDs
	rewound.Revision = current.Revision
	repo.byID[anchor.ID] = rewound

	second, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, input)
	if err != nil {
		t.Fatalf("retry after an interrupted refresh failed: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("retry returned child %q, want the existing %q — the unique index forbids a second",
			second.ID, first.ID)
	}
	if countResolutionChildren(repo) != 1 {
		t.Errorf("found %d children, want exactly 1 — a retry must never duplicate demand",
			countResolutionChildren(repo))
	}

	// Step 3 must now have completed.
	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceSyncState != mr.SourceSyncStateClean {
		t.Errorf("SourceSyncState = %q, want clean — the retry must finish the interrupted refresh",
			after.SourceSyncState)
	}
	if after.SourceFingerprint != proposal.ProposedFingerprint {
		t.Errorf("SourceFingerprint was not advanced by the retry")
	}
}

// An operation ID can recover an interrupted write, but it must NOT override
// the rule that a stale proposal is never applied (design spec §5.5, §5.7).
//
// The window: create_separate creates the child, then crashes before the source
// refresh. The contributing CostItems then change. The retry now describes a
// world that no longer exists — refreshing the anchor to the CURRENT aggregate
// would silently accept a source state the contractor never saw and never
// approved, using a delta child computed against a different one.
func TestCreateSeparateRetryRejectsStaleProposalAfterSourceChanged(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	ctx := context.Background()
	anchor := generate(t, svc).Created[0]

	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
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
	input := mr.ResolveDiscrepancyInput{
		Action:                      mr.ResolutionActionCreateSeparate,
		ExpectedRevision:            current.Revision,
		ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		ResolutionOperationID:       "op_stale",
	}

	child, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, input)
	if err != nil {
		t.Fatalf("first create_separate failed: %v", err)
	}

	// Simulate the crash: rewind the anchor to its pre-step-3 state, leaving
	// the child in place (the §5.7 orphan).
	rewound := repo.byID[anchor.ID]
	rewound.SourceSyncState = current.SourceSyncState
	rewound.SourceFingerprint = current.SourceFingerprint
	rewound.SourceQuantity = current.SourceQuantity
	rewound.SourceCostItemIDs = current.SourceCostItemIDs
	rewound.Revision = current.Revision
	repo.byID[anchor.ID] = rewound

	// The source moves AGAIN, after the child was created.
	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
		{"c3", "work_1", "material_1", "5", "bag"},
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, input)
	if !errors.Is(err, mr.ErrMaterialRequirementDiscrepancyChanged) {
		t.Fatalf("error = %v, want ErrMaterialRequirementDiscrepancyChanged — an operation ID "+
			"recovers an interrupted write, it does not license applying a stale proposal", err)
	}

	// Nothing may have been applied.
	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceFingerprint != current.SourceFingerprint {
		t.Errorf("the source snapshot was refreshed to a stale proposal")
	}
	if after.SourceSyncState == mr.SourceSyncStateClean {
		t.Error("the anchor was marked clean against a source the contractor never approved")
	}
	if after.Revision != current.Revision {
		t.Errorf("Revision = %d, want %d — a rejected retry must write nothing",
			after.Revision, current.Revision)
	}

	// The existing draft child stays visible for reconciliation, and no second
	// child is created.
	if countResolutionChildren(repo) != 1 {
		t.Errorf("found %d children, want exactly 1 — the orphan stays, nothing is added",
			countResolutionChildren(repo))
	}
	stillThere, err := repo.FindByID(ctx, "company_a", child.ID)
	if err != nil {
		t.Fatalf("the existing draft child must remain visible for reconciliation: %v", err)
	}
	if stillThere.Status != mr.RequirementStatusDraft {
		t.Errorf("orphan child Status = %q, want draft (therefore not RFQ-eligible)",
			stillThere.Status)
	}
}

// The retry guard must hold even when the caller's expectation agrees with the
// LIVE source — the case the top-level staleness check cannot catch.
//
// Here the child was created against fingerprint A; the CostItems then moved to
// B; the caller correctly passes B. The top-level guard (live == request) is
// satisfied, so only the child-fingerprint comparison inside the retry path can
// reject this. Without it, the anchor would be refreshed to B on the strength
// of a delta child computed against A.
func TestCreateSeparateRetryRejectsWhenOnlyTheChildFingerprintDisagrees(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	ctx := context.Background()
	anchor := generate(t, svc).Created[0]

	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
	}
	generate(t, svc)

	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposalA, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionCreateSeparate,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposalA.ProposedFingerprint,
			ResolutionOperationID:       "op_child_fp",
		}); err != nil {
		t.Fatalf("first create_separate failed: %v", err)
	}

	// Crash before step 3: rewind the anchor, leave the child.
	rewound := repo.byID[anchor.ID]
	rewound.SourceSyncState = current.SourceSyncState
	rewound.SourceFingerprint = current.SourceFingerprint
	rewound.SourceQuantity = current.SourceQuantity
	rewound.SourceCostItemIDs = current.SourceCostItemIDs
	rewound.Revision = current.Revision
	repo.byID[anchor.ID] = rewound

	// The source moves to B, and the caller correctly observes B.
	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
		{"c3", "work_1", "material_1", "6", "bag"},
	}
	proposalB, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposalB.ProposedFingerprint == proposalA.ProposedFingerprint {
		t.Fatal("setup: the source did not actually move")
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action: mr.ResolutionActionCreateSeparate,
			// Deliberately the CURRENT revision and the CURRENT fingerprint:
			// the top-level guard passes, so only the retry path can reject.
			ExpectedRevision:            repo.byID[anchor.ID].Revision,
			ExpectedProposedFingerprint: proposalB.ProposedFingerprint,
			ResolutionOperationID:       "op_child_fp",
		})
	if !errors.Is(err, mr.ErrMaterialRequirementDiscrepancyChanged) {
		t.Fatalf("error = %v, want ErrMaterialRequirementDiscrepancyChanged — the child's "+
			"recorded fingerprint no longer matches the source it would be applied against", err)
	}

	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.SourceFingerprint == proposalB.ProposedFingerprint {
		t.Error("the anchor was refreshed to fingerprint B using a child computed against A")
	}
	if after.SourceSyncState == mr.SourceSyncStateClean {
		t.Error("the anchor was marked clean against a proposal the contractor never approved")
	}
	if countResolutionChildren(repo) != 1 {
		t.Errorf("found %d children, want exactly 1", countResolutionChildren(repo))
	}
}

// The mirror case: the source is unchanged, but the CALLER supplies a
// fingerprint that does not match the child's. The request describes a
// different proposal than the one the child was created against.
func TestCreateSeparateRetryRejectsCallerFingerprintMismatch(t *testing.T) {
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
	input := mr.ResolveDiscrepancyInput{
		Action:                      mr.ResolutionActionCreateSeparate,
		ExpectedRevision:            current.Revision,
		ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		ResolutionOperationID:       "op_mismatch",
	}
	if _, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, input); err != nil {
		t.Fatalf("first create_separate failed: %v", err)
	}

	refreshed, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	retry := input
	retry.ExpectedRevision = refreshed.Revision
	retry.ExpectedProposedFingerprint = "some-other-fingerprint"

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID, retry)
	if !errors.Is(err, mr.ErrMaterialRequirementDiscrepancyChanged) {
		t.Fatalf("error = %v, want ErrMaterialRequirementDiscrepancyChanged", err)
	}
	if countResolutionChildren(repo) != 1 {
		t.Errorf("found %d children, want exactly 1", countResolutionChildren(repo))
	}
}

// Reusing one operation ID against a DIFFERENT source requirement is a
// conflict. Blind reuse would hand back another discrepancy's child and leave
// this anchor silently unresolved (acceptance test 31a).
func TestCreateSeparateRejectsOperationIDReusedAcrossRequirements(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"d1", "work_2", "material_1", "50", "bag"},
	})
	ctx := context.Background()

	first := generate(t, svc)
	if first.CreatedCount != 2 {
		t.Fatalf("setup: CreatedCount = %d, want 2", first.CreatedCount)
	}
	var anchorA, anchorB mr.MaterialRequirement
	for _, c := range first.Created {
		if *c.WorkItemID == "work_1" {
			anchorA = c
		} else {
			anchorB = c
		}
	}

	// Both anchors gain a positive discrepancy.
	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
		{"d1", "work_2", "material_1", "50", "bag"},
		{"d2", "work_2", "material_1", "5", "bag"},
	}
	generate(t, svc)

	resolve := func(anchorID string) (mr.MaterialRequirement, error) {
		t.Helper()
		cur, err := repo.FindByID(ctx, "company_a", anchorID)
		if err != nil {
			t.Fatal(err)
		}
		p, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchorID)
		if err != nil {
			t.Fatal(err)
		}
		return svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchorID,
			mr.ResolveDiscrepancyInput{
				Action:                      mr.ResolutionActionCreateSeparate,
				ExpectedRevision:            cur.Revision,
				ExpectedProposedFingerprint: p.ProposedFingerprint,
				ResolutionOperationID:       "op_shared",
			})
	}

	if _, err := resolve(anchorA.ID); err != nil {
		t.Fatalf("first resolution failed: %v", err)
	}

	beforeB, err := repo.FindByID(ctx, "company_a", anchorB.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolve(anchorB.ID)
	if !errors.Is(err, mr.ErrResolutionOperationConflict) {
		t.Fatalf("error = %v, want ErrResolutionOperationConflict — an operation ID identifies "+
			"one resolution of one specific discrepancy", err)
	}

	// Neither requirement may be modified (acceptance test 31a).
	afterB, err := repo.FindByID(ctx, "company_a", anchorB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterB.SourceSyncState != beforeB.SourceSyncState || afterB.Revision != beforeB.Revision {
		t.Errorf("anchor B was modified by a conflicting resolution: state %q->%q, revision %d->%d",
			beforeB.SourceSyncState, afterB.SourceSyncState, beforeB.Revision, afterB.Revision)
	}
	if countResolutionChildren(repo) != 1 {
		t.Errorf("found %d children, want exactly 1 — the conflict must create nothing",
			countResolutionChildren(repo))
	}
}

// Reusing one operation ID against the SAME requirement but a different delta
// is also a conflict (acceptance test 31b).
func TestCreateSeparateRejectsOperationIDReusedAtDifferentDelta(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	ctx := context.Background()
	anchor := generate(t, svc).Created[0]

	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
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
	if _, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionCreateSeparate,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
			ResolutionOperationID:       "op_delta",
		}); err != nil {
		t.Fatalf("first resolution failed: %v", err)
	}

	// The source moves again, so the same operation ID now denotes a DIFFERENT
	// delta against the same anchor.
	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
		{"c2", "work_1", "material_1", "8", "bag"},
		{"c3", "work_1", "material_1", "4", "bag"},
	}
	generate(t, svc)

	current2, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	proposal2, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionCreateSeparate,
			ExpectedRevision:            current2.Revision,
			ExpectedProposedFingerprint: proposal2.ProposedFingerprint,
			ResolutionOperationID:       "op_delta",
		})
	// This is a STALENESS failure, not an identity conflict. The operation ID
	// genuinely names this resolution of this discrepancy — but the CostItems
	// moved after the child was created, so the delta the child records no
	// longer describes the source. The two sentinels are deliberately distinct:
	// Conflict means "a different operation", DiscrepancyChanged means "this
	// operation, against a world that moved" (design spec §5.5, §5.7).
	if !errors.Is(err, mr.ErrMaterialRequirementDiscrepancyChanged) {
		t.Fatalf("error = %v, want ErrMaterialRequirementDiscrepancyChanged — the source moved "+
			"after the child was created", err)
	}
	if countResolutionChildren(repo) != 1 {
		t.Errorf("found %d children, want exactly 1", countResolutionChildren(repo))
	}

	// Nothing was applied to the anchor.
	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != current2.Revision {
		t.Errorf("Revision = %d, want %d — a rejected retry writes nothing",
			after.Revision, current2.Revision)
	}
}

// The anchor-identity term of §5.7's verification, isolated.
//
// TestCreateSeparateRejectsOperationIDReusedAcrossRequirements exercises the
// realistic path, but two anchors with different sources also differ in
// fingerprint, so the FINGERPRINT check rejects them first and the
// "belongs to THIS requirement" check is never reached. Mutation testing
// exposed that: deleting the anchor-identity check left every test green.
//
// This test seeds a child whose fingerprint matches the anchor's live proposal
// but whose CreatedFromDiscrepancyRequirementID names a different requirement —
// the one arrangement in which only the anchor-identity check can fire.
func TestCreateSeparateRejectsChildBelongingToAnotherRequirement(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()
	generate(t, svc)

	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A child carrying THIS anchor's proposed fingerprint and delta, but
	// recorded against a different source requirement.
	opID := "op_foreign"
	foreignSource := "mr_some_other_requirement"
	fingerprint := proposal.ProposedFingerprint
	if _, err := repo.Create(ctx, mr.MaterialRequirement{
		CompanyID: "company_a", ProjectID: anchor.ProjectID,
		WorkItemID: anchor.WorkItemID, MaterialID: anchor.MaterialID,
		RequiredQuantity: mustQty(t, "8", "bag"),
		SourceType:       mr.SourceTypeManual,
		SourceSyncState:  mr.SourceSyncStateClean,
		Status:           mr.RequirementStatusDraft,

		CreatedFromDiscrepancyRequirementID: &foreignSource,
		CreatedFromSourceFingerprint:        &fingerprint,
		ResolutionOperationID:               &opID,
	}); err != nil {
		t.Fatal(err)
	}

	_, err = svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
		mr.ResolveDiscrepancyInput{
			Action:                      mr.ResolutionActionCreateSeparate,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
			ResolutionOperationID:       opID,
		})
	if !errors.Is(err, mr.ErrResolutionOperationConflict) {
		t.Fatalf("error = %v, want ErrResolutionOperationConflict — an operation ID must identify "+
			"one resolution of ONE specific requirement, not merely a matching fingerprint", err)
	}

	// The anchor must be untouched: no snapshot refresh may ride on a
	// conflicting operation (acceptance test 31a).
	after, err := repo.FindByID(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != current.Revision || after.SourceSyncState != current.SourceSyncState {
		t.Errorf("anchor was modified by a conflicting resolution: revision %d->%d, state %q->%q",
			current.Revision, after.Revision, current.SourceSyncState, after.SourceSyncState)
	}
}

// create_separate without an operation ID is refused: without it a retry would
// create duplicate procurement demand (design spec §5.7).
func TestCreateSeparateRequiresOperationID(t *testing.T) {
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
			Action:                      mr.ResolutionActionCreateSeparate,
			ExpectedRevision:            current.Revision,
			ExpectedProposedFingerprint: proposal.ProposedFingerprint,
		})
	if !errors.Is(err, mr.ErrResolutionOperationIDRequired) {
		t.Fatalf("error = %v, want ErrResolutionOperationIDRequired", err)
	}
}

// --- anchor availability matrix (acceptance tests 34, 35, 36) ---

// A terminal anchor offers keep_current, never update or merge — both mutate
// RequiredQuantity (design spec §5.6).
func TestTerminalAnchorWithholdsUpdateAndMerge(t *testing.T) {
	for _, status := range []mr.RequirementStatus{
		mr.RequirementStatusSplit, mr.RequirementStatusArchived,
	} {
		t.Run(string(status), func(t *testing.T) {
			svc, repo, anchor := seedDiscrepancy(t, "33", "41")
			ctx := context.Background()

			// Drive it terminal directly, as a split or archive would.
			stored := repo.byID[anchor.ID]
			stored.Status = status
			repo.byID[anchor.ID] = stored

			generate(t, svc)
			current, err := repo.FindByID(ctx, "company_a", anchor.ID)
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
			if err != nil {
				t.Fatal(err)
			}

			if containsAction(proposal.AvailableActions, mr.ResolutionActionUpdate) {
				t.Errorf("availableActions = %v, must not offer update on a %s anchor",
					proposal.AvailableActions, status)
			}
			if containsAction(proposal.AvailableActions, mr.ResolutionActionMerge) {
				t.Errorf("availableActions = %v, must not offer merge on a %s anchor",
					proposal.AvailableActions, status)
			}
			if !containsAction(proposal.AvailableActions, mr.ResolutionActionKeepCurrent) {
				t.Errorf("availableActions = %v, must offer keep_current on a %s anchor",
					proposal.AvailableActions, status)
			}

			for _, blocked := range []mr.ResolutionAction{
				mr.ResolutionActionUpdate, mr.ResolutionActionMerge,
			} {
				_, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
					mr.ResolveDiscrepancyInput{
						Action:                      blocked,
						ExpectedRevision:            current.Revision,
						ExpectedProposedFingerprint: proposal.ProposedFingerprint,
					})
				if !errors.Is(err, mr.ErrResolutionActionNotAvailable) {
					t.Errorf("%s on a %s anchor: error = %v, want ErrResolutionActionNotAvailable",
						blocked, status, err)
				}
			}

			// keep_current still succeeds and converges the anchor without
			// reviving it.
			got, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
				mr.ResolveDiscrepancyInput{
					Action:                      mr.ResolutionActionKeepCurrent,
					ExpectedRevision:            current.Revision,
					ExpectedProposedFingerprint: proposal.ProposedFingerprint,
				})
			if err != nil {
				t.Fatalf("keep_current on a %s anchor failed: %v", status, err)
			}
			if got.Status != status {
				t.Errorf("Status = %q, want %q — keep_current must not revive a terminal anchor",
					got.Status, status)
			}
			if got.SourceSyncState != mr.SourceSyncStateClean {
				t.Errorf("SourceSyncState = %q, want clean", got.SourceSyncState)
			}
			if !got.RequiredQuantity.Value.Equal(current.RequiredQuantity.Value) {
				t.Errorf("RequiredQuantity moved on a terminal anchor")
			}
		})
	}
}

// A NEGATIVE source change against a split anchor offers keep_current ONLY:
// create_separate needs a positive delta, and M7 never mutates split children
// to absorb a reduction (acceptance test 36).
func TestNegativeChangeOnSplitAnchorOffersKeepCurrentOnly(t *testing.T) {
	svc, repo, source, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	ctx := context.Background()
	anchor := generate(t, svc).Created[0]

	stored := repo.byID[anchor.ID]
	stored.Status = mr.RequirementStatusSplit
	repo.byID[anchor.ID] = stored

	source.rows["company_a|project_1"] = []costRow{
		{"c1", "work_1", "material_1", "10", "bag"},
	}
	generate(t, svc)

	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.AvailableActions) != 1 ||
		proposal.AvailableActions[0] != mr.ResolutionActionKeepCurrent {
		t.Errorf("availableActions = %v, want [keep_current] only", proposal.AvailableActions)
	}
}

// A clean anchor has nothing to resolve, so no action is offered.
func TestCleanAnchorOffersNoActions(t *testing.T) {
	svc, _, _, _ := newGenService(t, []costRow{
		{"c1", "work_1", "material_1", "33", "bag"},
	})
	anchor := generate(t, svc).Created[0]

	proposal, err := svc.GetSourceDiscrepancy(context.Background(), "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposal.AvailableActions) != 0 {
		t.Errorf("availableActions = %v, want none on a clean anchor", proposal.AvailableActions)
	}
}

// Every resolution action is rejected while the requirement is claimed
// (acceptance test 37).
func TestEveryResolutionActionRejectedWhileClaimed(t *testing.T) {
	svc, repo, anchor := seedDiscrepancy(t, "33", "41")
	ctx := context.Background()
	generate(t, svc)

	proposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}

	claimed := repo.byID[anchor.ID]
	chainID := "rfq_chain_1"
	claimed.ActiveRFQChainID = &chainID
	repo.byID[anchor.ID] = claimed

	for _, action := range []mr.ResolutionAction{
		mr.ResolutionActionUpdate, mr.ResolutionActionMerge,
		mr.ResolutionActionKeepCurrent, mr.ResolutionActionCreateSeparate,
	} {
		_, err := svc.ResolveSourceDiscrepancy(ctx, "company_a", "user_1", anchor.ID,
			mr.ResolveDiscrepancyInput{
				Action:                      action,
				ExpectedRevision:            claimed.Revision,
				ExpectedProposedFingerprint: proposal.ProposedFingerprint,
				ResolutionOperationID:       "op_claimed",
			})
		if !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
			t.Errorf("%s while claimed: error = %v, want ErrMaterialRequirementAlreadyClaimed",
				action, err)
		}
	}

	// A claimed anchor offers no actions at all (design spec §2.3).
	claimedProposal, err := svc.GetSourceDiscrepancy(ctx, "company_a", anchor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimedProposal.AvailableActions) != 0 {
		t.Errorf("availableActions = %v, want none while claimed", claimedProposal.AvailableActions)
	}
}

// A manual requirement has no CostItem aggregate at all, so there is nothing to
// resolve (design spec §8.5).
func TestManualRequirementHasNoSourceDiscrepancy(t *testing.T) {
	svc, _, _ := newService(t)
	created := createManual(t, svc, nil)

	_, err := svc.GetSourceDiscrepancy(context.Background(), "company_a", created.ID)
	if !errors.Is(err, mr.ErrNoSourceDiscrepancy) {
		t.Fatalf("error = %v, want ErrNoSourceDiscrepancy", err)
	}
}

// --- helpers ---

func countResolutionChildren(repo *fakeRequirementRepo) int {
	n := 0
	for _, r := range repo.byID {
		if r.ResolutionOperationID != nil {
			n++
		}
	}
	return n
}

func samePtrTest(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
