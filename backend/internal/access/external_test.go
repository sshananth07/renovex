package access_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/access"
)

// shareAndToken shares a quotation and returns the raw token plus grant ID.
func shareAndToken(t *testing.T, h *harness, quotationID string) (string, string) {
	t.Helper()
	view, _, err := h.svc.ShareQuotation(context.Background(), "company_a", quotationID, "user_1")
	if err != nil {
		t.Fatalf("unexpected error sharing: %v", err)
	}
	return view.RawToken, view.GrantID
}

// --- External view ---

func TestViewByTokenReturnsAllowlistedQuotation(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	token, _ := shareAndToken(t, h, "quotation_1")

	view, err := h.svc.ViewQuotationByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.QuotationNumber != "QT-000001" || view.Version != 1 {
		t.Fatalf("expected the quotation identity, got %+v", view)
	}
	if view.CompanyName != "Renovate Sdn Bhd" || view.ProjectName != "Ahmad Residence" {
		t.Fatalf("expected resolved company/project names, got %q/%q", view.CompanyName, view.ProjectName)
	}
	if view.Status != "finalized" {
		t.Fatalf("expected finalized, got %q", view.Status)
	}
	if len(view.Lines) != 1 || view.Lines[0].Description != "Wall Tiles" {
		t.Fatalf("expected the commercial lines, got %+v", view.Lines)
	}
	if view.Total.Amount != 50000 || view.Total.Currency != "MYR" {
		t.Fatalf("expected the total, got %+v", view.Total)
	}
	if view.IsExpired {
		t.Fatal("expected a live quotation not to read as expired")
	}
	if h.audit.countOf("client_viewed") != 1 {
		t.Fatalf("expected a Client Viewed audit event, got %d", h.audit.countOf("client_viewed"))
	}
}

func TestViewRecordsEveryViewSeparately(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	token, _ := shareAndToken(t, h, "quotation_1")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := h.svc.ViewQuotationByToken(ctx, token); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if h.audit.countOf("client_viewed") != 3 {
		t.Fatalf("expected 3 view events (no collapsing), got %d", h.audit.countOf("client_viewed"))
	}
}

// Every unusable-token shape must fail identically (design spec §2.3/§16.2).
func TestUnusableTokensAllReturnTheSameError(t *testing.T) {
	ctx := context.Background()

	t.Run("malformed", func(t *testing.T) {
		h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
		if _, err := h.svc.ViewQuotationByToken(ctx, "!!!not-a-token!!!"); err != access.ErrExternalTokenUnusable {
			t.Fatalf("expected ErrExternalTokenUnusable, got %v", err)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
		if _, err := h.svc.ViewQuotationByToken(ctx, "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZmdoaWo"); err != access.ErrExternalTokenUnusable {
			t.Fatalf("expected ErrExternalTokenUnusable, got %v", err)
		}
	})

	t.Run("revoked", func(t *testing.T) {
		h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
		token, grantID := shareAndToken(t, h, "quotation_1")
		if _, err := h.svc.RevokeGrant(ctx, "company_a", grantID, "user_1", 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := h.svc.ViewQuotationByToken(ctx, token); err != access.ErrExternalTokenUnusable {
			t.Fatalf("expected ErrExternalTokenUnusable, got %v", err)
		}
	})

	t.Run("superseded", func(t *testing.T) {
		h := newHarness(t).
			withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil)).
			withQuotation(snapshotFor("quotation_2", "QT-000001", 2, nil))
		v1Token, _ := shareAndToken(t, h, "quotation_1")
		if _, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_2", "user_1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := h.svc.ViewQuotationByToken(ctx, v1Token); err != access.ErrExternalTokenUnusable {
			t.Fatalf("expected ErrExternalTokenUnusable, got %v", err)
		}
	})

	t.Run("rotated", func(t *testing.T) {
		h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
		oldToken, grantID := shareAndToken(t, h, "quotation_1")
		if _, err := h.svc.RotateGrant(ctx, "company_a", grantID, "user_1", 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := h.svc.ViewQuotationByToken(ctx, oldToken); err != access.ErrExternalTokenUnusable {
			t.Fatalf("expected the pre-rotation token to be dead, got %v", err)
		}
	})

	t.Run("expired grant", func(t *testing.T) {
		h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
		token, grantID := shareAndToken(t, h, "quotation_1")

		// Drive expiry deterministically through the repository rather than
		// sleeping: a wall-clock-dependent test is slow and flaky under load.
		expired := time.Now().Add(-time.Hour)
		if _, err := h.grants.UpdateExpiry(ctx, "company_a", grantID, 0, expired); err != nil {
			t.Fatalf("unexpected error expiring the grant: %v", err)
		}

		if _, err := h.svc.ViewQuotationByToken(ctx, token); err != access.ErrExternalTokenUnusable {
			t.Fatalf("expected an expired grant to be dead, got %v", err)
		}
	})
}

// A stored-active grant the coordinator no longer references (an orphan from
// a lost race) must be unusable purely on coordinator mismatch.
func TestOrphanedStoredActiveGrantIsUnusable(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	token, grantID := shareAndToken(t, h, "quotation_1")

	// Point the coordinator at a different grant WITHOUT touching the grant's
	// own stored status — exactly the orphan shape.
	coordinator, err := h.groups.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := h.groups.ClaimActiveGrant(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001",
		coordinator.Revision, "quotation_1", &grantID, "quotation_1", "some_other_grant", true); err != nil {
		t.Fatalf("unexpected error re-pointing the coordinator: %v", err)
	}

	stored, _ := h.grants.FindByID(ctx, "company_a", grantID)
	if stored.Status != access.AccessGrantStatusActive {
		t.Fatalf("precondition: expected the grant to remain stored-active, got %q", stored.Status)
	}
	if _, err := h.svc.ViewQuotationByToken(ctx, token); err != access.ErrExternalTokenUnusable {
		t.Fatalf("expected an orphaned stored-active grant to be unusable, got %v", err)
	}
}

// An expired Quotation stays viewable read-only through a live grant, but no
// decision may be submitted (design spec §10).
func TestExpiredQuotationIsViewableButNotActionable(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	// The grant stays live (30-day default); the Quotation itself expires.
	past := time.Now().Add(-time.Hour)
	expired := snapshotFor("quotation_1", "QT-000001", 1, &past)
	h.quotations.bySnapshotID["quotation_1"] = expired

	view, err := h.svc.ViewQuotationByToken(ctx, token)
	if err != nil {
		t.Fatalf("expected an expired quotation to remain viewable, got %v", err)
	}
	if !view.IsExpired {
		t.Fatal("expected IsExpired=true")
	}

	_, err = h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{
		Token: token, Status: "accepted",
	})
	if err != access.ErrQuotationExpiredForDecision {
		t.Fatalf("expected ErrQuotationExpiredForDecision, got %v", err)
	}
}

