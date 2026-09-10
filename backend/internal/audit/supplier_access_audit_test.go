package audit_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/audit"
)

func TestSupplierAccessAuditEventsUseSettledSubjectsActorsAndPrimitiveMetadata(
	t *testing.T) {

	at := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		eventType   string
		subjectType string
		subjectID   string
		record      func(*audit.Service) error
	}{
		{
			name:        "challenge requested",
			eventType:   audit.EventTypeSupplierChallengeRequested,
			subjectType: audit.SubjectTypeSupplierInvitation,
			subjectID:   "invitation-1",
			record: func(service *audit.Service) error {
				return service.RecordChallengeRequested(
					context.Background(), "company-1", "supplier-1",
					"invitation-1", "challenge-1", 4, at)
			},
		},
		{
			name:        "challenge delivery retried",
			eventType:   audit.EventTypeSupplierChallengeDeliveryRetried,
			subjectType: audit.SubjectTypeSupplierInvitation,
			subjectID:   "invitation-1",
			record: func(service *audit.Service) error {
				return service.RecordChallengeDeliveryRetried(
					context.Background(), "company-1", "supplier-1",
					"invitation-1", "challenge-1", "attempt-1", 4, at)
			},
		},
		{
			name:        "verification failed",
			eventType:   audit.EventTypeSupplierVerificationFailed,
			subjectType: audit.SubjectTypeSupplierInvitation,
			subjectID:   "invitation-1",
			record: func(service *audit.Service) error {
				return service.RecordVerificationFailed(
					context.Background(), "company-1", "supplier-1",
					"invitation-1", "challenge-1", 4, "invalid_code", at)
			},
		},
		{
			name:        "verification succeeded",
			eventType:   audit.EventTypeSupplierVerificationSucceeded,
			subjectType: audit.SubjectTypeSupplierInvitation,
			subjectID:   "invitation-1",
			record: func(service *audit.Service) error {
				return service.RecordVerificationSucceeded(
					context.Background(), "company-1", "supplier-1",
					"invitation-1", "challenge-1", "session-1", 4, at)
			},
		},
		{
			name:        "session created",
			eventType:   audit.EventTypeSupplierSessionCreated,
			subjectType: audit.SubjectTypeSupplierSession,
			subjectID:   "session-1",
			record: func(service *audit.Service) error {
				return service.RecordSessionCreated(
					context.Background(), "company-1", "supplier-1",
					"invitation-1", "challenge-1", "session-1", 4, 1, at)
			},
		},
		{
			name:        "session reverified",
			eventType:   audit.EventTypeSupplierSessionReverified,
			subjectType: audit.SubjectTypeSupplierSession,
			subjectID:   "session-1",
			record: func(service *audit.Service) error {
				return service.RecordSessionReverified(
					context.Background(), "company-1", "supplier-1",
					"invitation-1", "challenge-1", "session-1", 4, 2, at)
			},
		},
		{
			name:        "session renewed",
			eventType:   audit.EventTypeSupplierSessionRenewed,
			subjectType: audit.SubjectTypeSupplierSession,
			subjectID:   "session-1",
			record: func(service *audit.Service) error {
				return service.RecordSessionRenewed(
					context.Background(), "company-1", "supplier-1",
					"session-1", 2, at.Add(30*24*time.Hour), at)
			},
		},
		{
			name:        "session revoked",
			eventType:   audit.EventTypeSupplierSessionRevoked,
			subjectType: audit.SubjectTypeSupplierSession,
			subjectID:   "session-1",
			record: func(service *audit.Service) error {
				return service.RecordSessionRevoked(
					context.Background(), "company-1", "supplier-1",
					"session-1", 2, at)
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service, repository := newService()
			if err := testCase.record(service); err != nil {
				t.Fatalf("recording event: %v", err)
			}
			event := singleEvent(t, repository)
			if event.EventType != testCase.eventType ||
				event.SubjectType != testCase.subjectType ||
				event.SubjectID != testCase.subjectID {
				t.Fatalf("event identity = %+v", event)
			}
			if event.CompanyID != "company-1" ||
				event.ActorType != audit.ActorTypeSupplier ||
				event.ActorID != "supplier-1" {
				t.Fatalf("tenant/actor identity = %+v", event)
			}

			serialized, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("serializing event: %v", err)
			}
			for _, forbidden := range []string{
				"recipient@supplier.test", "raw-token", "verification-code",
				"csrf-value", "smtp", "provider-error",
			} {
				if strings.Contains(string(serialized), forbidden) {
					t.Fatalf("audit event contains forbidden value %q: %s",
						forbidden, serialized)
				}
			}
		})
	}
}
