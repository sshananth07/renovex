package supplieraccess_test

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

// setupDB gives each real-Mongo test an isolated database. Phase D's
// uniqueness and compare-and-swap guarantees are database behavior, so fakes
// are not acceptable evidence for those invariants.
func setupDB(t *testing.T) *mongo.Database {
	t.Helper()
	ctx := context.Background()
	container, err := mongodb.Run(ctx, "mongo:7")
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
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connecting to MongoDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })
	return client.Database(fmt.Sprintf("supplieraccess_test_%d", time.Now().UnixNano()))
}
