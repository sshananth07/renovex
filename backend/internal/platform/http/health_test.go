package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func TestHealthEndpointReturns200WithoutDependencies(t *testing.T) {
	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, nil, "") // nil client: /health must not touch Mongo

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Readiness needs a transaction-capable topology, not merely a reachable one
// (H7, §21.5): a standalone deployment answers pings while the withdrawal
// boundary cannot begin. The replica set here is what production requires.
func TestReadyEndpointReturns200WhenMongoIsUp(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	connStr += "&directConnection=true"

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = platformmongo.Disconnect(context.Background(), client) }()

	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, client, "readiness_test")

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestReadyEndpointReturns503WhenMongoIsUnreachable(t *testing.T) {
	client, err := platformmongo.Connect("mongodb://localhost:1/?connectTimeoutMS=500&serverSelectionTimeoutMS=500")
	if err != nil {
		t.Fatalf("Connect itself should not fail (lazy connection): %v", err)
	}
	defer func() { _ = platformmongo.Disconnect(context.Background(), client) }()

	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, client, "readiness_test")

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}
