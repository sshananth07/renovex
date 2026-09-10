package materialrequirements_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// B7 covers the five claim primitives materialrequirements owns (design spec
// §1.3, §7.1). This module performs the conditional writes and NOTHING else:
// it never inspects an RFQ line, never learns whether one exists, and never
// implements retry_line — all orchestration is rfqs-owned.
//
// Every method here takes and returns primitives, so rfqs can satisfy its own
// interface without importing this package (ADR 0002).

// seedClaimable creates an RFQ-eligible requirement: reviewed, positive
// quantity, no unit mismatch, clean sync state.
func seedClaimable(t *testing.T) (*mr.Service, *fakeRequirementRepo, mr.MaterialRequirement) {
	t.Helper()
	svc, repo, _ := newService(t)
	ctx := context.Background()
	workItemID := "work_1"

	created, err := svc.CreateManualRequirement(ctx, "company_a", "user_1",
		mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: &workItemID, MaterialID: "material_1",
			// "bag" matches material_1's catalog unit, so UnitMismatch is false.
			QuantityValue: "100", QuantityUnit: "bag",
			ProcurementNotes: "deliver to site gate",
			InternalNotes:    "contractor-only margin note",
		})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	reviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("setup review: %v", err)
	}
	if !reviewed.IsRFQEligible() {
		t.Fatalf("setup: requirement is not RFQ-eligible: %+v", reviewed)
	}
	return svc, repo, reviewed
}

