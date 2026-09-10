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

func rateLimitFixture(now time.Time) supplieraccess.VerificationRateLimitState {
	return supplieraccess.VerificationRateLimitState{
		ID:           "rate-state-1",
		Scope:        supplieraccess.VerificationRateScopeIdentity,
		ScopeKeyHash: strings.Repeat("d", 64),
		Reservations: []supplieraccess.VerificationRateReservation{
			{ReservationID: "exchange-1", RequestedAt: now},
		},
		Revision:  1,
		UpdatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
}

func TestRateLimitRepositoryEnforcesScopeUniquenessAndCASOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 16, 0, 0, 0, time.UTC)
	state := rateLimitFixture(now)
	if _, err := repo.CreateState(ctx, state); err != nil {
		t.Fatalf("creating rate state: %v", err)
	}

	duplicate := state
	duplicate.ID = "rate-state-2"
	if _, err := repo.CreateState(ctx, duplicate); !errors.Is(
		err, supplieraccess.ErrVerificationRateLimitStateAlreadyExists) {
		t.Fatalf("duplicate scope/hash error = %v", err)
	}

	const workers = 2
	start := make(chan struct{})
	var winners atomic.Int32
	var conflicts atomic.Int32
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			candidate := state
			candidate.Reservations = append(candidate.Reservations,
				supplieraccess.VerificationRateReservation{
					ReservationID: "exchange-concurrent-" + string(rune('a'+worker)),
					RequestedAt:   now.Add(time.Minute),
				})
			candidate.Revision = 2
			candidate.UpdatedAt = now.Add(time.Minute)
			candidate.ExpiresAt = now.Add(61 * time.Minute)
			_, err := repo.ReplaceStateCAS(ctx, candidate, 1)
			switch {
			case err == nil:
				winners.Add(1)
			case errors.Is(err, supplieraccess.ErrVerificationRateLimitStateConflict):
				conflicts.Add(1)
			default:
				t.Errorf("worker %d error: %v", worker, err)
			}
		}(worker)
	}
	close(start)
	wait.Wait()

	if winners.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("winners/conflicts = %d/%d, want 1/1", winners.Load(), conflicts.Load())
	}
	loaded, err := repo.FindState(ctx, state.Scope, state.ScopeKeyHash)
	if err != nil {
		t.Fatalf("finding winning state: %v", err)
	}
	if loaded.Revision != 2 || len(loaded.Reservations) != 2 {
		t.Fatalf("winning state = %#v", loaded)
	}
}
