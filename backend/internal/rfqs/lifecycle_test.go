package rfqs_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// C5 covers the lifecycle transitions, C3's deletion guard, and C6's
// reconciliation and M8 handoff capabilities.

// --- Mark ready (design spec §6.4) ---

func TestMarkReadyRequiresLinesAndDeliveryAddress(t *testing.T) {
	t.Run("no lines", func(t *testing.T) {
		svc, _, _, _ := newService(t)
		created := createDraft(t, svc)

		if _, err := svc.MarkReady(context.Background(), "company_a", "user_1",
			created.ID, created.Revision); !errors.Is(err, rfqs.ErrRFQNoLines) {
			t.Fatalf("error = %v, want ErrRFQNoLines", err)
		}
	})

	t.Run("no delivery address", func(t *testing.T) {
		svc, repo, _, _ := newService(t)
		ctx := context.Background()
		created, err := svc.CreateRFQ(ctx, "company_a", "user_1", "project_1",
			rfqs.CreateRFQInput{Title: "No address"})
		if err != nil {
			t.Fatal(err)
		}
		added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := svc.MarkReady(ctx, "company_a", "user_1", created.ID,
			added.Revision); !errors.Is(err, rfqs.ErrRFQDeliveryAddressRequired) {
			t.Fatalf("error = %v, want ErrRFQDeliveryAddressRequired — a supplier cannot "+
				"quote delivery to nowhere", err)
		}
		if repo.byID[created.ID].Status != rfqs.RFQStatusDraft {
			t.Error("the rfq moved to ready despite failing validation")
		}
	})
}

// Every line's claimed requirement is validated with
// ClaimedRequirementIsReadyForRFQ — NOT the unclaimed §8.5 predicate, which
// would reject every non-empty RFQ (design spec §6.4).
func TestMarkReadyValidatesEveryClaimedLine(t *testing.T) {
	svc, _, source, rec := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	first, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, first.Revision, "mr_2", 0)
	if err != nil {
		t.Fatal(err)
	}

	// One line's requirement is no longer ready.
	source.reqs["mr_2"].readyOK = false

	if _, err := svc.MarkReady(ctx, "company_a", "user_1", created.ID,
		second.Revision); !errors.Is(err, rfqs.ErrRFQLineNotEligible) {
		t.Fatalf("error = %v, want ErrRFQLineNotEligible", err)
	}
	if rec.markedReady != 0 {
		t.Error("a failed mark-ready was audited as success")
	}

	// Once it is ready again the transition succeeds.
	source.reqs["mr_2"].readyOK = true
	ready, err := svc.MarkReady(ctx, "company_a", "user_1", created.ID, second.Revision)
	if err != nil {
		t.Fatalf("mark ready failed: %v", err)
	}
	if ready.Status != rfqs.RFQStatusReady {
		t.Errorf("Status = %q, want ready", ready.Status)
	}
	if ready.ReadyAt == nil {
		t.Error("ReadyAt was not set")
	}
	if ready.Revision != second.Revision+1 {
		t.Errorf("Revision = %d, want an increment (the ABA hazard)", ready.Revision)
	}
	if rec.markedReady != 1 {
		t.Errorf("audit markedReady = %d, want 1", rec.markedReady)
	}
}

// A NON-EMPTY rfq must be markable as ready. This is the regression guard for
// the §6.4/§8.5 trap: reusing the unclaimed predicate here would make every
// real RFQ permanently unmarkable.
func TestMarkReadySucceedsForANonEmptyRFQ(t *testing.T) {
	svc, _, _, _ := newService(t)
	ready := readyRFQ(t, svc)

	if ready.Status != rfqs.RFQStatusReady {
		t.Fatalf("Status = %q, want ready — a non-empty rfq MUST be markable, or the "+
			"unclaimed §8.5 predicate has been wrongly reused here", ready.Status)
	}
}

// --- Reopen (design spec §6.4) ---