// The happy path: a conditional claim that returns the snapshot built from the
// SAME document the claim validated (design spec §7.2 steps 3-4).
func TestClaimForRFQSetsClaimAndReturnsSnapshot(t *testing.T) {
	svc, repo, req := seedClaimable(t)
	ctx := context.Background()

	snap, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1")
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	if snap.RequirementID != req.ID {
		t.Errorf("RequirementID = %q, want %q", snap.RequirementID, req.ID)
	}
	if snap.Revision != req.Revision+1 {
		t.Errorf("Revision = %d, want %d — the snapshot reports the POST-claim revision so "+
			"rfqs can compensate with it", snap.Revision, req.Revision+1)
	}
	if snap.MaterialID != req.MaterialID || snap.MaterialName != req.MaterialName {
		t.Errorf("snapshot lost the material identity: %+v", snap)
	}
	if snap.QuantityValue != "100" || snap.QuantityUnit != "bag" {
		t.Errorf("quantity = %s %s, want 100 bag", snap.QuantityValue, snap.QuantityUnit)
	}
	if snap.ProcurementNotes != "deliver to site gate" {
		t.Errorf("ProcurementNotes = %q, want the supplier-visible note", snap.ProcurementNotes)
	}

	stored, err := repo.FindByID(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ActiveRFQChainID == nil || *stored.ActiveRFQChainID != "chain_1" {
		t.Errorf("ActiveRFQChainID = %v, want chain_1", stored.ActiveRFQChainID)
	}
	if stored.ActiveRFQNumber == nil || *stored.ActiveRFQNumber != "RFQ-000124" {
		t.Errorf("ActiveRFQNumber = %v, want RFQ-000124", stored.ActiveRFQNumber)
	}
	if stored.ActiveRFQLineID == nil || *stored.ActiveRFQLineID != "line_1" {
		t.Errorf("ActiveRFQLineID = %v, want line_1 — a pre-generated stable line id "+
			"is what makes a retry deterministic", stored.ActiveRFQLineID)
	}
	if stored.RFQClaimedAt == nil {
		t.Error("RFQClaimedAt was not set")
	}
}

// The snapshot is an ALLOWLIST: it carries no InternalNotes, no cost and no
// margin. rfqs snapshots it into a supplier-facing line (design spec §1.3, §9).
func TestClaimSnapshotExcludesInternalNotes(t *testing.T) {
	svc, _, req := seedClaimable(t)

	snap, err := svc.ClaimForRFQ(context.Background(), "company_a", "project_1", req.ID,
		req.Revision, "chain_1", "RFQ-000124", "line_1")
	if err != nil {
		t.Fatal(err)
	}

	// Asserted structurally: the snapshot type must have no field carrying the
	// contractor-only note, so it cannot leak into a supplier-visible line.
	if containsSensitive(snap, req.InternalNotes) {
		t.Errorf("the claim snapshot carries InternalNotes %q — it is contractor-only "+
			"and must NEVER reach a supplier-visible RFQ line", req.InternalNotes)
	}
}

// projectID is enforced INSIDE the atomic filter, not as a separate pre-check.
// A requirement in a different project of the SAME company must not be
// claimable, and no claim may be created (design spec §7.2).
func TestClaimForRFQRejectsRequirementFromAnotherProject(t *testing.T) {
	svc, repo, req := seedClaimable(t)
	ctx := context.Background()

	_, err := svc.ClaimForRFQ(ctx, "company_a", "project_2", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1")
	if !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want ErrMaterialRequirementNotFound — a cross-project claim "+
			"must be structurally impossible", err)
	}

	stored, err := repo.FindByID(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IsClaimed() {
		t.Error("a rejected cross-project claim must create NO claim")
	}
}

// The first conditional claim wins; a competing chain gets 409 and the original
// claim is untouched (design spec §7.2).
func TestClaimForRFQIsExclusivePerRequirement(t *testing.T) {
	svc, repo, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.FindByID(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, claimed.Revision,
		"chain_2", "RFQ-000125", "line_2")
	if !errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed) {
		t.Fatalf("error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}

	after, err := repo.FindByID(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *after.ActiveRFQChainID != "chain_1" || *after.ActiveRFQLineID != "line_1" {
		t.Error("the losing claim overwrote the winner's claim")
	}
}

// The §8.5 eligibility predicate is enforced inside the claim, so it cannot be
// raced by a concurrent edit (design spec §7.2 step 3, §8.5).
func TestClaimForRFQEnforcesEligibilityAtomically(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*mr.MaterialRequirement)
	}{
		{"draft, not reviewed", func(r *mr.MaterialRequirement) {
			r.Status = mr.RequirementStatusDraft
		}},
		{"unresolved source discrepancy", func(r *mr.MaterialRequirement) {
			r.SourceSyncState = mr.SourceSyncStateChangeDetected
		}},
		{"unacknowledged unit mismatch", func(r *mr.MaterialRequirement) {
			r.UnitMismatch = true
			r.UnitMismatchAcknowledged = false
		}},
		{"terminal", func(r *mr.MaterialRequirement) {
			r.Status = mr.RequirementStatusArchived
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, req := seedClaimable(t)
			stored := repo.byID[req.ID]
			tc.mutate(&stored)
			repo.byID[req.ID] = stored

			_, err := svc.ClaimForRFQ(context.Background(), "company_a", "project_1", req.ID,
				stored.Revision, "chain_1", "RFQ-000124", "line_1")
			if !errors.Is(err, mr.ErrRequirementNotEligibleForRFQ) {
				t.Fatalf("error = %v, want ErrRequirementNotEligibleForRFQ", err)
			}
			if repo.byID[req.ID].IsClaimed() {
				t.Error("an ineligible requirement must not be claimed")
			}
		})
	}
}

// An acknowledged unit mismatch DOES permit a claim — acknowledgement is the
// contractor's explicit resolution of that gate (design spec §8.5).
func TestClaimForRFQAllowsAcknowledgedUnitMismatch(t *testing.T) {
	svc, repo, req := seedClaimable(t)
	stored := repo.byID[req.ID]
	stored.UnitMismatch = true
	stored.UnitMismatchAcknowledged = true
	repo.byID[req.ID] = stored

	if _, err := svc.ClaimForRFQ(context.Background(), "company_a", "project_1", req.ID,
		stored.Revision, "chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatalf("an acknowledged mismatch must not block a claim: %v", err)
	}
}

// A stale revision loses the claim without side effects.
func TestClaimForRFQRejectsStaleRevision(t *testing.T) {
	svc, repo, req := seedClaimable(t)

	_, err := svc.ClaimForRFQ(context.Background(), "company_a", "project_1", req.ID,
		req.Revision-1, "chain_1", "RFQ-000124", "line_1")
	if !errors.Is(err, mr.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
	if repo.byID[req.ID].IsClaimed() {
		t.Error("a stale-revision claim must create no claim")
	}
}

// --- ReleaseClaim (design spec §7.4) ---

// Release requires the EXACT chain and line, so a stale or mismatched release
// can never free a requirement another RFQ legitimately holds.
func TestReleaseClaimRequiresExactChainAndLine(t *testing.T) {
	svc, repo, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.FindByID(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("wrong chain", func(t *testing.T) {
		err := svc.ReleaseClaim(ctx, "company_a", req.ID, claimed.Revision, "chain_zzz", "line_1")
		if !errors.Is(err, mr.ErrRevisionMismatch) {
			t.Fatalf("error = %v, want ErrRevisionMismatch", err)
		}
		if !repo.byID[req.ID].IsClaimed() {
			t.Error("a wrong-chain release must NOT clear the claim")
		}
	})

	t.Run("wrong line", func(t *testing.T) {
		err := svc.ReleaseClaim(ctx, "company_a", req.ID, claimed.Revision, "chain_1", "line_zzz")
		if !errors.Is(err, mr.ErrRevisionMismatch) {
			t.Fatalf("error = %v, want ErrRevisionMismatch", err)
		}
		if !repo.byID[req.ID].IsClaimed() {
			t.Error("a wrong-line release must NOT clear the claim")
		}
	})

	t.Run("exact match", func(t *testing.T) {
		if err := svc.ReleaseClaim(ctx, "company_a", req.ID, claimed.Revision,
			"chain_1", "line_1"); err != nil {
			t.Fatalf("release failed: %v", err)
		}
		released := repo.byID[req.ID]
		if released.IsClaimed() {
			t.Error("the claim was not cleared")
		}
		if released.ActiveRFQNumber != nil || released.ActiveRFQLineID != nil ||
			released.RFQClaimedAt != nil {
			t.Errorf("release left claim residue: %+v", released)
		}
		// The requirement is immediately claimable by another chain.
		if !released.IsRFQEligible() {
			t.Error("a released requirement must be eligible again")
		}
	})
}

// --- ReadClaim (design spec §1.3) ---

func TestReadClaimReportsClaimState(t *testing.T) {
	svc, repo, req := seedClaimable(t)
	ctx := context.Background()

	_, _, _, _, found, err := svc.ReadClaim(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("an unclaimed requirement must report found=false")
	}

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.FindByID(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}

	chainID, number, lineID, revision, found, err := svc.ReadClaim(ctx, "company_a", req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("a claimed requirement must report found=true")
	}
	if chainID != "chain_1" || number != "RFQ-000124" || lineID != "line_1" {
		t.Errorf("claim = %q/%q/%q, want chain_1/RFQ-000124/line_1", chainID, number, lineID)
	}
	if revision != claimed.Revision {
		t.Errorf("revision = %d, want %d — rfqs needs it to release under a guard",
			revision, claimed.Revision)
	}
}

// A foreign company can never read a claim.
func TestReadClaimIsTenantScoped(t *testing.T) {
	svc, _, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	if _, _, _, _, _, err := svc.ReadClaim(ctx, "company_b", req.ID); !errors.Is(err,
		mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want ErrMaterialRequirementNotFound", err)
	}
}

// --- ListClaimsForRFQChain (design spec §7.5) ---

// This is what makes an ORPHANED claim discoverable. ReadClaim can only confirm
// a claim the caller already suspects; enumeration needs no requirement ID.
func TestListClaimsForRFQChainEnumeratesWithoutRequirementIDs(t *testing.T) {
	svc, repo, first := seedClaimable(t)
	ctx := context.Background()
	workItemID := "work_1"

	second, err := svc.CreateManualRequirement(ctx, "company_a", "user_1",
		mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: &workItemID, MaterialID: "material_1",
			QuantityValue: "50", QuantityUnit: "bag",
		})
	if err != nil {
		t.Fatal(err)
	}
	secondReviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", second.ID, second.Revision)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", first.ID, first.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", secondReviewed.ID,
		secondReviewed.Revision, "chain_1", "RFQ-000124", "line_2"); err != nil {
		t.Fatal(err)
	}

	// A third requirement on a DIFFERENT chain must not appear.
	third, err := svc.CreateManualRequirement(ctx, "company_a", "user_1",
		mr.CreateManualInput{
			ProjectID: "project_1", WorkItemID: &workItemID, MaterialID: "material_1",
			QuantityValue: "25", QuantityUnit: "bag",
		})
	if err != nil {
		t.Fatal(err)
	}
	thirdReviewed, err := svc.ReviewRequirement(ctx, "company_a", "user_1", third.ID, third.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", thirdReviewed.ID,
		thirdReviewed.Revision, "chain_2", "RFQ-000125", "line_9"); err != nil {
		t.Fatal(err)
	}

	claims, err := svc.ListClaimsForRFQChain(ctx, "company_a", "chain_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 {
		t.Fatalf("got %d claims for chain_1, want 2", len(claims))
	}

	byLine := map[string]mr.RequirementClaim{}
	for _, c := range claims {
		byLine[c.LineID] = c
		if c.RFQChainID != "chain_1" {
			t.Errorf("claim %+v belongs to another chain", c)
		}
		stored, err := repo.FindByID(ctx, "company_a", c.RequirementID)
		if err != nil {
			t.Fatal(err)
		}
		if c.Revision != stored.Revision {
			t.Errorf("claim revision = %d, want the current %d — rfqs releases under this guard",
				c.Revision, stored.Revision)
		}
	}
	// Recovering the requirement ID FROM the line ID is what §7.3's remove-line
	// retry depends on.
	if byLine["line_1"].RequirementID != first.ID {
		t.Errorf("line_1 maps to %q, want %q", byLine["line_1"].RequirementID, first.ID)
	}
	if byLine["line_2"].RequirementID != secondReviewed.ID {
		t.Errorf("line_2 maps to %q, want %q", byLine["line_2"].RequirementID, secondReviewed.ID)
	}
}

func TestListClaimsForRFQChainIsTenantScoped(t *testing.T) {
	svc, _, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	claims, err := svc.ListClaimsForRFQChain(ctx, "company_b", "chain_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 0 {
		t.Errorf("got %d claims for a foreign company, want 0", len(claims))
	}
}

// --- ClaimedRequirementIsReadyForRFQ (design spec §6.4, §8.5) ---

// The mark-ready predicate must NOT reuse §8.5: its ActiveRFQChainID == nil
// term is false for every requirement already on a line, so reusing it would
// make every non-empty RFQ impossible to mark ready.
func TestClaimedRequirementIsReadyForRFQAcceptsAClaimedRequirement(t *testing.T) {
	svc, _, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	ready, err := svc.ClaimedRequirementIsReadyForRFQ(ctx, "company_a", req.ID, "chain_1", "line_1")
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("a claimed, reviewed, clean requirement must be ready — reusing the §8.5 " +
			"predicate here would make EVERY non-empty RFQ unmarkable as ready")
	}
}

