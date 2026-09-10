package rfqs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// C4 covers the claim-and-append sequence, its retry matrix, and compensation
// (design spec §7.2-§7.5). rfqs owns ALL of this orchestration;
// materialrequirements only performs the conditional writes.

// --- Add line: the fail-closed sequence (design spec §7.2) ---

func TestAddLineClaimsThenAppendsSnapshot(t *testing.T) {
	svc, repo, source, rec := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	got, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision,
		"mr_1", 0)
	if err != nil {
		t.Fatalf("add line failed: %v", err)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(got.Lines))
	}
	line := got.Lines[0]

	// Every line field comes from the ClaimSnapshot the claim returned, so the
	// line reflects exactly the state the claim validated (§7.2 step 4).
	if line.SourceMaterialRequirementID != "mr_1" {
		t.Errorf("SourceMaterialRequirementID = %q, want mr_1", line.SourceMaterialRequirementID)
	}
	if line.MaterialName != "Portland Cement" || line.Specification != "OPC 50kg" {
		t.Errorf("snapshot fields not copied: %+v", line)
	}
	if line.Quantity.Value.String() != "100" || line.Quantity.Unit != "bag" {
		t.Errorf("quantity = %s %s, want 100 bag", line.Quantity.Value, line.Quantity.Unit)
	}
	if line.ProcurementNotes != "deliver to site gate" {
		t.Errorf("ProcurementNotes = %q", line.ProcurementNotes)
	}

	// The claim now names THIS chain and THIS line.
	chainID, _, lineID, _, found, err := source.ReadClaim(ctx, "company_a", "mr_1")
	if err != nil || !found {
		t.Fatalf("the requirement is not claimed: found=%v err=%v", found, err)
	}
	if chainID != created.ID {
		t.Errorf("claim names chain %q, want the rfq's own id %q", chainID, created.ID)
	}
	if lineID != line.ID {
		t.Errorf("claim names line %q, want %q — the pre-generated id is what makes a "+
			"retry deterministic", lineID, line.ID)
	}
	if rec.lineAdded != 1 {
		t.Errorf("audit lineAdded = %d, want 1", rec.lineAdded)
	}
	_ = repo
}

// The claim precedes the append. If the claim fails, no line may be appended —
// fail-closed (design spec §7.2).
func TestAddLineAppendsNothingWhenTheClaimFails(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)

	source.failClaim = rfqs.ErrMaterialRequirementAlreadyClaimed
	before := repo.replaceLinesCalls

	_, err := svc.AddLine(context.Background(), "company_a", "user_1", created.ID,
		created.Revision, "mr_1", 0)
	if !errors.Is(err, rfqs.ErrMaterialRequirementAlreadyClaimed) {
		t.Fatalf("error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
	if repo.replaceLinesCalls != before {
		t.Errorf("lines were written %d times after a failed claim, want 0",
			repo.replaceLinesCalls-before)
	}
}

// A requirement in another Project of the same company must not be claimable —
// enforced inside the atomic filter, surfaced here as not-found (§7.2).
func TestAddLineRejectsARequirementFromAnotherProject(t *testing.T) {
	svc, repo, _, _ := newService(t)
	created := createDraft(t, svc)
	before := repo.replaceLinesCalls

	_, err := svc.AddLine(context.Background(), "company_a", "user_1", created.ID,
		created.Revision, "mr_other_project", 0)
	if !errors.Is(err, rfqs.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want ErrMaterialRequirementNotFound — cross-project "+
			"contamination must be structurally impossible", err)
	}
	if repo.replaceLinesCalls != before {
		t.Error("a cross-project add wrote lines")
	}
}

// A ready RFQ accepts no line changes.
func TestAddLineRejectsAReadyRFQ(t *testing.T) {
	svc, _, source, _ := newService(t)
	ctx := context.Background()
	ready := readyRFQ(t, svc)

	before := source.claimCalls
	_, err := svc.AddLine(ctx, "company_a", "user_1", ready.ID, ready.Revision, "mr_2", 0)
	if !errors.Is(err, rfqs.ErrRFQNotDraft) {
		t.Fatalf("error = %v, want ErrRFQNotDraft", err)
	}
	// The status is checked BEFORE any claim: a rejected add must not claim a
	// requirement it cannot then use.
	if source.claimCalls != before {
		t.Errorf("a claim was attempted against a ready rfq (%d calls)",
			source.claimCalls-before)
	}
}

// --- Retry matrix (design spec §7.3) ---

