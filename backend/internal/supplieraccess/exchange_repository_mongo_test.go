package supplieraccess_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func exchangeFixture(now time.Time, id, hash string) supplieraccess.SupplierAccessExchange {
	return supplieraccess.SupplierAccessExchange{
		ID:                       id,
		ExchangeTokenHash:        hash,
		CompanyID:                "company-1",
		SupplierID:               "supplier-1",
		InvitationID:             "invitation-1",
		AccessGeneration:         2,
		NormalizedRecipientEmail: "recipient@supplier.test",
		CreatedAt:                now,
		ExpiresAt:                now.Add(10 * time.Minute),
	}
}

func TestAccessExchangeRepositoryEnforcesGlobalHashUniquenessAndTTL(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoAccessExchangeRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 14, 0, 0, 0, time.UTC)

	if _, err := repo.CreateExchange(ctx,
		exchangeFixture(now, "exchange-1", strings.Repeat("a", 64))); err != nil {
		t.Fatalf("creating first exchange: %v", err)
	}
	second := exchangeFixture(now, "exchange-2", strings.Repeat("a", 64))
	second.CompanyID = "company-2"
	if _, err := repo.CreateExchange(ctx, second); !errors.Is(
		err, supplieraccess.ErrExchangeTokenHashCollision) {
		t.Fatalf("duplicate global hash error = %v", err)
	}

	cursor, err := db.Collection("supplier_access_exchanges").Indexes().List(ctx)
	if err != nil {
		t.Fatalf("listing indexes: %v", err)
	}
	defer cursor.Close(ctx)
	var indexes []bson.M
	if err := cursor.All(ctx, &indexes); err != nil {
		t.Fatalf("decoding indexes: %v", err)
	}
	foundTTL := false
	for _, index := range indexes {
		if index["name"] == "ttl_supplier_access_exchanges_expires_at" {
			foundTTL = index["expireAfterSeconds"] == int32(0) ||
				index["expireAfterSeconds"] == int64(0) ||
				index["expireAfterSeconds"] == float64(0)
		}
	}
	if !foundTTL {
		t.Fatalf("named zero-second TTL index not found: %#v", indexes)
	}
}

func TestAccessExchangeRepositoryGlobalLookupEnforcesExactExpiryBoundary(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoAccessExchangeRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 14, 0, 0, 0, time.UTC)
	input := exchangeFixture(now, "exchange-1", strings.Repeat("b", 64))
	if _, err := repo.CreateExchange(ctx, input); err != nil {
		t.Fatalf("creating exchange: %v", err)
	}

	resolved, err := repo.FindUsableByTokenHash(ctx, input.ExchangeTokenHash,
		input.ExpiresAt.Add(-time.Nanosecond))
	if err != nil {
		t.Fatalf("resolving before expiry: %v", err)
	}
	if resolved.CompanyID != input.CompanyID || resolved.ID != input.ID {
		t.Fatalf("global resolution returned %#v", resolved)
	}
	if _, err := repo.FindUsableByTokenHash(ctx, input.ExchangeTokenHash,
		input.ExpiresAt); !errors.Is(err, supplieraccess.ErrAccessExchangeNotFound) {
		t.Fatalf("exact-expiry error = %v, want neutral not found", err)
	}
}

func TestConsumeAccessExchangeIsIdempotentOnlyForTheRecordedChallenge(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoAccessExchangeRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 14, 0, 0, 0, time.UTC)
	input := exchangeFixture(now, "exchange-1", strings.Repeat("c", 64))
	if _, err := repo.CreateExchange(ctx, input); err != nil {
		t.Fatalf("creating exchange: %v", err)
	}

	consumed, err := repo.ConsumeForChallenge(
		ctx, input.ID, "challenge-1", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("consuming exchange: %v", err)
	}
	if consumed.ConsumedAt == nil || consumed.ChallengeID == nil ||
		*consumed.ChallengeID != "challenge-1" {
		t.Fatalf("consumed exchange = %#v", consumed)
	}
	replayed, err := repo.ConsumeForChallenge(
		ctx, input.ID, "challenge-1", now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("same challenge replay: %v", err)
	}
	if replayed.ConsumedAt == nil || !replayed.ConsumedAt.Equal(*consumed.ConsumedAt) {
		t.Fatal("idempotent replay changed the authoritative consumed time")
	}
	if _, err := repo.ConsumeForChallenge(
		ctx, input.ID, "challenge-2", now.Add(2*time.Minute)); !errors.Is(
		err, supplieraccess.ErrAccessExchangeConsumed) {
		t.Fatalf("different challenge error = %v, want ErrAccessExchangeConsumed", err)
	}
	recovered, err := repo.FindExchangeByTokenHash(ctx, input.ExchangeTokenHash)
	if err != nil || recovered.ChallengeID == nil ||
		*recovered.ChallengeID != "challenge-1" {
		t.Fatalf("consumed exchange recovery = %#v/%v", recovered, err)
	}
}