// --- Client decisions ---

func TestAcceptClaimsCoordinatorThenWritesApproval(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	result, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{
		Token: token, Status: "accepted", ClientName: "Ahmad", ClientEmail: "a@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "accepted" {
		t.Fatalf("expected accepted, got %q", result.Status)
	}

	// The coordinator carries the authoritative terminal fact.
	coordinator, _ := h.groups.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if coordinator.AcceptedResourceID == nil || *coordinator.AcceptedResourceID != "quotation_1" {
		t.Fatalf("expected AcceptedResourceID set, got %+v", coordinator.AcceptedResourceID)
	}
	if h.decisions.recordCalls != 1 {
		t.Fatalf("expected exactly one Approval write, got %d", h.decisions.recordCalls)
	}
	if h.projects.approvedCalls != 1 {
		t.Fatalf("expected the project advanced to approved once, got %d", h.projects.approvedCalls)
	}
	if h.audit.countOf("client_decision") != 1 {
		t.Fatalf("expected one decision audit event, got %d", h.audit.countOf("client_decision"))
	}
}

func TestRepeatAcceptIsIdempotent(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	in := access.ClientDecisionInput{Token: token, Status: "accepted", ClientName: "Ahmad"}
	if _, err := h.svc.SubmitClientDecision(ctx, in); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := h.svc.SubmitClientDecision(ctx, in)
	if err != nil {
		t.Fatalf("expected a repeat accept to be idempotent, got %v", err)
	}
	if result.Status != "accepted" {
		t.Fatalf("expected accepted, got %q", result.Status)
	}
	// The second call reconciles rather than re-recording a new decision.
	if h.decisions.reconcileCalls != 1 {
		t.Fatalf("expected the repeat accept to reconcile once, got %d", h.decisions.reconcileCalls)
	}
	if h.audit.countOf("client_decision") != 1 {
		t.Fatalf("expected no duplicate decision audit event, got %d", h.audit.countOf("client_decision"))
	}
}

// If the coordinator claim succeeded but the Approval write failed, a later
// repeat accept must reconcile the Approval record (design spec §7.2).
func TestRepeatAcceptReconcilesAMissingApproval(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	// First accept: the coordinator claim lands, the Approval write fails.
	h.decisions.failRecordWithErr = errFakeApprovalWrite
	_, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{Token: token, Status: "accepted"})
	if err == nil {
		t.Fatal("expected the failing Approval write to surface an error")
	}

	coordinator, _ := h.groups.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if coordinator.AcceptedResourceID == nil {
		t.Fatal("expected the coordinator claim to remain authoritative despite the Approval failure")
	}

	// Second accept: reconciles the missing Approval and succeeds.
	h.decisions.failRecordWithErr = nil
	result, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{Token: token, Status: "accepted"})
	if err != nil {
		t.Fatalf("expected the repeat accept to reconcile and succeed, got %v", err)
	}
	if result.Status != "accepted" {
		t.Fatalf("expected accepted, got %q", result.Status)
	}
	if h.decisions.reconcileCalls != 1 {
		t.Fatalf("expected exactly one reconciliation, got %d", h.decisions.reconcileCalls)
	}
}