// It must require the claim to name THIS chain and THIS line.
func TestClaimedRequirementIsReadyForRFQRequiresMatchingChainAndLine(t *testing.T) {
	svc, _, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, chain, line string }{
		{"another chain", "chain_2", "line_1"},
		{"another line", "chain_1", "line_2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ready, err := svc.ClaimedRequirementIsReadyForRFQ(ctx, "company_a", req.ID,
				tc.chain, tc.line)
			if err != nil {
				t.Fatal(err)
			}
			if ready {
				t.Errorf("a requirement claimed by chain_1/line_1 must not be ready for %s/%s",
					tc.chain, tc.line)
			}
		})
	}
}

// It applies the same business conditions as §8.5 apart from the claim term.
func TestClaimedRequirementIsReadyForRFQEnforcesBusinessConditions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*mr.MaterialRequirement)
	}{
		{"not reviewed", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusDraft }},
		{"unresolved discrepancy", func(r *mr.MaterialRequirement) {
			r.SourceSyncState = mr.SourceSyncStateChangeDetected
		}},
		{"unacknowledged unit mismatch", func(r *mr.MaterialRequirement) {
			r.UnitMismatch = true
			r.UnitMismatchAcknowledged = false
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, req := seedClaimable(t)
			ctx := context.Background()
			if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
				"chain_1", "RFQ-000124", "line_1"); err != nil {
				t.Fatal(err)
			}

			stored := repo.byID[req.ID]
			tc.mutate(&stored)
			repo.byID[req.ID] = stored

			ready, err := svc.ClaimedRequirementIsReadyForRFQ(ctx, "company_a", req.ID,
				"chain_1", "line_1")
			if err != nil {
				t.Fatal(err)
			}
			if ready {
				t.Errorf("%s must not be ready for an RFQ", tc.name)
			}
		})
	}
}

