package supplieroffers

import (
	"context"
	"errors"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestWithdrawalTransactionReadinessRejectsStandaloneMongo(t *testing.T) {
	ctx := context.Background()
	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("start standalone MongoDB: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate standalone MongoDB: %v", err)
		}
	})
	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("standalone connection string: %v", err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetDirect(true))
	if err != nil {
		t.Fatalf("connect standalone MongoDB: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	repository := NewMongoOfferEligibilityRepository(client.Database("readiness_test"))
	if err := repository.VerifyWithdrawalTransactionSupport(ctx); !errors.Is(err, ErrOfferTransactionsUnavailable) {
		t.Fatalf("readiness error = %v, want transactions-unavailable", err)
	}
}
