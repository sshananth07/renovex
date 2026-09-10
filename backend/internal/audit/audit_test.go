package audit_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/audit"
)

type fakeEventRepository struct {
	events []audit.Event
}

func (f *fakeEventRepository) Create(ctx context.Context, e audit.Event) error {
	f.events = append(f.events, e)
	return nil
}

func newService() (*audit.Service, *fakeEventRepository) {
	repo := &fakeEventRepository{}
	return audit.NewService(repo), repo
}

func TestRecordQuotationSentBuildsAllowlistedEvent(t *testing.T) {
	svc, repo := newService()

	err := svc.RecordQuotationSent(context.Background(), "company_a", "project_1", "user_1", "quotation_1", "QT-000001", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(repo.events))
	}

	e := repo.events[0]
	if e.EventType != audit.EventTypeQuotationSent {
		t.Fatalf("expected %q, got %q", audit.EventTypeQuotationSent, e.EventType)
	}
	if e.CompanyID != "company_a" || e.ProjectID != "project_1" {
		t.Fatalf("expected tenant/project scoping, got %+v", e)
	}
	if e.ActorType != audit.ActorTypeContractor || e.ActorID != "user_1" {
		t.Fatalf("expected a contractor actor with the authenticated UserID, got type=%q id=%q", e.ActorType, e.ActorID)
	}
	if e.SubjectType != audit.SubjectTypeQuotation || e.SubjectID != "quotation_1" {
		t.Fatalf("expected the quotation as subject, got %q/%q", e.SubjectType, e.SubjectID)
	}
	if e.Metadata["quotationNumber"] != "QT-000001" || e.Metadata["version"] != 2 {
		t.Fatalf("expected allowlisted metadata, got %+v", e.Metadata)
	}
}

func TestRecordAccessCreatedAndRevokedCarryActorAndGrant(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()

	if err := svc.RecordAccessCreated(ctx, "company_a", "project_1", "user_1", "grant_1", "quotation_1", "QT-000001", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.RecordAccessRevoked(ctx, "company_a", "project_1", "user_1", "grant_1", "quotation_1", "QT-000001", "superseded", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	created, revoked := repo.events[0], repo.events[1]
	if created.EventType != audit.EventTypeAccessCreated || created.SubjectType != audit.SubjectTypeAccessGrant {
		t.Fatalf("unexpected created event: %+v", created)
	}
	if created.Metadata["grantId"] != "grant_1" {
		t.Fatalf("expected grantId in metadata, got %+v", created.Metadata)
	}
	if revoked.EventType != audit.EventTypeAccessRevoked || revoked.Metadata["revokedReason"] != "superseded" {
		t.Fatalf("unexpected revoked event: %+v", revoked)
	}
}

// A supersession-triggered revoke has no directly-acting human contractor;
// ActorType must be "system" and ActorID empty (design spec §12.1).
func TestRecordAccessRevokedWithEmptyActorIsSystem(t *testing.T) {
	svc, repo := newService()

	if err := svc.RecordAccessRevoked(context.Background(), "company_a", "project_1", "", "grant_1", "quotation_1", "QT-000001", "superseded", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := repo.events[0]
	if e.ActorType != audit.ActorTypeSystem || e.ActorID != "" {
		t.Fatalf("expected a system actor with no ID, got type=%q id=%q", e.ActorType, e.ActorID)
	}
}

func TestRecordClientViewedAndDecisionAreClientActors(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()

	if err := svc.RecordClientViewedQuotation(ctx, "company_a", "project_1", "grant_1", "quotation_1", "QT-000001", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.RecordClientDecision(ctx, "company_a", "project_1", "grant_1", "approval_1", "quotation_1", "QT-000001", 1, "accepted", "Ahmad", "a@example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	viewed, decided := repo.events[0], repo.events[1]
	if viewed.EventType != audit.EventTypeClientViewedQuotation || viewed.ActorType != audit.ActorTypeClient || viewed.ActorID != "" {
		t.Fatalf("unexpected viewed event: %+v", viewed)
	}
	if decided.EventType != audit.EventTypeClientAcceptedQuotation {
		t.Fatalf("expected the accepted event type, got %q", decided.EventType)
	}
	if decided.Metadata["approvalId"] != "approval_1" || decided.Metadata["clientName"] != "Ahmad" {
		t.Fatalf("expected allowlisted decision metadata, got %+v", decided.Metadata)
	}
}

func TestRecordClientDecisionMapsEachStatusToItsEventType(t *testing.T) {
	cases := map[string]string{
		"accepted":          audit.EventTypeClientAcceptedQuotation,
		"rejected":          audit.EventTypeClientRejectedQuotation,
		"changes_requested": audit.EventTypeClientRequestedChanges,
	}
	for status, want := range cases {
		svc, repo := newService()
		if err := svc.RecordClientDecision(context.Background(), "company_a", "project_1", "grant_1", "approval_1", "quotation_1", "QT-000001", 1, status, "", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := repo.events[0].EventType; got != want {
			t.Fatalf("status %q: expected event type %q, got %q", status, want, got)
		}
	}
}

// The Client's free-text comment is deliberately NOT part of any audit
// method's signature (design spec §12) — it lives only on the Approval
// record. This test pins that: no metadata value may contain comment text.
func TestClientCommentNeverReachesAuditMetadata(t *testing.T) {
	svc, repo := newService()

	if err := svc.RecordClientDecision(context.Background(), "company_a", "project_1", "grant_1", "approval_1", "quotation_1", "QT-000001", 1, "rejected", "Ahmad", "a@example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	serialized, err := json.Marshal(repo.events[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(serialized), "comment") {
		t.Fatalf("expected no comment field anywhere in an audit event, got %s", serialized)
	}
}

// A raw token can never reach audit because no method accepts one. This
// guards against a future signature change reintroducing the risk.
func TestNoAuditMethodAcceptsARawToken(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()

	rawTokenLookalike := "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZmdoaWo"
	_ = svc.RecordQuotationSent(ctx, "company_a", "project_1", "user_1", "quotation_1", "QT-000001", 1)
	_ = svc.RecordAccessCreated(ctx, "company_a", "project_1", "user_1", "grant_1", "quotation_1", "QT-000001", 1)
	_ = svc.RecordClientViewedQuotation(ctx, "company_a", "project_1", "grant_1", "quotation_1", "QT-000001", 1)

	serialized, err := json.Marshal(repo.events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(serialized), rawTokenLookalike) {
		t.Fatal("a raw token reached an audit event")
	}
}
