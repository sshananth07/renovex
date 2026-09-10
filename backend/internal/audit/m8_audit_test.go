package audit_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/audit"
)

// M8 RFQ issuance audit methods remain primitive-only. The tests assert the
// stored effect rather than the method calls so a recorder that silently drops
// an event cannot satisfy them.

func TestRecordRFQVersionIssued(t *testing.T) {
	svc, repo := newService()
	err := svc.RecordRFQVersionIssued(
		context.Background(),
		"company-1", "project-1", "user-1",
		"chain-1", "RFQ-000001", "version-1", 1,
	)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	event := singleEvent(t, repo)
	if event.EventType != audit.EventTypeRFQVersionIssued {
		t.Errorf("EventType = %q, want %q",
			event.EventType, audit.EventTypeRFQVersionIssued)
	}
	if event.CompanyID != "company-1" ||
		event.ProjectID != "project-1" ||
		event.SubjectType != audit.SubjectTypeRFQ ||
		event.SubjectID != "chain-1" ||
		event.ActorID != "user-1" {
		t.Errorf("event identity = %+v", event)
	}
	if event.Metadata["rfqNumber"] != "RFQ-000001" ||
		event.Metadata["versionId"] != "version-1" ||
		event.Metadata["versionNumber"] != 1 {
		t.Errorf("metadata = %+v", event.Metadata)
	}
}

func TestRecordRFQAmendmentDraftLifecycle(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		eventType string
		record    func(*audit.Service) error
	}{
		{
			name: "created", eventType: audit.EventTypeRFQAmendmentDraftCreated,
			record: func(s *audit.Service) error {
				return s.RecordRFQAmendmentDraftCreated(
					ctx, "company-1", "project-1", "user-1",
					"chain-1", "RFQ-000001", "draft-1", 1)
			},
		},
		{
			name: "updated", eventType: audit.EventTypeRFQAmendmentDraftUpdated,
			record: func(s *audit.Service) error {
				return s.RecordRFQAmendmentDraftUpdated(
					ctx, "company-1", "user-1", "chain-1", "draft-1", 4)
			},
		},
		{
			name: "discarded", eventType: audit.EventTypeRFQAmendmentDraftDiscarded,
			record: func(s *audit.Service) error {
				return s.RecordRFQAmendmentDraftDiscarded(
					ctx, "company-1", "user-1", "chain-1", "draft-1", 4)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newService()
			if err := tc.record(svc); err != nil {
				t.Fatalf("record: %v", err)
			}
			event := singleEvent(t, repo)
			if event.EventType != tc.eventType {
				t.Errorf("EventType = %q, want %q", event.EventType, tc.eventType)
			}
			if event.CompanyID != "company-1" ||
				event.SubjectType != audit.SubjectTypeRFQ ||
				event.SubjectID != "chain-1" ||
				event.ActorID != "user-1" {
				t.Errorf("event identity = %+v", event)
			}
			if event.Metadata["draftId"] != "draft-1" {
				t.Errorf("metadata = %+v", event.Metadata)
			}
		})
	}
}

func TestRecordRFQAmendmentIssuedAndChainReconciled(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		eventType string
		record    func(*audit.Service) error
	}{
		{
			name: "amendment issued", eventType: audit.EventTypeRFQAmendmentIssued,
			record: func(s *audit.Service) error {
				return s.RecordRFQAmendmentIssued(
					ctx, "company-1", "project-1", "user-1", "chain-1",
					"RFQ-000001", "draft-1", "version-2", 2)
			},
		},
		{
			name: "chain reconciled", eventType: audit.EventTypeRFQIssuanceChainReconciled,
			record: func(s *audit.Service) error {
				return s.RecordRFQIssuanceChainReconciled(
					ctx, "company-1", "project-1", "user-1", "chain-1",
					"RFQ-000001", "version-2", 2)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newService()
			if err := tc.record(svc); err != nil {
				t.Fatalf("record: %v", err)
			}
			event := singleEvent(t, repo)
			if event.EventType != tc.eventType {
				t.Errorf("EventType = %q, want %q", event.EventType, tc.eventType)
			}
			if event.Metadata["versionId"] != "version-2" ||
				event.Metadata["versionNumber"] != 2 {
				t.Errorf("metadata = %+v", event.Metadata)
			}
		})
	}
}