func TestRejectAndRequestChangesRequireAComment(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	for _, status := range []string{"rejected", "changes_requested"} {
		if _, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{
			Token: token, Status: status, Comment: "   ",
		}); err != access.ErrCommentRequired {
			t.Fatalf("%s: expected ErrCommentRequired for a blank comment, got %v", status, err)
		}
	}

	// Accept does NOT require a comment.
	if _, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{
		Token: token, Status: "accepted",
	}); err != nil {
		t.Fatalf("expected accept to allow an empty comment, got %v", err)
	}
}

func TestClientFieldLimitsAndEmailValidation(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	cases := []struct {
		name string
		in   access.ClientDecisionInput
		want error
	}{
		{"name too long", access.ClientDecisionInput{Token: token, Status: "accepted",
			ClientName: strings.Repeat("a", access.MaxClientNameLength+1)}, access.ErrFieldTooLong},
		{"email too long", access.ClientDecisionInput{Token: token, Status: "accepted",
			ClientEmail: strings.Repeat("a", access.MaxClientEmailLength+1)}, access.ErrFieldTooLong},
		{"comment too long", access.ClientDecisionInput{Token: token, Status: "accepted",
			Comment: strings.Repeat("a", access.MaxCommentLength+1)}, access.ErrFieldTooLong},
		{"invalid email", access.ClientDecisionInput{Token: token, Status: "accepted",
			ClientEmail: "not-an-email"}, access.ErrInvalidEmail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.svc.SubmitClientDecision(ctx, tc.in); err != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestRejectAfterAcceptanceIsRefused(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	if _, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{Token: token, Status: "accepted"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{
		Token: token, Status: "rejected", Comment: "changed my mind",
	})
	if err != access.ErrDecisionAlreadyAccepted {
		t.Fatalf("expected ErrDecisionAlreadyAccepted, got %v", err)
	}
}

// A non-terminal decision must not land once the chain has moved to a newer
// version — the fence blocks it (design spec §7.3).
func TestRejectAfterSupersessionIsRefused(t *testing.T) {
	h := newHarness(t).
		withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil)).
		withQuotation(snapshotFor("quotation_2", "QT-000001", 2, nil))
	ctx := context.Background()

	v1Token, _ := shareAndToken(t, h, "quotation_1")
	if _, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_2", "user_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// V1's token is dead the moment the coordinator switched.
	if _, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{
		Token: v1Token, Status: "rejected", Comment: "too expensive",
	}); err != access.ErrExternalTokenUnusable {
		t.Fatalf("expected ErrExternalTokenUnusable, got %v", err)
	}
	if h.decisions.recordCalls != 0 {
		t.Fatalf("expected NO Approval write for a dead token, got %d", h.decisions.recordCalls)
	}
}

func TestRejectDoesNotAdvanceProjectStatus(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()
	token, _ := shareAndToken(t, h, "quotation_1")

	if _, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{
		Token: token, Status: "rejected", Comment: "too expensive",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.projects.approvedCalls != 0 {
		t.Fatalf("expected a rejection to never advance the project, got %d calls", h.projects.approvedCalls)
	}

	coordinator, _ := h.groups.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if coordinator.AcceptedResourceID != nil {
		t.Fatal("expected a rejection to leave the chain un-accepted")
	}
}

func TestAcceptedChainCannotShareAnotherVersion(t *testing.T) {
	h := newHarness(t).
		withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil)).
		withQuotation(snapshotFor("quotation_2", "QT-000001", 2, nil))
	ctx := context.Background()

	token, _ := shareAndToken(t, h, "quotation_1")
	if _, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{Token: token, Status: "accepted"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_2", "user_1"); err != access.ErrQuotationChainAlreadyAccepted {
		t.Fatalf("expected ErrQuotationChainAlreadyAccepted, got %v", err)
	}
}

// Rotating the accepted version's own grant stays allowed for security
// purposes, and never touches the Approval or the accepted fact (§9.1).
func TestRotatingAcceptedVersionsOwnGrantIsAllowed(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	token, grantID := shareAndToken(t, h, "quotation_1")
	if _, err := h.svc.SubmitClientDecision(ctx, access.ClientDecisionInput{Token: token, Status: "accepted"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := h.svc.RotateGrant(ctx, "company_a", grantID, "user_1", 0)
	if err != nil {
		t.Fatalf("expected rotating the accepted version's grant to be allowed, got %v", err)
	}
	if rotated.RawToken == "" {
		t.Fatal("expected a fresh token")
	}

	coordinator, _ := h.groups.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if coordinator.AcceptedResourceID == nil || *coordinator.AcceptedResourceID != "quotation_1" {
		t.Fatal("expected rotation to leave the accepted fact untouched")
	}
}
