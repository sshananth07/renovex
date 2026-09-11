package mongo_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func TestConnectAndPing(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
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
	defer func() {
		_ = platformmongo.Disconnect(context.Background(), client)
	}()

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := platformmongo.Ping(pingCtx, client); err != nil {
		t.Fatalf("expected ping to succeed, got error: %v", err)
	}
}

func TestPingFailsWhenUnreachable(t *testing.T) {
	// Port 1 is reserved and nothing will be listening there.
	client, err := platformmongo.Connect("mongodb://localhost:1/?connectTimeoutMS=500&serverSelectionTimeoutMS=500")
	if err != nil {
		t.Fatalf("Connect itself should not fail (lazy connection): %v", err)
	}
	defer func() {
		_ = platformmongo.Disconnect(context.Background(), client)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := platformmongo.Ping(ctx, client); err == nil {
		t.Fatal("expected ping to an unreachable host to fail, got nil error")
	}
}
