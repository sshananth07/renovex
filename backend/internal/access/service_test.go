package access_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/access"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// --- fakes ---

type fakeQuotationSource struct {
	bySnapshotID map[string]access.ShareableQuotationSnapshot
}

func (f *fakeQuotationSource) GetFinalizedQuotationForShare(_ context.Context, companyID, quotationID string) (access.ShareableQuotationSnapshot, bool, error) {
	s, ok := f.bySnapshotID[quotationID]
	if !ok || s.CompanyID != companyID {
		return access.ShareableQuotationSnapshot{}, false, nil
	}
	return s, true, nil
}

type fakeProjectStatus struct {
	sentCalls     int
	approvedCalls int
}

func (f *fakeProjectStatus) AdvanceProjectToQuotationSent(_ context.Context, _, _ string) (bool, bool, error) {
	f.sentCalls++
	return true, true, nil
}
func (f *fakeProjectStatus) AdvanceProjectToQuotationApproved(_ context.Context, _, _ string) (bool, bool, error) {
	f.approvedCalls++
	return true, true, nil
}
func (f *fakeProjectStatus) GetProjectName(_ context.Context, _, _ string) (string, bool, error) {
	return "Ahmad Residence", true, nil
}

type recordedDecision struct {
	status, comment, actorName, actorEmail, grantID string
	decidedAt                                       time.Time
}

type fakeDecisions struct {
	bySubject         map[string]recordedDecision
	reconcileCalls    int
	recordCalls       int
	failRecordWithErr error
}

func newFakeDecisions() *fakeDecisions {
	return &fakeDecisions{bySubject: map[string]recordedDecision{}}
}

func (f *fakeDecisions) RecordDecision(_ context.Context, companyID, subjectType, subjectID, _, actorName, actorEmail, comment, status, grantID string) (string, time.Time, bool, error) {
	f.recordCalls++
	if f.failRecordWithErr != nil {
		return "", time.Time{}, false, f.failRecordWithErr
	}
	key := companyID + "|" + subjectType + "|" + subjectID
	_, existed := f.bySubject[key]
	f.bySubject[key] = recordedDecision{status: status, comment: comment, actorName: actorName, actorEmail: actorEmail, grantID: grantID, decidedAt: time.Now()}
	return "approval_1", time.Now(), !existed, nil
}

func (f *fakeDecisions) ReconcileAccepted(_ context.Context, companyID, subjectType, subjectID, _, actorName, actorEmail, comment, grantID string) (string, time.Time, error) {
	f.reconcileCalls++
	key := companyID + "|" + subjectType + "|" + subjectID
	f.bySubject[key] = recordedDecision{status: "accepted", comment: comment, actorName: actorName, actorEmail: actorEmail, grantID: grantID, decidedAt: time.Now()}
	return "approval_1", time.Now(), nil
}

func (f *fakeDecisions) GetDecision(_ context.Context, companyID, subjectType, subjectID string) (string, string, string, string, time.Time, bool, error) {
	d, ok := f.bySubject[companyID+"|"+subjectType+"|"+subjectID]
	if !ok {
		return "", "", "", "", time.Time{}, false, nil
	}
	return d.status, d.comment, d.actorName, d.actorEmail, d.decidedAt, true, nil
}

type fakeCompanies struct{}

func (fakeCompanies) GetCompanyName(_ context.Context, _ string) (string, error) {
	return "Renovate Sdn Bhd", nil
}

type auditCall struct {
	kind, actorUserID, grantID, quotationID, reason, decisionStatus string
}

type fakeAudit struct{ calls []auditCall }

func (f *fakeAudit) RecordQuotationSent(_ context.Context, _, _, actorUserID, quotationID, _ string, _ int) error {
	f.calls = append(f.calls, auditCall{kind: "quotation_sent", actorUserID: actorUserID, quotationID: quotationID})
	return nil
}
func (f *fakeAudit) RecordAccessCreated(_ context.Context, _, _, actorUserID, grantID, quotationID, _ string, _ int) error {
	f.calls = append(f.calls, auditCall{kind: "access_created", actorUserID: actorUserID, grantID: grantID, quotationID: quotationID})
	return nil
}
func (f *fakeAudit) RecordAccessRevoked(_ context.Context, _, _, actorUserID, grantID, quotationID, _, reason string, _ int) error {
	f.calls = append(f.calls, auditCall{kind: "access_revoked", actorUserID: actorUserID, grantID: grantID, quotationID: quotationID, reason: reason})
	return nil
}
func (f *fakeAudit) RecordClientViewedQuotation(_ context.Context, _, _, grantID, quotationID, _ string, _ int) error {
	f.calls = append(f.calls, auditCall{kind: "client_viewed", grantID: grantID, quotationID: quotationID})
	return nil
}
func (f *fakeAudit) RecordClientDecision(_ context.Context, _, _, grantID, _, quotationID, _ string, _ int, decisionStatus, _, _ string) error {
	f.calls = append(f.calls, auditCall{kind: "client_decision", grantID: grantID, quotationID: quotationID, decisionStatus: decisionStatus})
	return nil
}