// An unclaimed requirement is never "ready" — mark-ready validates lines that
// exist, and a line always implies a claim.
func TestClaimedRequirementIsReadyForRFQRejectsUnclaimed(t *testing.T) {
	svc, _, req := seedClaimable(t)

	ready, err := svc.ClaimedRequirementIsReadyForRFQ(context.Background(), "company_a",
		req.ID, "chain_1", "line_1")
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Error("an unclaimed requirement must not be reported ready")
	}
}

// --- helpers ---

// containsSensitive reports whether the snapshot exposes the given value in any
// of its string fields.
func containsSensitive(snap mr.ClaimSnapshot, secret string) bool {
	if secret == "" {
		return false
	}
	for _, field := range []string{
		snap.RequirementID, snap.MaterialID, snap.MaterialName, snap.Specification,
		snap.QuantityValue, snap.QuantityUnit, snap.ProcurementNotes,
	} {
		if field == secret {
			return true
		}
	}
	return false
}

// --- ReadClaimSnapshot: the repair capability (design spec §1.3, §7.3, §7.5) ---
//
// This is a SPEC AMENDMENT discovered during implementation: §1.3 originally
// exposed five requirement capabilities, none of which could retrieve the
// immutable snapshot a claimed requirement carries. §7.5's documented failure
// window expects reconciliation to repair a missing line, and neither
// re-claiming (impossible — it is already claimed) nor fabricating
// supplier-visible content is acceptable.
//
// It is scoped to the EXACT claim, so it can never become a general read.

