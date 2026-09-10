package rfqissuance_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

func TestObsoletePendingAttemptsBeforeGenerationPreservesHistoricalStatuses(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	seed := func(operationID string, generation int64, status rfqissuance.DeliveryStatus) {
		t.Helper()
		attempt := deliveryFixture("company-1", "invitation-1", operationID)
		attempt.AccessGeneration = generation
		created, err := repo.CreateAttempt(ctx, attempt)
		if err != nil {
			t.Fatalf("create %s: %v", operationID, err)
		}
		if status != rfqissuance.DeliveryStatusPending {
			if err := repo.UpdateAttemptStatus(ctx, "company-1", created.ID,
				status, "", nil); err != nil {
				t.Fatalf("set %s status: %v", operationID, err)
			}
		}
	}

	seed("pending-old", 1, rfqissuance.DeliveryStatusPending)
	seed("pending-current", 2, rfqissuance.DeliveryStatusPending)
	seed("sent-old", 1, rfqissuance.DeliveryStatusSent)
	seed("failed-old", 1, rfqissuance.DeliveryStatusFailed)
	seed("obsolete-old", 1, rfqissuance.DeliveryStatusObsolete)

	changed, err := repo.ObsoletePendingAttemptsBeforeGeneration(ctx,
		"company-1", "invitation-1", 2)
	if err != nil {
		t.Fatalf("obsolete pending attempts: %v", err)
	}
	if changed != 1 {
		t.Errorf("changed = %d, want only the one older pending attempt", changed)
	}

	attempts, err := repo.ListAttemptsForInvitation(ctx, "company-1", "invitation-1")
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	statuses := map[string]rfqissuance.DeliveryStatus{}
	for _, attempt := range attempts {
		statuses[attempt.DeliveryOperationID] = attempt.Status
	}
	for operationID, want := range map[string]rfqissuance.DeliveryStatus{
		"pending-old":     rfqissuance.DeliveryStatusObsolete,
		"pending-current": rfqissuance.DeliveryStatusPending,
		"sent-old":        rfqissuance.DeliveryStatusSent,
		"failed-old":      rfqissuance.DeliveryStatusFailed,
		"obsolete-old":    rfqissuance.DeliveryStatusObsolete,
	} {
		if statuses[operationID] != want {
			t.Errorf("%s status = %q, want %q", operationID, statuses[operationID], want)
		}
	}
}