func (f *fakeAudit) countOf(kind string) int {
	n := 0
	for _, c := range f.calls {
		if c.kind == kind {
			n++
		}
	}
	return n
}

type fakeCleanupLogger struct{ failures []string }

func (f *fakeCleanupLogger) LogCleanupFailure(_ context.Context, operation, _, grantID string, _ error) {
	f.failures = append(f.failures, operation+":"+grantID)
}

// --- test harness ---

type harness struct {
	svc        *access.Service
	grants     *access.MongoAccessGrantRepository
	groups     *access.MongoAccessGroupStateRepository
	quotations *fakeQuotationSource
	projects   *fakeProjectStatus
	decisions  *fakeDecisions
	audit      *fakeAudit
	cleanup    *fakeCleanupLogger
}

func snapshotFor(quotationID, quotationNumber string, version int, validUntil *time.Time) access.ShareableQuotationSnapshot {
	return access.ShareableQuotationSnapshot{
		QuotationID: quotationID, CompanyID: "company_a", ProjectID: "project_1",
		ClientID: "client_1", QuotationNumber: quotationNumber, Version: version,
		Status: "finalized", Currency: "MYR",
		Lines: []access.ShareableQuotationLine{
			{Description: "Wall Tiles", Amount: money.New(50000, "MYR")},
		},
		Subtotal: money.New(50000, "MYR"), TaxMode: "none",
		TaxAmount: money.New(0, "MYR"), Total: money.New(50000, "MYR"),
		ValidUntil: validUntil,
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := setupMongoDB(t)
	grants, groups := newRepos(t, db)

	quotationSource := &fakeQuotationSource{bySnapshotID: map[string]access.ShareableQuotationSnapshot{}}
	projectStatus := &fakeProjectStatus{}
	decisions := newFakeDecisions()
	auditRecorder := &fakeAudit{}
	cleanup := &fakeCleanupLogger{}

	svc := access.NewService(grants, groups, quotationSource, projectStatus, decisions,
		fakeCompanies{}, auditRecorder, cleanup, "http://localhost:8080")

	return &harness{svc: svc, grants: grants, groups: groups, quotations: quotationSource,
		projects: projectStatus, decisions: decisions, audit: auditRecorder, cleanup: cleanup}
}

func (h *harness) withQuotation(s access.ShareableQuotationSnapshot) *harness {
	h.quotations.bySnapshotID[s.QuotationID] = s
	return h
}

// --- Share ---

func TestShareCreatesCoordinatorAndGrantWithToken(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))

	view, created, err := h.svc.ShareQuotation(context.Background(), "company_a", "quotation_1", "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("expected the first share to report created=true (201)")
	}
	if view.RawToken == "" || view.URL == "" {
		t.Fatal("expected a raw token and URL on the first share")
	}
	if view.EffectiveStatus != access.EffectiveStatusActive {
		t.Fatalf("expected an active grant, got %q", view.EffectiveStatus)
	}
	// Default expiry when the Quotation carries no ValidUntil.
	if d := time.Until(view.ExpiresAt); d < 29*24*time.Hour || d > 31*24*time.Hour {
		t.Fatalf("expected ~30 days default validity, got %v", d)
	}
	if h.audit.countOf("quotation_sent") != 1 || h.audit.countOf("access_created") != 1 {
		t.Fatalf("expected Quotation Sent + Access Created, got %+v", h.audit.calls)
	}
	if h.projects.sentCalls != 1 {
		t.Fatalf("expected the project projection to advance once, got %d", h.projects.sentCalls)
	}
}

func TestRepeatShareIsIdempotentWithoutTokenOrURL(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	first, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, created, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("expected a repeat share to report created=false (200)")
	}
	if second.RawToken != "" || second.URL != "" {
		t.Fatal("expected NO token and NO url on a repeat share")
	}
	if second.GrantID != first.GrantID {
		t.Fatal("expected the same grant to be returned")
	}
	if h.audit.countOf("quotation_sent") != 1 {
		t.Fatalf("expected no duplicate Quotation Sent event, got %d", h.audit.countOf("quotation_sent"))
	}
	// The projection is retried on every repeat share so a prior failure heals.
	if h.projects.sentCalls != 2 {
		t.Fatalf("expected the projection retried on repeat share, got %d calls", h.projects.sentCalls)
	}
}

