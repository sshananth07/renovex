package supplieraccess_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func TestVerificationRateLimiterEnforcesIdempotencyCooldownAndRollingHour(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	limiter := supplieraccess.NewVerificationRateLimiter(
		repo, supplieraccess.CryptographicOpaqueTokenGenerator{})
	ctx := context.Background()
	hash := strings.Repeat("e", 64)
	start := time.Date(2026, 7, 29, 20, 0, 0, 0, time.UTC)

	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity,
		hash, "exchange-1", start); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity,
		hash, "exchange-1", start.Add(time.Second)); err != nil {
		t.Fatalf("same reservation must be idempotent during cooldown: %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity,
		hash, "exchange-2", start.Add(59*time.Second)); !errors.Is(
		err, supplieraccess.ErrVerificationRateLimited) {
		t.Fatalf("59-second cooldown error = %v", err)
	}

	for index, offset := range []time.Duration{
		time.Minute, 2 * time.Minute, 3 * time.Minute, 4 * time.Minute,
	} {
		if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity,
			hash, "exchange-"+string(rune('2'+index)), start.Add(offset)); err != nil {
			t.Fatalf("allowed claim %d: %v", index, err)
		}
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity,
		hash, "exchange-over-limit", start.Add(5*time.Minute)); !errors.Is(
		err, supplieraccess.ErrVerificationRateLimited) {
		t.Fatalf("sixth rolling-hour claim error = %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity,
		hash, "exchange-at-boundary", start.Add(time.Hour)); err != nil {
		t.Fatalf("claim at exact rolling-window boundary: %v", err)
	}
}

func TestVerificationRateLimiterConcurrentClientClaimsNeverExceedTwenty(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	limiter := supplieraccess.NewVerificationRateLimiter(
		repo, supplieraccess.CryptographicOpaqueTokenGenerator{})
	ctx := context.Background()
	hash := strings.Repeat("f", 64)
	now := time.Date(2026, 7, 29, 20, 0, 0, 0, time.UTC)

	const workers = 24
	start := make(chan struct{})
	var allowed atomic.Int32
	var limited atomic.Int32
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeClient,
				hash, "exchange-"+string(rune('a'+worker)), now)
			switch {
			case err == nil:
				allowed.Add(1)
			case errors.Is(err, supplieraccess.ErrVerificationRateLimited):
				limited.Add(1)
			default:
				t.Errorf("worker %d error: %v", worker, err)
			}
		}(worker)
	}
	close(start)
	wait.Wait()

	if allowed.Load() != 20 || limited.Load() != 4 {
		t.Fatalf("allowed/limited = %d/%d, want 20/4", allowed.Load(), limited.Load())
	}
	state, err := repo.FindState(
		ctx, supplieraccess.VerificationRateScopeClient, hash)
	if err != nil {
		t.Fatalf("loading final client rate state: %v", err)
	}
	if len(state.Reservations) != 20 {
		t.Fatalf("persisted reservations = %d, want 20", len(state.Reservations))
	}
}

func TestVerificationRateLimiterConcurrentIdentityBoundaryHasOneFifthWinner(
	t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	limiter := supplieraccess.NewVerificationRateLimiter(
		repo, supplieraccess.CryptographicOpaqueTokenGenerator{})
	ctx := context.Background()
	hash := strings.Repeat("d", 64)
	start := time.Date(2026, 7, 30, 8, 0, 0, 0, time.UTC)
	for index := 0; index < 4; index++ {
		if err := limiter.Claim(ctx,
			supplieraccess.VerificationRateScopeIdentity,
			hash, "seed-"+string(rune('a'+index)),
			start.Add(time.Duration(index)*time.Minute)); err != nil {
			t.Fatalf("seeding identity claim %d: %v", index, err)
		}
	}

	const contenders = 6
	boundary := start.Add(4 * time.Minute)
	startClaims := make(chan struct{})
	var allowed atomic.Int32
	var limited atomic.Int32
	var wait sync.WaitGroup
	for contender := 0; contender < contenders; contender++ {
		wait.Add(1)
		go func(contender int) {
			defer wait.Done()
			<-startClaims
			err := limiter.Claim(ctx,
				supplieraccess.VerificationRateScopeIdentity,
				hash, "contender-"+string(rune('a'+contender)), boundary)
			switch {
			case err == nil:
				allowed.Add(1)
			case errors.Is(err, supplieraccess.ErrVerificationRateLimited):
				limited.Add(1)
			default:
				t.Errorf("contender %d error: %v", contender, err)
			}
		}(contender)
	}
	close(startClaims)
	wait.Wait()

	if allowed.Load() != 1 || limited.Load() != contenders-1 {
		t.Fatalf("allowed/limited = %d/%d, want 1/%d",
			allowed.Load(), limited.Load(), contenders-1)
	}
	state, err := repo.FindState(
		ctx, supplieraccess.VerificationRateScopeIdentity, hash)
	if err != nil {
		t.Fatalf("loading final identity rate state: %v", err)
	}
	if len(state.Reservations) != 5 {
		t.Fatalf("persisted reservations = %d, want bounded 5",
			len(state.Reservations))
	}
}

func TestVerificationRateLimiterKeepsResendQuotaSeparateFromChallengeCreation(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	limiter := supplieraccess.NewVerificationRateLimiter(
		repo, supplieraccess.CryptographicOpaqueTokenGenerator{})
	ctx := context.Background()
	resendHash := strings.Repeat("a", 64)
	creationHash := strings.Repeat("b", 64)
	start := time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)

	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity,
		creationHash, "exchange-1", start); err != nil {
		t.Fatalf("challenge creation claim: %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeResend,
		resendHash, "resend-operation-1", start); err != nil {
		t.Fatalf("first resend claim: %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeResend,
		resendHash, "resend-operation-1", start.Add(time.Second)); err != nil {
		t.Fatalf("same resend operation must be idempotent: %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeResend,
		resendHash, "resend-operation-2", start.Add(59*time.Second)); !errors.Is(
		err, supplieraccess.ErrVerificationRateLimited) {
		t.Fatalf("resend before 60-second boundary error = %v", err)
	}
	for index := 2; index <= 5; index++ {
		at := start.Add(time.Duration(index-1) * time.Minute)
		if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeResend,
			resendHash, "resend-operation-"+string(rune('0'+index)), at); err != nil {
			t.Fatalf("allowed resend %d: %v", index, err)
		}
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeResend,
		resendHash, "resend-operation-6", start.Add(5*time.Minute)); !errors.Is(
		err, supplieraccess.ErrVerificationRateLimited) {
		t.Fatalf("sixth resend error = %v", err)
	}

	creationState, err := repo.FindState(
		ctx, supplieraccess.VerificationRateScopeIdentity, creationHash)
	if err != nil {
		t.Fatalf("loading creation rate state: %v", err)
	}
	if len(creationState.Reservations) != 1 {
		t.Fatalf("resends consumed creation quota: %#v", creationState.Reservations)
	}
}