// Claimed by THIS chain and the line exists -> return the existing line
// successfully. An uncertain response must never produce a duplicate.
func TestAddLineRetryReturnsTheExistingLine(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	first, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	claimsBefore := source.claimCalls

	second, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, first.Revision, "mr_1", 0)
	if err != nil {
		t.Fatalf("a retry must succeed, got %v", err)
	}
	if len(second.Lines) != 1 {
		t.Fatalf("got %d lines after a retry, want exactly 1 — no duplicate", len(second.Lines))
	}
	if second.Lines[0].ID != first.Lines[0].ID {
		t.Errorf("retry produced line %q, want the existing %q",
			second.Lines[0].ID, first.Lines[0].ID)
	}
	// No second claim is attempted: the requirement already names this chain.
	if source.claimCalls != claimsBefore {
		t.Errorf("the retry attempted %d additional claims, want 0",
			source.claimCalls-claimsBefore)
	}
	_ = repo
}

// Claimed by THIS chain but the line is MISSING -> append using the LineID from
// the claim. This repairs an interrupted operation (§7.3).
func TestAddLineRepairsAClaimWhoseLineIsMissing(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	originalLineID := added.Lines[0].ID

	// Simulate the interruption: the claim stands, the line vanished.
	stored := repo.byID[created.ID]
	stored.Lines = nil
	repo.byID[created.ID] = stored

	claimsBefore := source.claimCalls
	repaired, err := svc.AddLine(ctx, "company_a", "user_1", created.ID,
		repo.byID[created.ID].Revision, "mr_1", 0)
	if err != nil {
		t.Fatalf("the repair must succeed, got %v", err)
	}
	if len(repaired.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(repaired.Lines))
	}
	if repaired.Lines[0].ID != originalLineID {
		t.Errorf("repaired line id = %q, want the claim's %q — the stored LineID is what "+
			"makes the repair deterministic", repaired.Lines[0].ID, originalLineID)
	}
	// It must NOT re-claim: the requirement already holds this chain's claim.
	if source.claimCalls != claimsBefore {
		t.Errorf("the repair attempted %d claims, want 0", source.claimCalls-claimsBefore)
	}
}

// Claimed by ANOTHER chain -> 409, and nothing is appended.
func TestAddLineRejectsARequirementClaimedByAnotherChain(t *testing.T) {
	svc, repo, source, _ := newService(t)
	ctx := context.Background()

	first := createDraft(t, svc)
	if _, err := svc.AddLine(ctx, "company_a", "user_1", first.ID, first.Revision, "mr_1", 0); err != nil {
		t.Fatal(err)
	}

	second := createDraft(t, svc)
	before := repo.replaceLinesCalls

	_, err := svc.AddLine(ctx, "company_a", "user_1", second.ID, second.Revision, "mr_1", 0)
	if !errors.Is(err, rfqs.ErrMaterialRequirementAlreadyClaimed) {
		t.Fatalf("error = %v, want ErrMaterialRequirementAlreadyClaimed", err)
	}
	if repo.replaceLinesCalls != before {
		t.Error("a rejected add wrote lines")
	}
	// The original claim is untouched.
	chainID, _, _, _, _, _ := source.ReadClaim(ctx, "company_a", "mr_1")
	if chainID != first.ID {
		t.Errorf("the claim moved to %q, want the original %q", chainID, first.ID)
	}
}

// --- Compensation (design spec §7.5) ---

// Claim succeeded, append failed: rfqs compensates immediately using the NEW
// revision from the claim, then returns the ORIGINAL append error.
func TestAddLineCompensatesTheClaimWhenTheAppendFails(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	appendErr := errors.New("append exploded")
	repo.failReplaceLines = appendErr

	_, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err == nil {
		t.Fatal("expected the append failure to surface")
	}
	if !errors.Is(err, appendErr) {
		t.Errorf("error = %v, want the ORIGINAL append error returned after compensation", err)
	}

	// Compensation must have released the claim, so the requirement is free.
	_, _, _, _, found, readErr := source.ReadClaim(ctx, "company_a", "mr_1")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if found {
		t.Error("the claim survived a failed append; compensation did not run")
	}
	if source.releaseCalls != 1 {
		t.Errorf("release was called %d times, want 1", source.releaseCalls)
	}
}

// Compensation itself failed: the claim is LEFT IN PLACE — fail-closed. It is
// never silently cleared, and the original error is still returned (§7.5).
func TestAddLineLeavesTheClaimWhenCompensationFails(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	appendErr := errors.New("append exploded")
	repo.failReplaceLines = appendErr
	source.failRelease = errors.New("release exploded")

	_, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if !errors.Is(err, appendErr) {
		t.Errorf("error = %v, want the original append error even when compensation fails", err)
	}

	// The orphaned claim REMAINS: the requirement cannot enter another RFQ, and
	// reconciliation is the documented recovery.
	source.failRelease = nil
	chainID, _, _, _, found, _ := source.ReadClaim(ctx, "company_a", "mr_1")
	if !found {
		t.Fatal("the claim was cleared despite compensation failing — it must fail CLOSED")
	}
	if chainID != created.ID {
		t.Errorf("the orphaned claim names %q, want %q", chainID, created.ID)
	}
}

