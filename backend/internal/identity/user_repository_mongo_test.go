package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return client
}

func TestUserRepositoryCreateAndFind(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test")
	repo := identity.NewMongoUserRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, identity.User{
		Email:        "owner@example.com",
		PasswordHash: "hash",
		CreatedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating user: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	byEmail, err := repo.FindByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatalf("unexpected error finding by email: %v", err)
	}
	if byEmail.ID != created.ID {
		t.Fatalf("expected same user, got different ID")
	}

	byID, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding by id: %v", err)
	}
	if byID.Email != "owner@example.com" {
		t.Fatalf("expected owner@example.com, got %s", byID.Email)
	}
}

func TestUserRepositoryDuplicateEmailRejected(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_dup")
	repo := identity.NewMongoUserRepository(db)

	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	_, err := repo.Create(ctx, identity.User{Email: "dup@example.com", PasswordHash: "h", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error on first create: %v", err)
	}

	_, err = repo.Create(ctx, identity.User{Email: "dup@example.com", PasswordHash: "h2", CreatedAt: time.Now()})
	if err == nil {
		t.Fatal("expected error creating duplicate email, got nil")
	}
	if err != identity.ErrDuplicateEmail {
		t.Fatalf("expected ErrDuplicateEmail, got %v", err)
	}
}

func TestUserRepositoryFindByEmailNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_notfound")
	repo := identity.NewMongoUserRepository(db)

	_, err := repo.FindByEmail(context.Background(), "nobody@example.com")
	if err != identity.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserRepositoryDelete(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "identity_test_delete")
	repo := identity.NewMongoUserRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, identity.User{Email: "todelete@example.com", PasswordHash: "h", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}

	_, err = repo.FindByID(ctx, created.ID)
	if err != identity.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound after delete, got %v", err)
	}
}