func TestReopenReturnsToDraftAndIncrementsRevision(t *testing.T) {
	svc, _, source, rec := newService(t)
	ready := readyRFQ(t, svc)
	ctx := context.Background()

	reopened, err := svc.Reopen(ctx, "company_a", "user_1", ready.ID, ready.Revision)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	if reopened.Status != rfqs.RFQStatusDraft {
		t.Errorf("Status = %q, want draft", reopened.Status)
	}
	if reopened.ReopenedAt == nil {
		t.Error("ReopenedAt was not set")
	}
	if reopened.Revision != ready.Revision+1 {
		t.Errorf("Revision = %d, want an increment", reopened.Revision)
	}
	if rec.reopened != 1 {
		t.Errorf("audit reopened = %d, want 1", rec.reopened)
	}

	// Claims survive the transition: changing status never releases a
	// requirement (design spec §6.4).
	_, _, _, _, found, err := source.ReadClaim(ctx, "company_a", "mr_1")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("reopen released a claim; only explicit line removal may do that")
	}
}

// stubIssuance lets a test drive the M8 seam's three outcomes.
type stubIssuance struct {
	issued bool
	err    error
}

func (s stubIssuance) RFQChainIssued(context.Context, string, string) (bool, error) {
	return s.issued, s.err
}

// Issued -> 409, and the RFQ REMAINS ready (design spec §6.4).
func TestReopenRefusedWhenTheChainIsAlreadyIssued(t *testing.T) {
	svc, repo, _, _ := newService(t)
	ready := readyRFQ(t, svc)

	svc.SetIssuanceStatusSource(stubIssuance{issued: true})

	if _, err := svc.Reopen(context.Background(), "company_a", "user_1", ready.ID,
		ready.Revision); !errors.Is(err, rfqs.ErrRFQAlreadyIssued) {
		t.Fatalf("error = %v, want ErrRFQAlreadyIssued", err)
	}
	if repo.byID[ready.ID].Status != rfqs.RFQStatusReady {
		t.Error("the rfq was reopened despite being issued; it must REMAIN ready")
	}
}

// The seam errored -> 503, and the RFQ remains ready. Reopen FAILS CLOSED
// rather than proceeding on an unverified assumption (design spec §6.4).
func TestReopenFailsClosedWhenIssuanceStatusIsUnavailable(t *testing.T) {
	svc, repo, _, _ := newService(t)
	ready := readyRFQ(t, svc)

	svc.SetIssuanceStatusSource(stubIssuance{err: errors.New("m8 unreachable")})

	if _, err := svc.Reopen(context.Background(), "company_a", "user_1", ready.ID,
		ready.Revision); !errors.Is(err, rfqs.ErrIssuanceStatusUnavailable) {
		t.Fatalf("error = %v, want ErrIssuanceStatusUnavailable", err)
	}
	if repo.byID[ready.ID].Status != rfqs.RFQStatusReady {
		t.Error("the rfq was reopened on an unverified issuance status; it must FAIL CLOSED")
	}
}

func TestReopenRejectsADraftRFQ(t *testing.T) {
	svc, _, _, _ := newService(t)
	created := createDraft(t, svc)

	if _, err := svc.Reopen(context.Background(), "company_a", "user_1", created.ID,
		created.Revision); !errors.Is(err, rfqs.ErrRFQNotReady) {
		t.Fatalf("error = %v, want ErrRFQNotReady", err)
	}
}

// --- Deletion: the three §7.7 conditions ---

func TestDeleteRequiresDraftEmptyAndNoOutstandingClaims(t *testing.T) {
	t.Run("empty draft deletes", func(t *testing.T) {
		svc, repo, _, rec := newService(t)
		created := createDraft(t, svc)

		if err := svc.DeleteRFQ(context.Background(), "company_a", "user_1",
			created.ID, created.Revision); err != nil {
			t.Fatalf("delete failed: %v", err)
		}
		if _, ok := repo.byID[created.ID]; ok {
			t.Error("the rfq was not deleted")
		}
		if rec.deleted != 1 {
			t.Errorf("audit deleted = %d, want 1", rec.deleted)
		}
	})

	t.Run("rfq with lines is refused", func(t *testing.T) {
		svc, _, _, _ := newService(t)
		created := createDraft(t, svc)
		ctx := context.Background()
		added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
		if err != nil {
			t.Fatal(err)
		}

		if err := svc.DeleteRFQ(ctx, "company_a", "user_1", created.ID,
			added.Revision); !errors.Is(err, rfqs.ErrRFQNotEmpty) {
			t.Fatalf("error = %v, want ErrRFQNotEmpty", err)
		}
	})

	t.Run("ready rfq is refused", func(t *testing.T) {
		svc, _, _, _ := newService(t)
		ready := readyRFQ(t, svc)

		if err := svc.DeleteRFQ(context.Background(), "company_a", "user_1", ready.ID,
			ready.Revision); !errors.Is(err, rfqs.ErrRFQNotDraft) {
			t.Fatalf("error = %v, want ErrRFQNotDraft", err)
		}
	})
}