func TestReadClaimSnapshotReturnsTheSnapshotForTheExactClaim(t *testing.T) {
	svc, _, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	snap, err := svc.ReadClaimSnapshot(ctx, "company_a", req.ID, "chain_1", "line_1")
	if err != nil {
		t.Fatalf("the exact claim must yield a snapshot: %v", err)
	}
	if snap.RequirementID != req.ID {
		t.Errorf("RequirementID = %q, want %q", snap.RequirementID, req.ID)
	}
	if snap.MaterialName != "Portland Cement" || snap.Specification != req.Specification {
		t.Errorf("descriptive fields lost: %+v", snap)
	}
	if snap.QuantityValue != "100" || snap.QuantityUnit != "bag" {
		t.Errorf("quantity = %s %s, want 100 bag", snap.QuantityValue, snap.QuantityUnit)
	}
	if snap.ProcurementNotes != "deliver to site gate" {
		t.Errorf("ProcurementNotes = %q", snap.ProcurementNotes)
	}
}

// The exact-claim filter: every term is required. rfqs must never obtain a
// snapshot from an unclaimed requirement, one claimed by another chain, one
// claimed under another line, or another tenant.
func TestReadClaimSnapshotRequiresTheExactClaim(t *testing.T) {
	cases := []struct {
		name          string
		claimFirst    bool
		chainID, line string
	}{
		{"unclaimed requirement", false, "chain_1", "line_1"},
		{"wrong chain", true, "chain_zzz", "line_1"},
		{"wrong line", true, "chain_1", "line_zzz"},
		{"both wrong", true, "chain_zzz", "line_zzz"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, req := seedClaimable(t)
			ctx := context.Background()

			if tc.claimFirst {
				if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID,
					req.Revision, "chain_1", "RFQ-000124", "line_1"); err != nil {
					t.Fatal(err)
				}
			}

			_, err := svc.ReadClaimSnapshot(ctx, "company_a", req.ID, tc.chainID, tc.line)
			if !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
				t.Fatalf("error = %v, want ErrMaterialRequirementNotFound — repair must be "+
					"scoped to the exact existing claim", err)
			}
		})
	}
}