func TestShareRejectsDraftQuotation(t *testing.T) {
	draft := snapshotFor("quotation_1", "QT-000001", 1, nil)
	draft.Status = "draft"
	h := newHarness(t).withQuotation(draft)

	if _, _, err := h.svc.ShareQuotation(context.Background(), "company_a", "quotation_1", "user_1"); err != access.ErrQuotationNotFinalized {
		t.Fatalf("expected ErrQuotationNotFinalized, got %v", err)
	}
}

func TestShareRejectsCommerciallyExpiredQuotation(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, &past))

	if _, _, err := h.svc.ShareQuotation(context.Background(), "company_a", "quotation_1", "user_1"); err != access.ErrQuotationExpiredForShare {
		t.Fatalf("expected ErrQuotationExpiredForShare, got %v", err)
	}
}

func TestShareUsesQuotationValidUntilAsExpiry(t *testing.T) {
	future := time.Now().Add(10 * 24 * time.Hour)
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, &future))

	view, _, err := h.svc.ShareQuotation(context.Background(), "company_a", "quotation_1", "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.ExpiresAt.Unix() != future.Unix() {
		t.Fatalf("expected ExpiresAt to equal ValidUntil, got %v want %v", view.ExpiresAt, future)
	}
}

func TestShareCrossTenantIsNotFound(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))

	if _, _, err := h.svc.ShareQuotation(context.Background(), "company_b", "quotation_1", "user_1"); err != access.ErrQuotationNotFound {
		t.Fatalf("expected ErrQuotationNotFound cross-tenant, got %v", err)
	}
}

// --- Supersession ---

func TestSharingV2SupersedesV1(t *testing.T) {
	h := newHarness(t).
		withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil)).
		withQuotation(snapshotFor("quotation_2", "QT-000001", 2, nil))
	ctx := context.Background()

	v1, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	v2, created, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_2", "user_1")
	if err != nil {
		t.Fatalf("unexpected error sharing V2: %v", err)
	}
	if !created || v2.RawToken == "" {
		t.Fatal("expected sharing V2 to mint a new grant")
	}

	// V1's grant is now revoked=superseded and its view reads superseded.
	v1Grant, err := h.grants.FindByID(ctx, "company_a", v1.GrantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v1Grant.Status != access.AccessGrantStatusRevoked || v1Grant.RevokedReason != access.RevokedReasonSuperseded {
		t.Fatalf("expected V1's grant revoked as superseded, got %+v", v1Grant)
	}
	if h.audit.countOf("access_revoked") != 1 {
		t.Fatalf("expected one Access Revoked event, got %d", h.audit.countOf("access_revoked"))
	}
	// A second Quotation Sent fires for V2 (a genuinely new send).
	if h.audit.countOf("quotation_sent") != 2 {
		t.Fatalf("expected 2 Quotation Sent events, got %d", h.audit.countOf("quotation_sent"))
	}
}

func TestSupersededVersionCannotBeReshared(t *testing.T) {
	h := newHarness(t).
		withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil)).
		withQuotation(snapshotFor("quotation_2", "QT-000001", 2, nil))
	ctx := context.Background()

	if _, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_2", "user_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1"); err != access.ErrQuotationSuperseded {
		t.Fatalf("expected ErrQuotationSuperseded re-sharing V1, got %v", err)
	}
}

// --- Rotation ---

func TestRotateActiveGrantMintsReplacementAndRevokesPredecessor(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	original, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := h.svc.RotateGrant(ctx, "company_a", original.GrantID, "user_1", original.Revision)
	if err != nil {
		t.Fatalf("unexpected error rotating: %v", err)
	}
	if rotated.GrantID == original.GrantID {
		t.Fatal("expected rotation to create a NEW grant document")
	}
	if rotated.RawToken == "" || rotated.RawToken == original.RawToken {
		t.Fatal("expected a fresh raw token on rotation")
	}

	old, _ := h.grants.FindByID(ctx, "company_a", original.GrantID)
	if old.Status != access.AccessGrantStatusRevoked || old.RevokedReason != access.RevokedReasonRotated {
		t.Fatalf("expected the predecessor revoked as rotated, got %+v", old)
	}
	// Rotation is not a new "send".
	if h.audit.countOf("quotation_sent") != 1 {
		t.Fatalf("expected no additional Quotation Sent on rotation, got %d", h.audit.countOf("quotation_sent"))
	}
}

