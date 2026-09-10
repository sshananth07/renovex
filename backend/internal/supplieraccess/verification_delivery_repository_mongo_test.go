package supplieraccess_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func verificationDeliveryFixture(now time.Time, id, operationID string) supplieraccess.VerificationDeliveryAttempt {
	return supplieraccess.VerificationDeliveryAttempt{
		ID:          id,
		CompanyID:   "company-1",
		ChallengeID: "challenge-1",
		OperationID: operationID,
		Status:      supplieraccess.VerificationDeliveryPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestVerificationDeliveryRepositorySerializesOperationsAndPreservesTerminalHistory(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationDeliveryRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 15, 0, 0, 0, time.UTC)
	input := verificationDeliveryFixture(now, "attempt-1", "operation-1")

	if _, err := repo.CreateAttempt(ctx, input); err != nil {
		t.Fatalf("creating delivery attempt: %v", err)
	}
	if _, err := repo.CreateAttempt(ctx,
		verificationDeliveryFixture(now, "attempt-2", "operation-1")); !errors.Is(
		err, supplieraccess.ErrVerificationDeliveryOperationAlreadyUsed) {
		t.Fatalf("duplicate operation error = %v", err)
	}

	sentAt := now.Add(time.Second)
	sent, err := repo.TransitionPending(ctx, input.CompanyID, input.ID,
		supplieraccess.VerificationDeliverySent, nil, sentAt)
	if err != nil {
		t.Fatalf("marking sent: %v", err)
	}
	if sent.Status != supplieraccess.VerificationDeliverySent ||
		!sent.UpdatedAt.Equal(sentAt) {
		t.Fatalf("sent transition = %#v", sent)
	}
	failure := supplieraccess.VerificationDeliveryFailureCode("provider_unavailable")
	if _, err := repo.TransitionPending(ctx, input.CompanyID, input.ID,
		supplieraccess.VerificationDeliveryFailed, &failure,
		now.Add(2*time.Second)); !errors.Is(
		err, supplieraccess.ErrVerificationDeliveryNotPending) {
		t.Fatalf("terminal rewrite error = %v", err)
	}
	reloaded, err := repo.FindAttemptByOperation(
		ctx, input.CompanyID, input.ChallengeID, input.OperationID)
	if err != nil {
		t.Fatalf("finding attempt: %v", err)
	}
	if reloaded.Status != supplieraccess.VerificationDeliverySent ||
		reloaded.FailureCode != nil {
		t.Fatalf("terminal historical attempt was rewritten: %#v", reloaded)
	}
}
