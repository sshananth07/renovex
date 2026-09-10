package supplieroffers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// setupInternalDB supports service tests that need package-private boundary
// fakes and real MongoDB repositories in the same test.
func setupInternalDB(t *testing.T) *mongo.Database {
	t.Helper()
	ctx := context.Background()

	// Service-level withdrawal tests exercise G5's approved transaction, so the
	// internal-package fixture must use the same replica-set topology as the
	// external repository fixture.
	container, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	if err != nil {
		t.Fatalf("starting MongoDB: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminating MongoDB: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("reading MongoDB connection string: %v", err)
	}
	// Direct mode avoids following the member's container-only advertised
	// address on Windows while preserving replica-set transaction semantics.
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetDirect(true))
	if err != nil {
		t.Fatalf("connecting to MongoDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	return client.Database(fmt.Sprintf("supplieroffers_internal_test_%d", time.Now().UnixNano()))
}