func TestRotateWithStaleRevisionIsRejected(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	original, _, _ := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")

	if _, err := h.svc.RotateGrant(ctx, "company_a", original.GrantID, "user_1", 99); err != access.ErrGrantRevisionMismatch {
		t.Fatalf("expected ErrGrantRevisionMismatch, got %v", err)
	}
}

func TestRotateSupersededGrantIsRejected(t *testing.T) {
	h := newHarness(t).
		withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil)).
		withQuotation(snapshotFor("quotation_2", "QT-000001", 2, nil))
	ctx := context.Background()

	v1, _, _ := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")
	if _, _, err := h.svc.ShareQuotation(ctx, "company_a", "quotation_2", "user_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := h.svc.RotateGrant(ctx, "company_a", v1.GrantID, "user_1", 0); err != access.ErrGrantSuperseded {
		t.Fatalf("expected ErrGrantSuperseded rotating a superseded grant, got %v", err)
	}
}

func TestRotateManuallyRevokedGrantCreatesReplacement(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	original, _, _ := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")

	if _, err := h.svc.RevokeGrant(ctx, "company_a", original.GrantID, "user_1", original.Revision); err != nil {
		t.Fatalf("unexpected error revoking: %v", err)
	}

	// Branch 2: re-share semantics — the revoked grant is NOT re-revoked.
	replacement, err := h.svc.RotateGrant(ctx, "company_a", original.GrantID, "user_1", 0)
	if err != nil {
		t.Fatalf("expected rotating a manually-revoked grant to succeed, got %v", err)
	}
	if replacement.RawToken == "" {
		t.Fatal("expected a fresh token")
	}
	if replacement.EffectiveStatus != access.EffectiveStatusActive {
		t.Fatalf("expected the replacement to be active, got %q", replacement.EffectiveStatus)
	}
}

// --- Revoke / expiry ---

func TestRevokeGrantClearsCoordinatorPointer(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	original, _, _ := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")

	view, err := h.svc.RevokeGrant(ctx, "company_a", original.GrantID, "user_1", original.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.EffectiveStatus != access.EffectiveStatusRevoked {
		t.Fatalf("expected revoked, got %q", view.EffectiveStatus)
	}
	if view.RawToken != "" || view.URL != "" {
		t.Fatal("expected no token/url on a revoke response")
	}

	coordinator, _ := h.groups.Find(ctx, "company_a", access.ResourceTypeQuotation, "quotation:QT-000001")
	if coordinator.ActiveGrantID != nil {
		t.Fatal("expected the coordinator's active grant pointer to be cleared")
	}
}

func TestUpdateGrantExpiryIsRevisionGuarded(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	original, _, _ := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")
	newExpiry := time.Now().Add(60 * 24 * time.Hour)

	if _, err := h.svc.UpdateGrantExpiry(ctx, "company_a", original.GrantID, 99, newExpiry); err != access.ErrGrantRevisionMismatch {
		t.Fatalf("expected ErrGrantRevisionMismatch, got %v", err)
	}

	updated, err := h.svc.UpdateGrantExpiry(ctx, "company_a", original.GrantID, original.Revision, newExpiry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.ExpiresAt.Unix() != newExpiry.Unix() {
		t.Fatalf("expected expiry updated, got %v", updated.ExpiresAt)
	}
	if updated.RawToken != "" {
		t.Fatal("expected no token on an expiry-edit response")
	}
}

func TestUpdateExpiryOnRevokedGrantIsRejected(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	original, _, _ := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")
	if _, err := h.svc.RevokeGrant(ctx, "company_a", original.GrantID, "user_1", original.Revision); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := h.svc.UpdateGrantExpiry(ctx, "company_a", original.GrantID, 1, time.Now().Add(time.Hour)); err != access.ErrGrantNotActive {
		t.Fatalf("expected ErrGrantNotActive, got %v", err)
	}
}

func TestGetShareStatusReturnsCurrentGrantWithoutToken(t *testing.T) {
	h := newHarness(t).withQuotation(snapshotFor("quotation_1", "QT-000001", 1, nil))
	ctx := context.Background()

	original, _, _ := h.svc.ShareQuotation(ctx, "company_a", "quotation_1", "user_1")

	status, err := h.svc.GetShareStatus(ctx, "company_a", "quotation_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.GrantID != original.GrantID {
		t.Fatal("expected the current grant")
	}
	if status.RawToken != "" || status.URL != "" {
		t.Fatal("expected GET share status to never carry a token or url")
	}
}

// errFakeApprovalWrite simulates the Approval write failing after the
// coordinator claim already succeeded (design spec §7.2's reconciliation case).
var errFakeApprovalWrite = errors.New("fake: approval write failed")
