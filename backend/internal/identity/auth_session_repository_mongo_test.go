package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func TestAuthSessionRepositoryCreateAndFindActive(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_sessions")
	repo := identity.NewMongoAuthSessionRepository(db)

	ctx := context.Background()
	now := time.Now()
	created, err := repo.Create(ctx, identity.AuthSession{
		UserID:           "user_1",
		RefreshTokenHash: "hash_1",
		CreatedAt:        now,
		ExpiresAt:        now.Add(7 * 24 * time.Hour),
		LastUsedAt:       now,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindActiveByHash(ctx, "hash_1")
	if err != nil {
		t.Fatalf("unexpected error finding active: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected same session, got different ID")
	}
}

func TestAuthSessionRepositoryRotateHash(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_sessions_rotate")
	repo := identity.NewMongoAuthSessionRepository(db)

	ctx := context.Background()
	now := time.Now()
	created, err := repo.Create(ctx, identity.AuthSession{
		UserID: "user_1", RefreshTokenHash: "hash_old", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastUsedAt: now,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	if err := repo.RotateHash(ctx, created.ID, "hash_new", time.Now()); err != nil {
		t.Fatalf("unexpected error rotating: %v", err)
	}

	_, err = repo.FindActiveByHash(ctx, "hash_old")
	if err != identity.ErrSessionNotFound {
		t.Fatalf("expected old hash to no longer resolve, got %v", err)
	}

	found, err := repo.FindActiveByHash(ctx, "hash_new")
	if err != nil {
		t.Fatalf("unexpected error finding by new hash: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected same session under new hash")
	}
}

func TestAuthSessionRepositoryRevoke(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_sessions_revoke")
	repo := identity.NewMongoAuthSessionRepository(db)

	ctx := context.Background()
	now := time.Now()
	created, err := repo.Create(ctx, identity.AuthSession{
		UserID: "user_1", RefreshTokenHash: "hash_1", CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastUsedAt: now,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	if err := repo.Revoke(ctx, created.ID, time.Now()); err != nil {
		t.Fatalf("unexpected error revoking: %v", err)
	}

	_, err = repo.FindActiveByHash(ctx, "hash_1")
	if err != identity.ErrSessionNotFound {
		t.Fatalf("expected revoked session to no longer resolve as active, got %v", err)
	}
}

func TestAuthSessionRepositoryExpiredNotActive(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_sessions_expired")
	repo := identity.NewMongoAuthSessionRepository(db)

	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	_, err := repo.Create(ctx, identity.AuthSession{
		UserID: "user_1", RefreshTokenHash: "hash_expired", CreatedAt: past, ExpiresAt: past.Add(time.Minute), LastUsedAt: past,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindActiveByHash(ctx, "hash_expired")
	if err != identity.ErrSessionNotFound {
		t.Fatalf("expected expired session to not be found as active, got %v", err)
	}
}
