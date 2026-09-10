package supplieraccess_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func challengeFixture(now time.Time, id, exchangeID, operationID string) supplieraccess.EmailVerificationChallenge {
	return supplieraccess.EmailVerificationChallenge{
		ID:                       id,
		ExchangeID:               exchangeID,
		CompanyID:                "company-1",
		SupplierID:               "supplier-1",
		RecipientEmailNormalized: "recipient@supplier.test",
		InvitationID:             "invitation-1",
		AccessGeneration:         2,
		ChallengeOperationID:     operationID,
		CodeVerifier:             strings.Repeat("a", 64),
		CodeKeyVersion:           1,
		AttemptsRemaining:        5,
		ExpiresAt:                now.Add(10 * time.Minute),
		Revision:                 1,
		CreatedAt:                now,
		SchemaVersion:            1,
	}
}

func TestChallengeRepositoryRoundTripsRecoveryFieldsAndEnforcesIdentities(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationChallengeRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 15, 0, 0, 0, time.UTC)
	input := challengeFixture(now, "challenge-1", "exchange-1", "operation-1")
	input.ConsumedAt = pointerTime(now.Add(time.Minute))
	input.ConsumedOperationID = "verify-operation-1"
	input.SupplierSessionID = "session-1"
	input.TargetTokenGeneration = 3
	input.SessionTokenKeyVersion = 2

	created, err := repo.CreateChallenge(ctx, input)
	if err != nil {
		t.Fatalf("creating challenge: %v", err)
	}
	loaded, err := repo.FindChallenge(ctx, created.ID)
	if err != nil {
		t.Fatalf("finding challenge globally by opaque ID: %v", err)
	}
	if loaded.ConsumedOperationID != input.ConsumedOperationID ||
		loaded.SupplierSessionID != input.SupplierSessionID ||
		loaded.TargetTokenGeneration != input.TargetTokenGeneration ||
		loaded.SessionTokenKeyVersion != input.SessionTokenKeyVersion {
		t.Fatalf("recovery fields did not round-trip: %#v", loaded)
	}
	byExchange, err := repo.FindChallengeByExchange(ctx, input.ExchangeID)
	if err != nil || byExchange.ID != input.ID {
		t.Fatalf("exchange lookup = %q/%v", byExchange.ID, err)
	}
	byOperation, err := repo.FindChallengeByOperation(
		ctx, input.CompanyID, input.ChallengeOperationID)
	if err != nil || byOperation.ID != input.ID {
		t.Fatalf("operation lookup = %q/%v", byOperation.ID, err)
	}

	sameOperation := challengeFixture(
		now, "challenge-2", "exchange-2", input.ChallengeOperationID)
	if _, err := repo.CreateChallenge(ctx, sameOperation); !errors.Is(
		err, supplieraccess.ErrChallengeOperationAlreadyUsed) {
		t.Fatalf("same company/operation error = %v", err)
	}
	sameExchange := challengeFixture(
		now, "challenge-3", input.ExchangeID, "operation-3")
	if _, err := repo.CreateChallenge(ctx, sameExchange); !errors.Is(
		err, supplieraccess.ErrExchangeChallengeAlreadyExists) {
		t.Fatalf("same exchange error = %v", err)
	}

	foreignCompany := challengeFixture(
		now, "challenge-4", "exchange-4", input.ChallengeOperationID)
	foreignCompany.CompanyID = "company-2"
	if _, err := repo.CreateChallenge(ctx, foreignCompany); err != nil {
		t.Fatalf("operation IDs are tenant-scoped, got %v", err)
	}
}

func pointerTime(value time.Time) *time.Time { return &value }

func TestNewerChallengeBecomesAuthoritativeAndObsoletesOnlyPendingDelivery(t *testing.T) {
	db := setupDB(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	deliveries := supplieraccess.NewMongoVerificationDeliveryRepository(db)
	ctx := context.Background()
	if err := challenges.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring challenge indexes: %v", err)
	}
	if err := deliveries.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring delivery indexes: %v", err)
	}
	now := time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	older := challengeFixture(now, "challenge-a", "exchange-a", "operation-a")
	if _, err := challenges.CreateChallenge(ctx, older); err != nil {
		t.Fatalf("creating older challenge: %v", err)
	}
	pending := verificationDeliveryFixture(now, "attempt-pending", "delivery-pending")
	pending.ChallengeID = older.ID
	if _, err := deliveries.CreateAttempt(ctx, pending); err != nil {
		t.Fatalf("creating pending delivery: %v", err)
	}
	sent := verificationDeliveryFixture(now, "attempt-sent", "delivery-sent")
	sent.ChallengeID = older.ID
	if _, err := deliveries.CreateAttempt(ctx, sent); err != nil {
		t.Fatalf("creating sent delivery: %v", err)
	}
	if _, err := deliveries.TransitionPending(ctx, sent.CompanyID, sent.ID,
		supplieraccess.VerificationDeliverySent, nil, now.Add(time.Second)); err != nil {
		t.Fatalf("marking historical delivery sent: %v", err)
	}

	newer := challengeFixture(
		now.Add(time.Minute), "challenge-b", "exchange-b", "operation-b")
	if _, err := challenges.CreateChallenge(ctx, newer); err != nil {
		t.Fatalf("creating newer challenge: %v", err)
	}
	supersededIDs, err := challenges.SupersedeOlderChallenges(
		ctx, newer, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("superseding older challenges: %v", err)
	}
	if len(supersededIDs) != 1 || supersededIDs[0] != older.ID {
		t.Fatalf("superseded IDs = %v, want [%s]", supersededIDs, older.ID)
	}
	if err := deliveries.ObsoletePendingForChallenges(
		ctx, older.CompanyID, supersededIDs, now.Add(time.Minute)); err != nil {
		t.Fatalf("obsoleting pending delivery: %v", err)
	}

	latest, err := challenges.FindLatestChallenge(
		ctx, newer.CompanyID, newer.InvitationID, newer.AccessGeneration)
	if err != nil || latest.ID != newer.ID {
		t.Fatalf("latest challenge/error = %q/%v, want %q/nil",
			latest.ID, err, newer.ID)
	}
	loadedOlder, err := challenges.FindChallenge(ctx, older.ID)
	if err != nil || loadedOlder.SupersededAt == nil {
		t.Fatalf("older challenge/error = %#v/%v", loadedOlder, err)
	}
	loadedPending, _ := deliveries.FindAttemptByOperation(
		ctx, pending.CompanyID, pending.ChallengeID, pending.OperationID)
	loadedSent, _ := deliveries.FindAttemptByOperation(
		ctx, sent.CompanyID, sent.ChallengeID, sent.OperationID)
	if loadedPending.Status != supplieraccess.VerificationDeliveryObsolete {
		t.Fatalf("pending status = %q, want obsolete", loadedPending.Status)
	}
	if loadedSent.Status != supplieraccess.VerificationDeliverySent {
		t.Fatalf("sent status = %q, historical fact was rewritten", loadedSent.Status)
	}
}
