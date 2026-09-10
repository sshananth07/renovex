package awards_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
)

// F8 persistence (§8I). Intent is persisted BEFORE the send, and idempotency is
// per CompanyID + DeliveryOperationID so a retry cannot tell a Supplier twice.

func deliveryRepository(t *testing.T) *awards.MongoAwardDeliveryRepository {
	t.Helper()
	repository := awards.NewMongoAwardDeliveryRepository(setupDB(t))
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repository
}

func candidateDelivery(operationID string) awards.AwardOutcomeDelivery {
	return awards.AwardOutcomeDelivery{
		CompanyID: "company-1", AwardOutcomeID: "outcome-1",
		AwardRevisionID: "revision-1", SupplierID: "supplier-a",
		RecipientIdentity: "buyer@example.test", AccessGeneration: 1,
		DeliveryOperationID: operationID,
		Channel:             awards.DeliveryChannelEmail,
		Status:              awards.DeliveryPending,
	}
}

// A same-operation retry resolves the SAME record and does not re-send.
func TestEnsureDeliveryIntentIsIdempotentPerOperation(t *testing.T) {
	repository := deliveryRepository(t)
	ctx := context.Background()

	first, created, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-1"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}
	if !created {
		t.Fatal("the first intent must report creation")
	}

	second, created, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-1"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent (retry): %v", err)
	}
	if created {
		t.Fatal("a same-operation retry must NOT create a second record; " +
			"the Supplier would be told twice")
	}
	if first.ID != second.ID {
		t.Fatalf("two records exist: %s and %s", first.ID, second.ID)
	}
}

// Concurrent sends with one operation ID converge on one delivery record.
func TestConcurrentDeliveryIntentProducesOneRecord(t *testing.T) {
	repository := deliveryRepository(t)
	ctx := context.Background()

	const attempts = 8
	ids := make([]string, attempts)
	creations := make([]bool, attempts)
	errs := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			delivery, created, err := repository.EnsureDeliveryIntent(
				ctx, candidateDelivery("op-1"))
			ids[index], creations[index], errs[index] = delivery.ID, created, err
		}(index)
	}
	wait.Wait()

	unique := map[string]bool{}
	created := 0
	for index, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: %v", index, err)
		}
		unique[ids[index]] = true
		if creations[index] {
			created++
		}
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent intent produced %d records, want 1", len(unique))
	}
	// Exactly one caller may believe it should send.
	if created != 1 {
		t.Fatalf("%d callers were told they created the record, want 1", created)
	}
}

// A resolved record is never rewritten: `sent` and `failed` are historical.
func TestAResolvedDeliveryCannotBeRewritten(t *testing.T) {
	repository := deliveryRepository(t)
	ctx := context.Background()

	delivery, _, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-1"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}
	if err := repository.MarkDeliverySent(
		ctx, "company-1", delivery.ID, time.Now().UTC()); err != nil {
		t.Fatalf("MarkDeliverySent: %v", err)
	}

	if err := repository.MarkDeliveryFailed(ctx, "company-1", delivery.ID,
		awards.DeliveryFailureTransportRejected); err == nil {
		t.Fatal("a sent delivery must not be rewritten as failed")
	}
}

// Only PENDING records become obsolete when a correction publishes.
func TestObsoletingTouchesOnlyPendingDeliveries(t *testing.T) {
	repository := deliveryRepository(t)
	ctx := context.Background()

	pending, _, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-pending"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}
	sent, _, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-sent"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}
	if err := repository.MarkDeliverySent(
		ctx, "company-1", sent.ID, time.Now().UTC()); err != nil {
		t.Fatalf("MarkDeliverySent: %v", err)
	}
	failed, _, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-failed"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}
	if err := repository.MarkDeliveryFailed(ctx, "company-1", failed.ID,
		awards.DeliveryFailureInvalidRecipient); err != nil {
		t.Fatalf("MarkDeliveryFailed: %v", err)
	}

	affected, err := repository.ObsoletePendingDeliveries(
		ctx, "company-1", "revision-1")
	if err != nil {
		t.Fatalf("ObsoletePendingDeliveries: %v", err)
	}
	if len(affected) != 1 || affected[0].ID != pending.ID {
		t.Fatalf("obsoleted %+v, want only the pending record", affected)
	}

	// The historical facts survive untouched.
	stored, _, err := repository.FindDelivery(ctx, "company-1", sent.ID)
	if err != nil {
		t.Fatalf("FindDelivery: %v", err)
	}
	if stored.Status != awards.DeliverySent {
		t.Errorf("sent record became %q; a Supplier really was told",
			stored.Status)
	}
	stored, _, err = repository.FindDelivery(ctx, "company-1", failed.ID)
	if err != nil {
		t.Fatalf("FindDelivery: %v", err)
	}
	if stored.Status != awards.DeliveryFailed {
		t.Errorf("failed record became %q; the attempt really did fail",
			stored.Status)
	}
}

// A foreign tenant cannot read or resolve another company's deliveries.
func TestDeliveriesAreTenantScoped(t *testing.T) {
	repository := deliveryRepository(t)
	ctx := context.Background()

	delivery, _, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-1"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}

	if _, found, err := repository.FindDelivery(
		ctx, "company-2", delivery.ID); err != nil {
		t.Fatalf("FindDelivery: %v", err)
	} else if found {
		t.Fatal("a foreign company must not read another tenant's delivery")
	}
	if err := repository.MarkDeliverySent(
		ctx, "company-2", delivery.ID, time.Now().UTC()); err == nil {
		t.Fatal("a foreign company must not resolve another tenant's delivery")
	}
}

// A retry after failure creates a NEW record for the same outcome, so the
// history shows both attempts rather than overwriting the first.
func TestRetryCreatesANewRecordForTheSameOutcome(t *testing.T) {
	repository := deliveryRepository(t)
	ctx := context.Background()

	first, _, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-1"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}
	if err := repository.MarkDeliveryFailed(ctx, "company-1", first.ID,
		awards.DeliveryFailureTransportUnavailable); err != nil {
		t.Fatalf("MarkDeliveryFailed: %v", err)
	}

	second, created, err := repository.EnsureDeliveryIntent(
		ctx, candidateDelivery("op-2"))
	if err != nil {
		t.Fatalf("EnsureDeliveryIntent (retry): %v", err)
	}
	if !created || second.ID == first.ID {
		t.Fatal("an explicit retry must create a new delivery record")
	}

	deliveries, err := repository.ListDeliveries(ctx, "company-1", "revision-1")
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("deliveries = %d, want both attempts in the history",
			len(deliveries))
	}
}