// The THIRD condition, and the reason it is not redundant: an orphaned claim is
// precisely a claim with NO line, so an RFQ can be line-empty while still
// holding a requirement hostage. Deleting it would strand that requirement
// permanently — ActiveRFQChainID would name a chain that no longer exists, with
// no RFQ left to reconcile through (design spec §7.7).
func TestDeleteRefusedWhenAnOrphanedClaimStillNamesTheChain(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}

	// The line is gone but the claim survives — exactly the §7.5 orphan.
	stored := repo.byID[created.ID]
	stored.Lines = nil
	repo.byID[created.ID] = stored

	if !repo.byID[created.ID].IsDeletable() {
		t.Fatal("setup: the rfq must look deletable on the aggregate alone")
	}

	err = svc.DeleteRFQ(ctx, "company_a", "user_1", created.ID, repo.byID[created.ID].Revision)
	if !errors.Is(err, rfqs.ErrRFQHasOutstandingClaims) {
		t.Fatalf("error = %v, want ErrRFQHasOutstandingClaims — deleting would strand the "+
			"requirement permanently", err)
	}
	if _, ok := repo.byID[created.ID]; !ok {
		t.Error("the rfq was deleted despite an outstanding claim")
	}

	// After reconciling with release, deletion succeeds.
	if _, err := svc.ReconcileClaim(ctx, "company_a", "user_1", created.ID, "mr_1",
		rfqs.ReconcileActionRelease); err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if err := svc.DeleteRFQ(ctx, "company_a", "user_1", created.ID,
		repo.byID[created.ID].Revision); err != nil {
		t.Fatalf("delete after reconciliation failed: %v", err)
	}
	_ = added
	_ = source
}

// --- C6: reconciliation (design spec §7.5) ---

// GET joins ListClaimsForRFQChain against this chain's own lines. ReadClaim
// alone could not find an orphan: it answers only for a requirement ID the
// caller already has.
func TestClaimReconciliationClassifiesConsistentAndOrphanedClaims(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	first, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, first.Revision, "mr_2", 0)
	if err != nil {
		t.Fatal(err)
	}

	// mr_2's line vanishes; its claim survives.
	stored := repo.byID[created.ID]
	var kept []rfqs.RFQLine
	for _, l := range stored.Lines {
		if l.SourceMaterialRequirementID != "mr_2" {
			kept = append(kept, l)
		}
	}
	stored.Lines = kept
	repo.byID[created.ID] = stored

	report, err := svc.GetClaimReconciliation(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("reconciliation read failed: %v", err)
	}

	byRequirement := map[string]string{}
	for _, e := range report.Entries {
		byRequirement[e.RequirementID] = e.Classification
	}
	if byRequirement["mr_1"] != rfqs.ReconciliationConsistent {
		t.Errorf("mr_1 = %q, want consistent", byRequirement["mr_1"])
	}
	if byRequirement["mr_2"] != rfqs.ReconciliationOrphanedClaim {
		t.Errorf("mr_2 = %q, want orphaned_claim — a claim with no line is the actionable case",
			byRequirement["mr_2"])
	}
	_ = second
	_ = source
}

// A line whose requirement no longer claims this chain is an orphaned_line.
func TestClaimReconciliationClassifiesOrphanedLines(t *testing.T) {
	svc, _, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	if _, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0); err != nil {
		t.Fatal(err)
	}

	// The claim vanishes; the line survives.
	source.reqs["mr_1"].chainID = ""
	source.reqs["mr_1"].lineID = ""

	report, err := svc.GetClaimReconciliation(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(report.Entries))
	}
	if report.Entries[0].Classification != rfqs.ReconciliationOrphanedLine {
		t.Errorf("classification = %q, want orphaned_line", report.Entries[0].Classification)
	}
}

