package approvals_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/approvals"
)

// fakeApprovalRepository is a map-backed stand-in that reproduces the two
// behaviours the service depends on: uniqueness per subject (the
// uq_approvals_subject collision), and Revision-guarded conditional update.
type fakeApprovalRepository struct {
	bySubject map[string]approvals.Approval
	next      int
}

func newFakeRepo() *fakeApprovalRepository {
	return &fakeApprovalRepository{bySubject: map[string]approvals.Approval{}}
}

func subjectKey(companyID, subjectType, subjectID string) string {
	return companyID + "|" + subjectType + "|" + subjectID
}

func (f *fakeApprovalRepository) Create(ctx context.Context, a approvals.Approval) (approvals.Approval, error) {
	k := subjectKey(a.CompanyID, a.SubjectType, a.SubjectID)
	if _, exists := f.bySubject[k]; exists {
		return approvals.Approval{}, approvals.ErrApprovalAlreadyExists
	}
	f.next++
	a.ID = "approval_" + string(rune('0'+f.next))
	f.bySubject[k] = a
	return a, nil
}

func (f *fakeApprovalRepository) FindBySubject(ctx context.Context, companyID, subjectType, subjectID string) (approvals.Approval, error) {
	a, ok := f.bySubject[subjectKey(companyID, subjectType, subjectID)]
	if !ok {
		return approvals.Approval{}, approvals.ErrApprovalNotFound
	}
	return a, nil
}

func (f *fakeApprovalRepository) UpdateDecision(ctx context.Context, companyID, subjectType, subjectID string, expectedRevision int64, updated approvals.Approval) (approvals.Approval, error) {
	k := subjectKey(companyID, subjectType, subjectID)
	existing, ok := f.bySubject[k]
	if !ok {
		return approvals.Approval{}, approvals.ErrApprovalNotFound
	}
	if existing.Revision != expectedRevision {
		return approvals.Approval{}, approvals.ErrApprovalRevisionMismatch
	}
	updated.ID = existing.ID
	updated.Revision = existing.Revision + 1
	f.bySubject[k] = updated
	return updated, nil
}

func newService() (*approvals.Service, *fakeApprovalRepository) {
	repo := newFakeRepo()
	return approvals.NewService(repo), repo
}

func TestRecordDecisionCreatesFirstApproval(t *testing.T) {
	svc, _ := newService()

	a, isNew, err := svc.RecordDecision(context.Background(), approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		ActorName: "Ahmad", ActorEmail: "a@example.com", Comment: "Looks good",
		Status: approvals.ApprovalStatusAccepted, AccessGrantID: "grant_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isNew {
		t.Fatal("expected the first decision to report isNewDecision=true")
	}
	if a.Status != approvals.ApprovalStatusAccepted || a.Revision != 0 {
		t.Fatalf("expected an accepted approval at revision 0, got %+v", a)
	}
	if a.ActorType != approvals.ActorTypeClient {
		t.Fatalf("expected ActorType client, got %q", a.ActorType)
	}
}

