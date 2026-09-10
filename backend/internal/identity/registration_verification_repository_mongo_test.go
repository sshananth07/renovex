package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func TestRegistrationVerificationRepositoryIssueOrResumeCreatesOneCurrentChallenge(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_regverif_issue")
	repo := identity.NewMongoRegistrationVerificationRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now()
	created, err := repo.IssueOrResume(context.Background(), "tester@example.com", identity.RegistrationVerificationPurposeRegistration, "hash_1", now)
	if err != nil {
		t.Fatalf("IssueOrResume: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected a generated ID")
	}

	found, err := repo.FindCurrent(context.Background(), "tester@example.com", identity.RegistrationVerificationPurposeRegistration)
	if err != nil {
		t.Fatalf("FindCurrent: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected the same current challenge, got a different ID")
	}
}

func TestRegistrationVerificationRepositoryIssueOrResumeReplacesTheSameDocumentPastCooldown(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_regverif_resend")
	repo := identity.NewMongoRegistrationVerificationRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now()
	first, err := repo.IssueOrResume(context.Background(), "tester@example.com", identity.RegistrationVerificationPurposeRegistration, "hash_1", now)
	if err != nil {
		t.Fatalf("first issue: %v", err)
	}

	// Within the cooldown: rate-limited.
	_, err = repo.IssueOrResume(context.Background(), "tester@example.com", identity.RegistrationVerificationPurposeRegistration, "hash_2", now.Add(time.Second))
	if !errors.Is(err, identity.ErrRegistrationVerificationRateLimited) {
		t.Fatalf("expected ErrRegistrationVerificationRateLimited within cooldown, got %v", err)
	}

	// Past the cooldown: replaces the SAME document (same ID), never a
	// second unrelated one.
	second, err := repo.IssueOrResume(context.Background(), "tester@example.com", identity.RegistrationVerificationPurposeRegistration, "hash_2", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("resend past cooldown: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same document to be updated, got a new ID %q vs %q", second.ID, first.ID)
	}
	if second.CodeHash != "hash_2" {
		t.Fatalf("expected the code hash to be replaced, got %q", second.CodeHash)
	}
}

func TestRegistrationVerificationRepositoryConsumeIsExactlyOnce(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_regverif_consume")
	repo := identity.NewMongoRegistrationVerificationRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now()
	challenge, err := repo.IssueOrResume(context.Background(), "tester@example.com", identity.RegistrationVerificationPurposeRegistration, "hash_1", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := repo.Consume(context.Background(), challenge.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	_, err = repo.Consume(context.Background(), challenge.ID, now.Add(2*time.Second))
	if !errors.Is(err, identity.ErrRegistrationVerificationInvalid) {
		t.Fatalf("expected the second consume to fail, got %v", err)
	}
}

func TestRegistrationVerificationRepositoryRecordFailedAttemptStopsAtLimit(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_regverif_attempts")
	repo := identity.NewMongoRegistrationVerificationRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	now := time.Now()
	challenge, err := repo.IssueOrResume(context.Background(), "tester@example.com", identity.RegistrationVerificationPurposeRegistration, "hash_1", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	var last identity.RegistrationVerificationChallenge
	for i := 0; i < 5; i++ {
		last, err = repo.RecordFailedAttempt(context.Background(), challenge.ID, now.Add(time.Duration(i+1)*time.Second))
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if last.FailedAttempts != 5 {
		t.Fatalf("expected FailedAttempts=5, got %d", last.FailedAttempts)
	}

	// One more attempt beyond the limit must be rejected, not silently
	// incremented past the bound.
	_, err = repo.RecordFailedAttempt(context.Background(), challenge.ID, now.Add(10*time.Second))
	if !errors.Is(err, identity.ErrRegistrationVerificationInvalid) {
		t.Fatalf("expected rejection past the attempt limit, got %v", err)
	}
}