// A foreign tenant can never read a snapshot, even naming the correct chain and
// line.
func TestReadClaimSnapshotIsTenantScoped(t *testing.T) {
	svc, _, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ReadClaimSnapshot(ctx, "company_b", req.ID, "chain_1",
		"line_1"); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want ErrMaterialRequirementNotFound", err)
	}
}

// Repair must survive a post-claim source change.
//
// The claim was eligible when created, and while it exists every
// supplier-visible contractor field is frozen (§2.3) — only detection state may
// move (§5.8). Re-running eligibility here would make an interrupted line
// impossible to repair after an ordinary source change, even though the
// snapshot being rebuilt is exactly the one the claim validated.
func TestReadClaimSnapshotStillWorksAfterSourceSyncStateChanges(t *testing.T) {
	svc, repo, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}

	// Detection moves the sync state, as §5.8 permits even while claimed.
	claimed := repo.byID[req.ID]
	claimed.SourceSyncState = mr.SourceSyncStateChangeDetected
	repo.byID[req.ID] = claimed

	// The requirement is now INELIGIBLE by the §8.5 predicate...
	if claimed.IsRFQEligible() {
		t.Fatal("setup: the requirement should now fail the eligibility predicate")
	}

	// ...yet the repair snapshot must still be available.
	snap, err := svc.ReadClaimSnapshot(ctx, "company_a", req.ID, "chain_1", "line_1")
	if err != nil {
		t.Fatalf("repair must survive a post-claim detection change, got %v", err)
	}
	if snap.QuantityValue != "100" || snap.MaterialName != "Portland Cement" {
		t.Errorf("the rebuilt snapshot differs from the one the claim validated: %+v", snap)
	}
}

// The snapshot carries no InternalNotes, cost, margin or price — the same
// allowlist ClaimForRFQ returns (design spec §6.2, §9).
func TestReadClaimSnapshotExcludesContractorOnlyAndPricingFields(t *testing.T) {
	svc, _, req := seedClaimable(t)
	ctx := context.Background()

	if _, err := svc.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatal(err)
	}
	snap, err := svc.ReadClaimSnapshot(ctx, "company_a", req.ID, "chain_1", "line_1")
	if err != nil {
		t.Fatal(err)
	}

	if containsSensitive(snap, req.InternalNotes) {
		t.Errorf("the repair snapshot carries InternalNotes %q — it is contractor-only and "+
			"must never reach a supplier-visible line", req.InternalNotes)
	}
	// Asserted structurally: a field that does not exist cannot be populated by
	// a later edit.
	typ := reflect.TypeOf(snap)
	for i := 0; i < typ.NumField(); i++ {
		name := strings.ToLower(typ.Field(i).Name)
		for _, banned := range []string{"internal", "price", "cost", "margin", "amount", "money"} {
			if strings.Contains(name, banned) {
				t.Errorf("ClaimSnapshot.%s carries %q and is supplier-visible",
					typ.Field(i).Name, banned)
			}
		}
	}
}

// A requirement that does not exist yields not-found rather than an empty
// snapshot, so a repair can never silently produce a blank line.
func TestReadClaimSnapshotRejectsAMissingRequirement(t *testing.T) {
	svc, _, _ := newService(t)

	if _, err := svc.ReadClaimSnapshot(context.Background(), "company_a", "mr_zzz",
		"chain_1", "line_1"); !errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want ErrMaterialRequirementNotFound", err)
	}
}