func TestRecordDecisionTransitionsNonTerminalStates(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	base := approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		AccessGrantID: "grant_1",
	}

	rejected := base
	rejected.Status = approvals.ApprovalStatusRejected
	rejected.Comment = "Too expensive"
	if _, _, err := svc.RecordDecision(ctx, rejected); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// rejected -> changes_requested is allowed.
	changes := base
	changes.Status = approvals.ApprovalStatusChangesRequested
	changes.Comment = "Please revise tiling"
	a, isNew, err := svc.RecordDecision(ctx, changes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if isNew {
		t.Fatal("expected a transition to report isNewDecision=false")
	}
	if a.Status != approvals.ApprovalStatusChangesRequested {
		t.Fatalf("expected changes_requested, got %q", a.Status)
	}
	if a.Revision != 1 {
		t.Fatalf("expected revision incremented to 1, got %d", a.Revision)
	}

	// changes_requested -> accepted is allowed.
	accepted := base
	accepted.Status = approvals.ApprovalStatusAccepted
	if a, _, err = svc.RecordDecision(ctx, accepted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Status != approvals.ApprovalStatusAccepted {
		t.Fatalf("expected accepted, got %q", a.Status)
	}
}

func TestRecordDecisionRepeatAcceptIsIdempotent(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	in := approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		Status: approvals.ApprovalStatusAccepted, AccessGrantID: "grant_1",
	}

	first, _, err := svc.RecordDecision(ctx, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, isNew, err := svc.RecordDecision(ctx, in)
	if err != nil {
		t.Fatalf("expected a repeat accept to be idempotent, got %v", err)
	}
	if isNew {
		t.Fatal("expected a repeat accept to report isNewDecision=false")
	}
	if second.Revision != first.Revision {
		t.Fatalf("expected NO write on a repeat accept (revision unchanged), got %d then %d", first.Revision, second.Revision)
	}
}

func TestRecordDecisionRejectAfterAcceptIsRefused(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	base := approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		AccessGrantID: "grant_1",
	}

	accepted := base
	accepted.Status = approvals.ApprovalStatusAccepted
	if _, _, err := svc.RecordDecision(ctx, accepted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, status := range []approvals.ApprovalStatus{approvals.ApprovalStatusRejected, approvals.ApprovalStatusChangesRequested} {
		conflicting := base
		conflicting.Status = status
		conflicting.Comment = "changed my mind"
		if _, _, err := svc.RecordDecision(ctx, conflicting); err != approvals.ErrDecisionAlreadyAccepted {
			t.Fatalf("expected ErrDecisionAlreadyAccepted for %q after acceptance, got %v", status, err)
		}
	}
}

func TestRecordDecisionRejectsInvalidStatus(t *testing.T) {
	svc, _ := newService()
	_, _, err := svc.RecordDecision(context.Background(), approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", Status: approvals.ApprovalStatus("maybe"),
	})
	if err != approvals.ErrInvalidApprovalStatus {
		t.Fatalf("expected ErrInvalidApprovalStatus, got %v", err)
	}
}

// ReconcileAccepted exists for the design spec §7.2 case: the coordinator
// claim succeeded but the Approval write failed, so a later repeat accept
// must bring the Approval record into line with the already-authoritative
// coordinator fact.
func TestReconcileAcceptedCreatesMissingApproval(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()

	a, err := svc.ReconcileAccepted(ctx, approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		Status: approvals.ApprovalStatusAccepted, AccessGrantID: "grant_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Status != approvals.ApprovalStatusAccepted {
		t.Fatalf("expected reconciliation to create an accepted approval, got %q", a.Status)
	}
	if _, err := repo.FindBySubject(ctx, "company_a", approvals.SubjectTypeQuotation, "quotation_1"); err != nil {
		t.Fatalf("expected the approval to be persisted, got %v", err)
	}
}

func TestReconcileAcceptedUpgradesStaleApproval(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	base := approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		AccessGrantID: "grant_1",
	}

	// A stale non-terminal decision exists (e.g. the accept's Approval write
	// failed after the coordinator already recorded acceptance).
	stale := base
	stale.Status = approvals.ApprovalStatusRejected
	if _, _, err := svc.RecordDecision(ctx, stale); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reconciled, err := svc.ReconcileAccepted(ctx, approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		Status: approvals.ApprovalStatusAccepted, AccessGrantID: "grant_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reconciled.Status != approvals.ApprovalStatusAccepted {
		t.Fatalf("expected the stale approval to be upgraded to accepted, got %q", reconciled.Status)
	}
}

func TestGetDecisionIsTenantScoped(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	if _, _, err := svc.RecordDecision(ctx, approvals.RecordDecisionInput{
		CompanyID: "company_a", SubjectType: approvals.SubjectTypeQuotation,
		SubjectID: "quotation_1", SubjectGroupKey: "quotation:QT-000001",
		Status: approvals.ApprovalStatusAccepted, AccessGrantID: "grant_1",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, found, err := svc.GetDecision(ctx, "company_b", approvals.SubjectTypeQuotation, "quotation_1"); err != nil || found {
		t.Fatalf("expected company_b to find nothing, got found=%v err=%v", found, err)
	}

	got, found, err := svc.GetDecision(ctx, "company_a", approvals.SubjectTypeQuotation, "quotation_1")
	if err != nil || !found {
		t.Fatalf("expected company_a to find the approval, got found=%v err=%v", found, err)
	}
	if got.Status != approvals.ApprovalStatusAccepted {
		t.Fatalf("expected accepted, got %q", got.Status)
	}
	if got.DecidedAt.After(time.Now().Add(time.Minute)) {
		t.Fatal("expected a sane DecidedAt timestamp")
	}
}