// retry_line appends the missing line using the LineID from the CLAIM, not from
// client input — so the repair is deterministic (design spec §7.5).
func TestReconcileRetryLineAppendsUsingTheClaimsLineID(t *testing.T) {
	svc, repo, source, rec := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	claimLineID := added.Lines[0].ID

	stored := repo.byID[created.ID]
	stored.Lines = nil
	repo.byID[created.ID] = stored

	got, err := svc.ReconcileClaim(ctx, "company_a", "user_1", created.ID, "mr_1",
		rfqs.ReconcileActionRetryLine)
	if err != nil {
		t.Fatalf("retry_line failed: %v", err)
	}
	if len(got.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(got.Lines))
	}
	if got.Lines[0].ID != claimLineID {
		t.Errorf("line id = %q, want the claim's %q", got.Lines[0].ID, claimLineID)
	}
	if rec.reconciled != 1 || rec.lastReconcileAction != rfqs.ReconcileActionRetryLine {
		t.Errorf("audit = %d/%q, want 1/retry_line", rec.reconciled, rec.lastReconcileAction)
	}
	_ = source
}

// release clears the claim under the Revision guard taken from the claim itself.
func TestReconcileReleaseClearsTheClaim(t *testing.T) {
	svc, repo, source, rec := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	if _, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0); err != nil {
		t.Fatal(err)
	}
	stored := repo.byID[created.ID]
	stored.Lines = nil
	repo.byID[created.ID] = stored

	if _, err := svc.ReconcileClaim(ctx, "company_a", "user_1", created.ID, "mr_1",
		rfqs.ReconcileActionRelease); err != nil {
		t.Fatalf("release failed: %v", err)
	}

	_, _, _, _, found, err := source.ReadClaim(ctx, "company_a", "mr_1")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("the claim was not released")
	}
	if rec.lastReconcileAction != rfqs.ReconcileActionRelease {
		t.Errorf("audit action = %q, want release", rec.lastReconcileAction)
	}
}

func TestReconcileRejectsAnUnknownAction(t *testing.T) {
	svc, _, _, _ := newService(t)
	created := createDraft(t, svc)

	if _, err := svc.ReconcileClaim(context.Background(), "company_a", "user_1",
		created.ID, "mr_1", "delete_everything"); !errors.Is(err, rfqs.ErrReconciliationActionInvalid) {
		t.Fatalf("error = %v, want ErrReconciliationActionInvalid", err)
	}
}

// Reconciling a requirement that holds no claim on this chain is refused —
// there is nothing to retry or release.
func TestReconcileRejectsARequirementWithNoClaimOnThisChain(t *testing.T) {
	svc, _, _, _ := newService(t)
	created := createDraft(t, svc)

	if _, err := svc.ReconcileClaim(context.Background(), "company_a", "user_1",
		created.ID, "mr_1", rfqs.ReconcileActionRelease); !errors.Is(err, rfqs.ErrNoClaimToReconcile) {
		t.Fatalf("error = %v, want ErrNoClaimToReconcile", err)
	}
}

// --- C6: the three M8 handoff capabilities (design spec §9) ---

// Only READY rfqs are handoff-visible: a draft returns found=false.
func TestGetReadyRFQSnapshotOnlyExposesReadyRFQs(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()
	created := createDraft(t, svc)

	if _, _, _, _, _, _, _, _, _, found, err := svc.GetReadyRFQSnapshot(ctx, "company_a",
		created.ID); err != nil || found {
		t.Fatalf("a draft rfq must not be handoff-visible: found=%v err=%v", found, err)
	}

	ready := readyRFQ(t, svc)
	number, projectID, _, _, address, _, _, instructions, lines, found, err := svc.GetReadyRFQSnapshot(
		ctx, "company_a", ready.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("a ready rfq must be handoff-visible")
	}
	if number != ready.RFQNumber || projectID != "project_1" {
		t.Errorf("header = %q/%q", number, projectID)
	}
	if address != "12 Site Road" {
		t.Errorf("deliveryAddress = %q", address)
	}
	if instructions != ready.SupplierInstructions {
		t.Errorf("supplierInstructions = %q", instructions)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].MaterialName != "Portland Cement" || lines[0].QuantityValue != "100" {
		t.Errorf("line snapshot = %+v", lines[0])
	}
}

func TestGetReadyRFQSnapshotIsTenantScoped(t *testing.T) {
	svc, _, _, _ := newService(t)
	ready := readyRFQ(t, svc)

	if _, _, _, _, _, _, _, _, _, found, err := svc.GetReadyRFQSnapshot(context.Background(),
		"company_b", ready.ID); err != nil || found {
		t.Fatalf("a foreign company must not see the rfq: found=%v err=%v", found, err)
	}
}