// --- Remove line: the opposite order (design spec §7.4) ---

// Line removal precedes claim release, so the system never makes a requirement
// available to another RFQ while its previous line may still exist.
func TestRemoveLineRemovesThenReleases(t *testing.T) {
	svc, _, source, rec := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	lineID := added.Lines[0].ID

	got, err := svc.RemoveLine(ctx, "company_a", "user_1", created.ID, added.Revision, lineID)
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if len(got.Lines) != 0 {
		t.Errorf("got %d lines, want 0", len(got.Lines))
	}

	_, _, _, _, found, err := source.ReadClaim(ctx, "company_a", "mr_1")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("the claim survived line removal")
	}
	if rec.lineRemoved != 1 {
		t.Errorf("audit lineRemoved = %d, want 1", rec.lineRemoved)
	}
}

// A retry arriving after the line is already gone completes the release using
// the claim's own data (design spec §7.3, §7.4).
func TestRemoveLineRetryCompletesTheReleaseWhenTheLineIsAlreadyGone(t *testing.T) {
	svc, repo, source, _ := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	lineID := added.Lines[0].ID

	// Simulate: the line write landed, the release did not.
	stored := repo.byID[created.ID]
	stored.Lines = nil
	repo.byID[created.ID] = stored

	got, err := svc.RemoveLine(ctx, "company_a", "user_1", created.ID,
		repo.byID[created.ID].Revision, lineID)
	if err != nil {
		t.Fatalf("the retry must complete the release, got %v", err)
	}
	if len(got.Lines) != 0 {
		t.Errorf("got %d lines, want 0", len(got.Lines))
	}

	_, _, _, _, found, _ := source.ReadClaim(ctx, "company_a", "mr_1")
	if found {
		t.Error("the retry did not complete the release")
	}
}

func TestRemoveLineRejectsAnUnknownLine(t *testing.T) {
	svc, _, _, _ := newService(t)
	created := createDraft(t, svc)

	if _, err := svc.RemoveLine(context.Background(), "company_a", "user_1", created.ID,
		created.Revision, "line_zzz"); !errors.Is(err, rfqs.ErrRFQLineNotFound) {
		t.Fatalf("error = %v, want ErrRFQLineNotFound", err)
	}
}

func TestRemoveLineRejectsAReadyRFQ(t *testing.T) {
	svc, _, _, _ := newService(t)
	ready := readyRFQ(t, svc)

	if _, err := svc.RemoveLine(context.Background(), "company_a", "user_1", ready.ID,
		ready.Revision, ready.Lines[0].ID); !errors.Is(err, rfqs.ErrRFQNotDraft) {
		t.Fatalf("error = %v, want ErrRFQNotDraft", err)
	}
}

// --- Sort order (design spec §6.2) ---

func TestAddLineAppendsAfterTheHighestSortOrder(t *testing.T) {
	svc, _, _, _ := newService(t)
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

	if len(second.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(second.Lines))
	}
	if second.Lines[0].SortOrder == second.Lines[1].SortOrder {
		t.Error("two lines share a sort order")
	}
}

func TestSetLineSortOrderReordersUnderRevisionGuard(t *testing.T) {
	svc, _, _, _ := newService(t)
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
	target := second.Lines[0].ID

	got, err := svc.SetLineSortOrder(ctx, "company_a", "user_1", created.ID,
		second.Revision, target, 99)
	if err != nil {
		t.Fatalf("reorder failed: %v", err)
	}
	line, ok := got.FindLine(target)
	if !ok {
		t.Fatal("the line vanished")
	}
	if line.SortOrder != 99 {
		t.Errorf("SortOrder = %d, want 99", line.SortOrder)
	}
}

// readyRFQ builds a draft with one line and marks it ready.
func readyRFQ(t *testing.T, svc *rfqs.Service) rfqs.RFQ {
	t.Helper()
	ctx := context.Background()
	created := createDraft(t, svc)

	added, err := svc.AddLine(ctx, "company_a", "user_1", created.ID, created.Revision, "mr_1", 0)
	if err != nil {
		t.Fatalf("setup add line: %v", err)
	}
	ready, err := svc.MarkReady(ctx, "company_a", "user_1", created.ID, added.Revision)
	if err != nil {
		t.Fatalf("setup mark ready: %v", err)
	}
	return ready
}
