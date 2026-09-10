package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// Phase H7 — readiness closure (spec §21.5, gap 1).
//
// Startup already fails on a topology that cannot run transactions, but a
// point-in-time readiness response could still report `ok` without checking
// that capability. A pod that answers /ready while the G5 withdrawal boundary
// cannot begin is reporting a readiness it does not have.
//
// Readiness therefore depends on BOTH connectivity and the existing
// side-effect-free, read-only transaction probe. Topology details, replica-set
// names, driver text and connection strings stay out of the external response.

// A standalone MongoDB is reachable but cannot run the transaction the
// withdrawal boundary requires, so readiness must fail closed.
func TestReadyFailsClosedWhenTransactionsAreUnsupported(t *testing.T) {
	ctx := context.Background()

	// No replica set: pings succeed, transactions do not.
	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	connectionString, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	client, err := platformmongo.Connect(connectionString)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = platformmongo.Disconnect(context.Background(), client) }()

	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, client, "readiness_test")

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready = %d on a topology without transactions, want 503: %s",
			response.Code, response.Body.String())
	}

	// One generic external code; operational diagnostics stay in the logs.
	body := response.Body.String()
	if !strings.Contains(body, "service_not_ready") {
		t.Errorf("/ready body = %s, want the generic service_not_ready code", body)
	}
	for _, leaked := range []string{
		"replica set", "replicaSet", "rs0", "mongodb://", "localhost",
		"transaction numbers are only allowed", "topology",
	} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leaked)) {
			t.Errorf("/ready leaked operational detail %q: %s", leaked, body)
		}
	}
}

// A transaction-capable replica set is ready, so the probe cannot simply
// refuse everything.
func TestReadySucceedsOnATransactionCapableTopology(t *testing.T) {
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	connectionString, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	// Keep discovery on Docker's mapped endpoint, as the tenant suite does.
	connectionString += "&directConnection=true"

	client, err := platformmongo.Connect(connectionString)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() { _ = platformmongo.Disconnect(context.Background(), client) }()

	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, client, "readiness_test")

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("/ready = %d on a replica set, want 200: %s",
			response.Code, response.Body.String())
	}
}

// An unreachable MongoDB uses the SAME generic external code, so a caller
// cannot distinguish "down" from "wrong topology".
func TestReadyUsesOneGenericCodeForEveryUnreadyCause(t *testing.T) {
	client, err := platformmongo.Connect(
		"mongodb://localhost:1/?connectTimeoutMS=500&serverSelectionTimeoutMS=500")
	if err != nil {
		t.Fatalf("Connect itself should not fail (lazy connection): %v", err)
	}
	defer func() { _ = platformmongo.Disconnect(context.Background(), client) }()

	router, api := platformhttp.NewRouter("Test API", "0.0.1")
	platformhttp.RegisterHealth(api, client, "readiness_test")

	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready = %d when Mongo is unreachable, want 503: %s",
			response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "service_not_ready") {
		t.Errorf("/ready body = %s, want the generic service_not_ready code", body)
	}
	// A driver error must never reach the external response.
	for _, leaked := range []string{"localhost:1", "mongodb://", "dial tcp"} {
		if strings.Contains(body, leaked) {
			t.Errorf("/ready leaked driver detail %q: %s", leaked, body)
		}
	}
}
