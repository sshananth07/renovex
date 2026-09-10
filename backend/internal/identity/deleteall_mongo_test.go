package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// TestAuthSessionRepository_DeleteAllForUser proves Task 1a Step 6's
// exception: AuthSession is scoped by UserID, not CompanyID, so its
// cleanup method is DeleteAllForUser, not DeleteAllForCompany.
func TestAuthSessionRepository_DeleteAllForUser(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_deleteall_sessions")
	repo := identity.NewMongoAuthSessionRepository(db)
	ctx := context.Background()
	now := time.Now()

	if _, err := repo.Create(ctx, identity.AuthSession{
		UserID: "user_a", RefreshTokenHash: "hash_a",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastUsedAt: now,
	}); err != nil {
		t.Fatalf("create user_a session: %v", err)
	}
	if _, err := repo.Create(ctx, identity.AuthSession{
		UserID: "user_b", RefreshTokenHash: "hash_b",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), LastUsedAt: now,
	}); err != nil {
		t.Fatalf("create user_b session: %v", err)
	}

	if err := repo.DeleteAllForUser(ctx, "user_a"); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}

	if _, err := repo.FindActiveByHash(ctx, "hash_a"); err != identity.ErrSessionNotFound {
		t.Fatalf("expected user_a's session to be deleted, got %v", err)
	}
	if _, err := repo.FindActiveByHash(ctx, "hash_b"); err != nil {
		t.Fatalf("expected user_b's session to be untouched, got %v", err)
	}
}

func TestAuthSessionRepository_DeleteAllForUser_EmptyUserIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_deleteall_sessions_empty")
	repo := identity.NewMongoAuthSessionRepository(db)
	ctx := context.Background()

	if err := repo.DeleteAllForUser(ctx, "user_never_existed"); err != nil {
		t.Fatalf("DeleteAllForUser on a user with no sessions should succeed, got: %v", err)
	}
	if err := repo.DeleteAllForUser(ctx, "user_never_existed"); err != nil {
		t.Fatalf("DeleteAllForUser called TWICE should still succeed, got: %v", err)
	}
}
