package audit_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/audit"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestSupplierOfferAndAwardAuditEventsUseExactPrimitiveAllowlists(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	svc, repo := newService()

	if err := svc.RecordOfferSubmitted(ctx, "company-1", "supplier-1",
		"invitation-1", "offer-chain-1", "draft-1", "offer-version-1", 2, now); err != nil {
		t.Fatalf("RecordOfferSubmitted: %v", err)
	}
	assertMetadataKeys(t, repo.events[0].Metadata,
		"draftId", "invitationId", "offerChainId", "versionNumber")

	if err := svc.RecordAwardCorrected(ctx, "company-1", "user-1",
		"award-chain-1", "award-revision-2", "award-revision-1", "finalise-op-2",
		2, now); err != nil {
		t.Fatalf("RecordAwardCorrected: %v", err)
	}
	corrected := repo.events[1]
	assertMetadataKeys(t, corrected.Metadata, "awardChainId", "revisionNumber",
		"supersededRevisionId")
	if corrected.DedupeIdentity != "finalise-op-2" {
		t.Fatalf("corrected dedupe = %q", corrected.DedupeIdentity)
	}

	if err := svc.RecordAwardOutcomeAcknowledged(ctx, "company-1", "outcome-1",
		"supplier-1", "invitation-1", "session-1", now); err != nil {
		t.Fatalf("RecordAwardOutcomeAcknowledged: %v", err)
	}
	acknowledged := repo.events[2]
	assertMetadataKeys(t, acknowledged.Metadata, "invitationId", "sessionId", "supplierId")
}

func TestEnsuredAuditBSONUsesExactTopLevelAllowlist(t *testing.T) {
	db := setupMongoDB(t)
	repo := audit.NewMongoEventRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	if err := audit.NewService(repo).RecordAwardFinalised(context.Background(),
		"company-1", "user-1", "award-chain-1", "award-revision-1",
		"finalise-op-1", 1, time.Now().UTC()); err != nil {
		t.Fatalf("RecordAwardFinalised: %v", err)
	}
	var document bson.M
	if err := db.Collection("audit_events").FindOne(context.Background(), bson.M{}).
		Decode(&document); err != nil {
		t.Fatalf("read raw audit BSON: %v", err)
	}
	assertMetadataKeys(t, document, "_id", "actorId", "actorType", "companyId",
		"createdAt", "dedupeIdentity", "eventType", "metadata", "projectId",
		"schemaVersion", "subjectId", "subjectType")
}

func TestMongoAuditEnsureOnceConvergesConcurrentAwardFinalisation(t *testing.T) {
	repo := newMongoRepo(t)
	svc := audit.NewService(repo)
	ctx := context.Background()
	now := time.Now().UTC()

	errorsOut := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			errorsOut <- svc.RecordAwardFinalised(ctx, "company-1", "user-1",
				"award-chain-1", "award-revision-1", "finalise-op-1", 1, now)
		}()
	}
	for index := 0; index < 2; index++ {
		if err := <-errorsOut; err != nil {
			t.Fatalf("RecordAwardFinalised: %v", err)
		}
	}
	events, err := repo.ListBySubject(ctx, "company-1", audit.SubjectTypeAwardRevision,
		"award-revision-1")
	if err != nil {
		t.Fatalf("ListBySubject: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ensured finalisation events = %d, want exactly one", len(events))
	}
}

func assertMetadataKeys(t *testing.T, metadata map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(metadata))
	for key := range metadata {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("metadata keys = %v, want %v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("metadata keys = %v, want %v", got, want)
		}
	}
}
