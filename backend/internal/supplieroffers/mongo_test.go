package supplieroffers_test

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

// setupDB gives every persistence test its own real MongoDB database. Phase
// E's uniqueness and compare-and-swap guarantees live in MongoDB indexes and
// conditional writes, so an in-memory fake cannot prove these boundaries.
func setupDB(t *testing.T) *mongo.Database {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	t.Helper()
	ctx := context.Background()

	// G5's one approved multi-document transaction requires a transaction-
	// capable topology. A single-member replica set is sufficient for tests and
	// exercises the same session/commit semantics as production.
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
	// The single member advertises its container address, which is not routable
	// from a Windows host. Direct mode keeps discovery on the mapped endpoint
	// while retaining replica-set transaction support.
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetDirect(true))
	if err != nil {
		t.Fatalf("connecting to MongoDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	return client.Database(fmt.Sprintf("supplieroffers_test_%d", time.Now().UnixNano()))
}
