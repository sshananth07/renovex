// Package mongo centralizes MongoDB client creation and lifecycle
// management for the application. Domain modules receive a *mongo.Database
// from here and own their own collections; this package does not expose a
// generic cross-domain repository.
package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// transactionProbeCollection is counted inside the readiness transaction. It
// holds no business data and need not exist: counting an absent collection is
// still a real transactional read, which is exactly what the probe verifies.
const transactionProbeCollection = "_readiness_probe"

// Connect builds a MongoDB client for the given connection URI. Connection
// is lazy: this call does not itself verify connectivity — call Ping to do
// that (e.g. from a /ready handler).
func Connect(uri string) (*mongo.Client, error) {
	serverAPI := options.ServerAPI(options.ServerAPIVersion1)
	clientOpts := options.Client().
		ApplyURI(uri).
		SetServerAPIOptions(serverAPI)

	return mongo.Connect(clientOpts)
}

// Ping verifies the client can reach the primary within ctx's deadline.
func Ping(ctx context.Context, client *mongo.Client) error {
	return client.Ping(ctx, readpref.Primary())
}

// VerifyTransactionSupport reports whether the connected topology can run a
// multi-document transaction.
//
// Readiness depends on this as well as connectivity: a standalone deployment
// answers pings perfectly while the withdrawal boundary cannot begin, so a
// ping-only probe would report a readiness the process does not have.
//
// The transaction is READ-ONLY — a bounded count over one probe collection —
// so a readiness check never mutates business data, and it runs under whatever
// deadline the caller supplies. Administrative commands such as listDatabases
// are not permitted inside a transaction, so a count is used instead.
func VerifyTransactionSupport(
	ctx context.Context, client *mongo.Client, databaseName string) error {

	session, err := client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)

	collection := client.Database(databaseName).
		Collection(transactionProbeCollection)
	_, err = session.WithTransaction(ctx,
		func(transactionContext context.Context) (any, error) {
			return collection.CountDocuments(transactionContext, bson.M{},
				options.Count().SetLimit(1))
		})
	return err
}

// Disconnect closes the client's connections.
func Disconnect(ctx context.Context, client *mongo.Client) error {
	return client.Disconnect(ctx)
}

// Database returns a handle to the named database on the given client.
func Database(client *mongo.Client, name string) *mongo.Database {
	return client.Database(name)
}
