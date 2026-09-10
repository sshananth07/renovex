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

func sessionFixture(now time.Time, id, tokenHash, challengeID string) supplieraccess.SupplierSession {
	return supplieraccess.SupplierSession{
		ID:                       id,
		CompanyID:                "company-1",
		SupplierID:               "supplier-1",
		RecipientEmailNormalized: "recipient@supplier.test",
		TokenHash:                tokenHash,
		TokenGeneration:          1,
		TokenKeyVersion:          1,
		CreatedFromChallengeID:   challengeID,
		LastVerifiedChallengeID:  challengeID,
		LastVerifiedAt:           now,
		SlidingExpiresAt:         now.Add(30 * 24 * time.Hour),
		AbsoluteExpiresAt:        now.Add(90 * 24 * time.Hour),
		LastUsedAt:               now,
		Revision:                 1,
		SchemaVersion:            1,
	}
}

func TestSessionRepositoryAllowsMultipleDevicesButProtectsCredentialAndProvenance(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoSupplierSessionRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 17, 0, 0, 0, time.UTC)
	first := sessionFixture(now, "session-1", strings.Repeat("1", 64), "challenge-1")
	if _, err := repo.CreateSession(ctx, first); err != nil {
		t.Fatalf("creating first session: %v", err)
	}

	secondDevice := sessionFixture(now, "session-2", strings.Repeat("2", 64), "challenge-2")
	if _, err := repo.CreateSession(ctx, secondDevice); err != nil {
		t.Fatalf("same identity on another device must be allowed: %v", err)
	}
	tokenCollision := sessionFixture(now, "session-3", first.TokenHash, "challenge-3")
	tokenCollision.CompanyID = "company-2"
	if _, err := repo.CreateSession(ctx, tokenCollision); !errors.Is(
		err, supplieraccess.ErrSupplierSessionTokenHashCollision) {
		t.Fatalf("global token collision error = %v", err)
	}
	provenanceCollision := sessionFixture(
		now, "session-4", strings.Repeat("4", 64), first.CreatedFromChallengeID)
	if _, err := repo.CreateSession(ctx, provenanceCollision); !errors.Is(
		err, supplieraccess.ErrSupplierSessionChallengeAlreadyUsed) {
		t.Fatalf("challenge provenance collision error = %v", err)
	}

	resolved, err := repo.FindSessionByTokenHash(ctx, first.TokenHash)
	if err != nil {
		t.Fatalf("global token lookup: %v", err)
	}
	if resolved.ID != first.ID || resolved.CompanyID != first.CompanyID {
		t.Fatalf("resolved session = %#v", resolved)
	}
}

func TestSessionRepositoryRevisionCASProducesExactlyOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoSupplierSessionRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 17, 0, 0, 0, time.UTC)
	session := sessionFixture(now, "session-1", strings.Repeat("5", 64), "challenge-1")
	if _, err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("creating session: %v", err)
	}

	start := make(chan struct{})
	var winners atomic.Int32
	var conflicts atomic.Int32
	var wait sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			candidate := session
			candidate.LastUsedAt = now.Add(time.Duration(worker+1) * time.Minute)
			candidate.Revision = 2
			_, err := repo.ReplaceSessionCAS(ctx, candidate, 1)
			switch {
			case err == nil:
				winners.Add(1)
			case errors.Is(err, supplieraccess.ErrSupplierSessionConflict):
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
}