// M8 design spec §2.1A (Revision 3): the projection carries the identifiers an
// immutable issued line must persist.
//
// SourceMaterialRequirementID is what copy-forward matches on (approved
// decision 11) and what award traceability references. Without it in the
// projection, M8 would have to either match on the M7 line ID — weaker than the
// approved rule — or reach into materialrequirements, breaking the boundary.
func TestGetReadyRFQSnapshotCarriesTheIdentifiersIssuanceRequires(t *testing.T) {
	svc, _, _, _ := newService(t)
	ready := readyRFQ(t, svc)

	_, _, revision, title, _, _, _, _, lines, found, err := svc.GetReadyRFQSnapshot(
		context.Background(), "company_a", ready.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("a ready rfq must be handoff-visible")
	}
	if revision != ready.Revision {
		t.Errorf("revision = %d, want the exact ready RFQ revision %d",
			revision, ready.Revision)
	}

	if title != "Cement and aggregate" {
		t.Errorf("title = %q, want the RFQ title; an issued version records it", title)
	}

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	line := lines[0]
	if line.SourceMaterialRequirementID != "mr_1" {
		t.Errorf("SourceMaterialRequirementID = %q, want mr_1. Copy-forward matches on "+
			"this field, not on Material ID alone (M8 approved decision 11)",
			line.SourceMaterialRequirementID)
	}
	if line.MaterialID != "material_1" {
		t.Errorf("MaterialID = %q, want material_1", line.MaterialID)
	}
	// The existing fields must survive the widening.
	if line.LineID == "" {
		t.Error("LineID must remain populated; it becomes SourceM7RFQLineID on the issued line")
	}
	if line.MaterialName != "Portland Cement" {
		t.Errorf("MaterialName = %q", line.MaterialName)
	}
}

// The widening exposes IDENTIFIERS only. The supplier-visible allowlist is a
// property of the type: a contractor-only note, a cost, a margin or a price has
// nowhere to land, so it cannot leak even by mistake (design spec §2.1A).
func TestRFQLineSnapshotExposesNoContractorPrivateFields(t *testing.T) {
	forbidden := []string{
		"InternalNotes",
		"Cost", "EstimatedCost", "CommittedCost", "ActualCost",
		"Price", "IndicativePrice", "UnitPrice",
		"Margin", "MarkUp", "Markup",
		"PreferredSupplier", "SupplierID",
	}

	snapshotType := reflect.TypeOf(rfqs.RFQLineSnapshot{})
	for i := 0; i < snapshotType.NumField(); i++ {
		name := snapshotType.Field(i).Name
		for _, bad := range forbidden {
			if name == bad {
				t.Errorf("RFQLineSnapshot has field %q. The M8 handoff projection carries "+
					"identifiers and supplier-visible content only; this field would give "+
					"contractor-private data a destination (design spec §2.1A)", name)
			}
		}
	}
}

func TestRFQChainIsReady(t *testing.T) {
	svc, _, _, _ := newService(t)
	ctx := context.Background()

	created := createDraft(t, svc)
	if ready, err := svc.RFQChainIsReady(ctx, "company_a", created.ID); err != nil || ready {
		t.Errorf("a draft chain reported ready=%v err=%v", ready, err)
	}

	readyRFQ := readyRFQ(t, svc)
	if ready, err := svc.RFQChainIsReady(ctx, "company_a", readyRFQ.ID); err != nil || !ready {
		t.Errorf("a ready chain reported ready=%v err=%v", ready, err)
	}
}

// ReopenPermitted answers the same question reopen enforces, without mutating.
func TestReopenPermittedReflectsTheIssuanceSeam(t *testing.T) {
	svc, _, _, _ := newService(t)
	ready := readyRFQ(t, svc)
	ctx := context.Background()

	permitted, err := svc.ReopenPermitted(ctx, "company_a", ready.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !permitted {
		t.Error("M7 issues nothing, so reopen must always be permitted")
	}

	svc.SetIssuanceStatusSource(stubIssuance{issued: true})
	permitted, err = svc.ReopenPermitted(ctx, "company_a", ready.ID)
	if err != nil {
		t.Fatal(err)
	}
	if permitted {
		t.Error("an issued chain must not be reopenable")
	}
}
