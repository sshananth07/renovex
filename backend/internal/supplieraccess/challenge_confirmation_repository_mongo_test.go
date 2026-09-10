package supplieraccess_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func TestChallengeRepositoryFifthFailedConfirmationLocksExactlyOnce(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationChallengeRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring challenge indexes: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	challenge := challengeFixture(
		now, opaqueToken(11), opaqueToken(12), "challenge-operation-lock")
	challenge.AttemptsRemaining = 1
	if _, err := repo.CreateChallenge(ctx, challenge); err != nil {
		t.Fatalf("creating challenge: %v", err)
	}

	start := make(chan struct{})
	var transitions atomic.Int32
	var rejected atomic.Int32
	var wait sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := repo.RecordFailedConfirmation(
				ctx, challenge, now.Add(time.Minute))
			switch {
			case err == nil:
				transitions.Add(1)
			case errors.Is(err,
				supplieraccess.ErrVerificationChallengeNotConfirmable):
				rejected.Add(1)
			default:
				t.Errorf("failed-confirmation transition: %v", err)
			}
		}()
	}
	close(start)
	wait.Wait()

	if transitions.Load() != 1 || rejected.Load() != 1 {
		t.Fatalf("transitions/rejected = %d/%d, want 1/1",
			transitions.Load(), rejected.Load())
	}
	locked, err := repo.FindChallenge(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("loading locked challenge: %v", err)
	}
	if locked.AttemptsRemaining != 0 || locked.LockedAt == nil ||
		!locked.LockedAt.Equal(now.Add(time.Minute)) || locked.Revision != 2 {
		t.Fatalf("locked challenge = %#v", locked)
	}
}

func TestChallengeRepositoryConcurrentConfirmationHasOneConsumer(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationChallengeRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring challenge indexes: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	challenge := challengeFixture(
		now, opaqueToken(21), opaqueToken(22), "challenge-operation-consume")
	if _, err := repo.CreateChallenge(ctx, challenge); err != nil {
		t.Fatalf("creating challenge: %v", err)
	}

	targets := []supplieraccess.VerificationSessionTarget{
		{
			OperationID: "verify-operation-a", SupplierSessionID: opaqueToken(31),
			TokenGeneration: 1, TokenKeyVersion: 2,
		},
		{
			OperationID: "verify-operation-b", SupplierSessionID: opaqueToken(32),
			TokenGeneration: 1, TokenKeyVersion: 2,
		},
	}
	start := make(chan struct{})
	var transitions atomic.Int32
	var rejected atomic.Int32
	var wait sync.WaitGroup
	for _, target := range targets {
		wait.Add(1)
		go func(target supplieraccess.VerificationSessionTarget) {
			defer wait.Done()
			<-start
			_, err := repo.ConsumeForVerification(
				ctx, challenge, target, now.Add(time.Minute))
			switch {
			case err == nil:
				transitions.Add(1)
			case errors.Is(err,
				supplieraccess.ErrVerificationChallengeNotConfirmable):
				rejected.Add(1)
			default:
				t.Errorf("consume transition: %v", err)
			}
		}(target)
	}
	close(start)
	wait.Wait()

	if transitions.Load() != 1 || rejected.Load() != 1 {
		t.Fatalf("transitions/rejected = %d/%d, want 1/1",
			transitions.Load(), rejected.Load())
	}
	consumed, err := repo.FindChallenge(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("loading consumed challenge: %v", err)
	}
	if consumed.ConsumedAt == nil ||
		!consumed.ConsumedAt.Equal(now.Add(time.Minute)) ||
		consumed.Revision != 2 || consumed.SupplierSessionID == "" ||
		consumed.TargetTokenGeneration != 1 ||
		consumed.SessionTokenKeyVersion != 2 {
		t.Fatalf("consumed challenge = %#v", consumed)
	}
	winnerMatches := false
	for _, target := range targets {
		if consumed.ConsumedOperationID == target.OperationID &&
			consumed.SupplierSessionID == target.SupplierSessionID {
			winnerMatches = true
		}
	}
	if !winnerMatches {
		t.Fatalf("consumption fields do not belong to one target: %#v", consumed)
	}
}

func TestChallengeRepositoryCannotConfirmAtExactExpiry(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationChallengeRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring challenge indexes: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	challenge := challengeFixture(
		now, opaqueToken(41), opaqueToken(42), "challenge-operation-expiry")
	if _, err := repo.CreateChallenge(ctx, challenge); err != nil {
		t.Fatalf("creating challenge: %v", err)
	}
	target := supplieraccess.VerificationSessionTarget{
		OperationID:       "verify-operation-expiry",
		SupplierSessionID: opaqueToken(43),
		TokenGeneration:   1,
		TokenKeyVersion:   1,
	}

	if _, err := repo.ConsumeForVerification(
		ctx, challenge, target, challenge.ExpiresAt); !errors.Is(
		err, supplieraccess.ErrVerificationChallengeNotConfirmable) {
		t.Fatalf("exact-expiry confirmation error = %v", err)
	}
	unchanged, err := repo.FindChallenge(ctx, challenge.ID)
	if err != nil {
		t.Fatalf("loading unchanged challenge: %v", err)
	}
	if unchanged.ConsumedAt != nil || unchanged.Revision != 1 ||
		unchanged.AttemptsRemaining != 5 {
		t.Fatalf("exact-expiry confirmation mutated challenge: %#v", unchanged)
	}
}
