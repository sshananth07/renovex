package rfqissuance_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Delivery attempt persistence (design spec §3.5, §10.2, §11.2).
//
// Real MongoDB: send idempotency IS a unique index. A fake would assert the
// test's own bookkeeping rather than the constraint that actually prevents a
// Supplier receiving two copies of the same invitation.

func newDeliveryRepo(t *testing.T, db *mongo.Database) *rfqissuance.MongoDeliveryAttemptRepository {
	t.Helper()
	repo := rfqissuance.NewMongoDeliveryAttemptRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure indexes: %v", err)
	}
	return repo
}

func deliveryFixture(companyID, invitationID, operationID string) rfqissuance.InvitationDeliveryAttempt {
	return rfqissuance.NewDeliveryAttempt(rfqissuance.NewDeliveryAttemptInput{
		CompanyID: companyID, InvitationID: invitationID,
		DeliveryOperationID: operationID,
		AccessGeneration:    2,
		Channel:             rfqissuance.DeliveryChannelEmail,
		RecipientEmail:      "sales@supplier.com",
		IssuedRFQVersionID:  "version-1",
		RequestedByUserID:   "user-1",
	})
}

func TestCreateAttemptRoundTripsEveryField(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateAttempt(ctx, deliveryFixture("company-1", "invitation-1", "op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("the created attempt must carry its persisted ID")
	}

	found, ok, err := repo.FindAttemptByOperationID(ctx, "company-1", "invitation-1", "op-1")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if !ok {
		t.Fatal("the attempt must be resolvable by its operation ID")
	}

	if found.Status != rfqissuance.DeliveryStatusPending {
		t.Errorf("Status = %q, want pending", found.Status)
	}
	if found.Channel != rfqissuance.DeliveryChannelEmail {
		t.Errorf("Channel = %q, want email", found.Channel)
	}
	if found.AccessGeneration != 2 {
		t.Errorf("AccessGeneration = %d, want the snapshotted 2", found.AccessGeneration)
	}
	if found.RecipientEmail != "sales@supplier.com" {
		t.Errorf("RecipientEmail = %q, want the snapshot", found.RecipientEmail)
	}
	if found.IssuedRFQVersionID != "version-1" {
		t.Errorf("IssuedRFQVersionID = %q", found.IssuedRFQVersionID)
	}
}

// Send idempotency (§10.2, §11.2): the same operation ID must not create a
// second attempt, which is what stops a retry emailing the Supplier twice.
func TestCreateAttemptRejectsAReusedOperationID(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateAttempt(ctx,
		deliveryFixture("company-1", "invitation-1", "op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.CreateAttempt(ctx, deliveryFixture("company-1", "invitation-1", "op-1"))

	if !errors.Is(err, rfqissuance.ErrDeliveryOperationAlreadyUsed) {
		t.Errorf("error = %v, want ErrDeliveryOperationAlreadyUsed", err)
	}
}

// The index is scoped per invitation and company: two invitations may
// legitimately carry the same caller-generated operation ID.
func TestOperationIDUniquenessIsScopedPerCompanyAndInvitation(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateAttempt(ctx,
		deliveryFixture("company-1", "invitation-1", "op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := repo.CreateAttempt(ctx,
		deliveryFixture("company-1", "invitation-2", "op-1")); err != nil {
		t.Errorf("another invitation's identical operation ID must be permitted, got %v", err)
	}
	if _, err := repo.CreateAttempt(ctx,
		deliveryFixture("company-2", "invitation-1", "op-1")); err != nil {
		t.Errorf("another company's identical operation ID must be permitted, got %v", err)
	}
}

func TestFindAttemptByOperationIDIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	if _, err := repo.CreateAttempt(ctx,
		deliveryFixture("company-1", "invitation-1", "op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok, err := repo.FindAttemptByOperationID(ctx, "company-2", "invitation-1",
		"op-1"); err != nil || ok {
		t.Errorf("a foreign company resolved the attempt: ok=%v err=%v", ok, err)
	}
}

// The status transition after a synchronous send (§1A.2).
func TestUpdateAttemptStatusRecordsSentAndFailed(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateAttempt(ctx, deliveryFixture("company-1", "invitation-1", "op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sentAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.UpdateAttemptStatus(ctx, "company-1", created.ID,
		rfqissuance.DeliveryStatusSent, "", &sentAt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, _, err := repo.FindAttemptByOperationID(ctx, "company-1", "invitation-1", "op-1")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if found.Status != rfqissuance.DeliveryStatusSent {
		t.Errorf("Status = %q, want sent", found.Status)
	}
	if found.SentAt == nil || !found.SentAt.Equal(sentAt) {
		t.Errorf("SentAt = %v, want %v", found.SentAt, sentAt)
	}

	// And the failure path, with a bounded code.
	failed, err := repo.CreateAttempt(ctx, deliveryFixture("company-1", "invitation-1", "op-2"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repo.UpdateAttemptStatus(ctx, "company-1", failed.ID,
		rfqissuance.DeliveryStatusFailed,
		rfqissuance.DeliveryFailureCodeSendFailed, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundFailed, _, err := repo.FindAttemptByOperationID(ctx, "company-1", "invitation-1", "op-2")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if foundFailed.Status != rfqissuance.DeliveryStatusFailed {
		t.Errorf("Status = %q, want failed", foundFailed.Status)
	}
	if foundFailed.FailureCode != rfqissuance.DeliveryFailureCodeSendFailed {
		t.Errorf("FailureCode = %q", foundFailed.FailureCode)
	}
	if foundFailed.SentAt != nil {
		t.Error("a failed attempt must not carry a SentAt: it was never sent")
	}
}

func TestUpdateAttemptStatusIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateAttempt(ctx, deliveryFixture("company-1", "invitation-1", "op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = repo.UpdateAttemptStatus(ctx, "company-2", created.ID,
		rfqissuance.DeliveryStatusSent, "", nil)

	if !errors.Is(err, rfqissuance.ErrDeliveryAttemptNotFound) {
		t.Errorf("error = %v, want ErrDeliveryAttemptNotFound for a foreign company", err)
	}
}

func TestListAttemptsForInvitationIsScoped(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	for _, op := range []string{"op-1", "op-2"} {
		if _, err := repo.CreateAttempt(ctx,
			deliveryFixture("company-1", "invitation-1", op)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if _, err := repo.CreateAttempt(ctx,
		deliveryFixture("company-1", "invitation-2", "op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	listed, err := repo.ListAttemptsForInvitation(ctx, "company-1", "invitation-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("got %d attempts, want the 2 for this invitation", len(listed))
	}
	for _, attempt := range listed {
		if attempt.InvitationID != "invitation-1" {
			t.Errorf("leaked an attempt for %s", attempt.InvitationID)
		}
	}
}

// Concurrent sends under ONE operation ID must produce exactly one attempt —
// the guarantee that stops a double-click emailing the Supplier twice.
func TestConcurrentCreateAttemptHasExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := newDeliveryRepo(t, db)
	ctx := context.Background()

	const concurrency = 12
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.CreateAttempt(ctx,
				deliveryFixture("company-1", "invitation-1", "op-1"))
		}(i)
	}
	wg.Wait()

	winners := 0
	for i, err := range errs {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, rfqissuance.ErrDeliveryOperationAlreadyUsed):
		default:
			t.Fatalf("goroutine %d failed unexpectedly: %v", i, err)
		}
	}

	if winners != 1 {
		t.Errorf("got %d winners, want exactly 1: a retry must never send the Supplier "+
			"a second copy (§10.2)", winners)
	}
}
