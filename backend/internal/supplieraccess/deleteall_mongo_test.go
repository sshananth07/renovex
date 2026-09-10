package supplieraccess_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// TestService_DeleteAllForCompany proves Task 1a's multi-collection
// guidance (spec §6.6): ONE Service.DeleteAllForCompany call removes
// AccessExchange, VerificationChallenge, VerificationDelivery,
// SupplierSession, and SessionInvitationBinding records for companyID.
//
// VerificationRateLimit is verified separately below
// (TestService_DeleteAllForCompany_RemovesRateLimitStateDerivedFromCompanysChallenges):
// that collection has no companyId field, so cleanup must re-derive the
// exact keyed hash from each of the company's own challenges instead of a
// plain company-scoped DeleteMany.
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	exchanges := supplieraccess.NewMongoAccessExchangeRepository(db)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	deliveries := supplieraccess.NewMongoVerificationDeliveryRepository(db)
	rates := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	sessions := supplieraccess.NewMongoSupplierSessionRepository(db)
	bindings := supplieraccess.NewMongoSessionInvitationBindingRepository(db)
	ctx := context.Background()
	for _, ensure := range []func(context.Context) error{
		exchanges.EnsureIndexes, challenges.EnsureIndexes, deliveries.EnsureIndexes,
		rates.EnsureIndexes, sessions.EnsureIndexes, bindings.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			t.Fatalf("ensuring indexes: %v", err)
		}
	}

	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(encodedChallengeKey(90))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}
	tokens := supplieraccess.CryptographicOpaqueTokenGenerator{}
	limiter := supplieraccess.NewVerificationRateLimiter(rates, tokens)
	codeKeys, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{1: encodedChallengeKey(91)})
	if err != nil {
		t.Fatalf("constructing code keyring: %v", err)
	}
	svc := supplieraccess.NewService(
		supplieraccess.WithAccessExchangeStore(exchanges),
		supplieraccess.WithVerificationStores(challenges, deliveries),
		supplieraccess.WithVerificationSecurity(codeKeys, fingerprinter, limiter),
		supplieraccess.WithSessionStores(sessions, bindings),
	)

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	seedCompany := func(companyID string) (
		supplieraccess.SupplierAccessExchange, supplieraccess.EmailVerificationChallenge) {

		exchange := exchangeFixture(now, companyID+"-exchange", strings.Repeat("a", 63)+companyID[len(companyID)-1:])
		exchange.CompanyID = companyID
		if _, err := exchanges.CreateExchange(ctx, exchange); err != nil {
			t.Fatalf("create %s exchange: %v", companyID, err)
		}

		challenge := challengeFixture(now, companyID+"-challenge", exchange.ID, companyID+"-operation")
		challenge.CompanyID = companyID
		if _, err := challenges.CreateChallenge(ctx, challenge); err != nil {
			t.Fatalf("create %s challenge: %v", companyID, err)
		}

		delivery := supplieraccess.VerificationDeliveryAttempt{
			ID: companyID + "-delivery", CompanyID: companyID,
			ChallengeID: challenge.ID, OperationID: companyID + "-delivery-op",
			Status:    supplieraccess.VerificationDeliveryPending,
			CreatedAt: now, UpdatedAt: now,
		}
		if _, err := deliveries.CreateAttempt(ctx, delivery); err != nil {
			t.Fatalf("create %s delivery: %v", companyID, err)
		}

		session := sessionFixture(now, companyID+"-session", strings.Repeat("b", 63)+companyID[len(companyID)-1:], challenge.ID)
		session.CompanyID = companyID
		if _, err := sessions.CreateSession(ctx, session); err != nil {
			t.Fatalf("create %s session: %v", companyID, err)
		}

		binding := bindingFixture(now, companyID+"-binding", challenge.InvitationID)
		binding.CompanyID = companyID
		binding.SupplierSessionID = session.ID
		if _, err := bindings.CreateBinding(ctx, binding); err != nil {
			t.Fatalf("create %s binding: %v", companyID, err)
		}

		return exchange, challenge
	}

	seedCompany("company_a")
	_, challengeB := seedCompany("company_b")

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	if _, err := exchanges.FindExchangeByTokenHash(ctx, strings.Repeat("a", 63)+"a"); err != supplieraccess.ErrAccessExchangeNotFound {
		t.Fatalf("expected company_a's exchange to be deleted, got %v", err)
	}
	if _, err := challenges.FindChallenge(ctx, "company_a-challenge"); err != supplieraccess.ErrVerificationChallengeNotFound {
		t.Fatalf("expected company_a's challenge to be deleted, got %v", err)
	}
	if _, err := deliveries.FindAttemptByOperation(ctx, "company_a", "company_a-challenge", "company_a-delivery-op"); err != supplieraccess.ErrVerificationDeliveryAttemptNotFound {
		t.Fatalf("expected company_a's delivery attempt to be deleted, got %v", err)
	}
	if _, err := sessions.FindSession(ctx, "company_a", "company_a-session"); err != supplieraccess.ErrSupplierSessionNotFound {
		t.Fatalf("expected company_a's session to be deleted, got %v", err)
	}
	if _, err := bindings.FindBinding(ctx, "company_a", "company_a-session", "invitation-1"); err != supplieraccess.ErrSessionInvitationBindingNotFound {
		t.Fatalf("expected company_a's binding to be deleted, got %v", err)
	}

	if _, err := exchanges.FindExchangeByTokenHash(ctx, strings.Repeat("a", 63)+"b"); err != nil {
		t.Fatalf("expected company_b's exchange to be untouched, got %v", err)
	}
	if _, err := challenges.FindChallenge(ctx, "company_b-challenge"); err != nil {
		t.Fatalf("expected company_b's challenge to be untouched, got %v", err)
	}
	if _, err := deliveries.FindAttemptByOperation(ctx, "company_b", "company_b-challenge", "company_b-delivery-op"); err != nil {
		t.Fatalf("expected company_b's delivery attempt to be untouched, got %v", err)
	}
	if _, err := sessions.FindSession(ctx, "company_b", "company_b-session"); err != nil {
		t.Fatalf("expected company_b's session to be untouched, got %v", err)
	}
	if _, err := bindings.FindBinding(ctx, "company_b", "company_b-session", "invitation-1"); err != nil {
		t.Fatalf("expected company_b's binding to be untouched, got %v", err)
	}
	_ = challengeB
}

// TestService_DeleteAllForCompany_RemovesRateLimitStateDerivedFromCompanysChallenges
// proves the hash-derived cleanup path: VerificationRateLimit stores no
// companyId, only an irreversible HMAC hash. Service.DeleteAllForCompany
// must re-derive the exact identity- and resend-scope hashes from the
// company's own challenges (which it deletes in the same call) and remove
// those specific rate-limit documents — a real record disappearing, not a
// zero-match no-op.
func TestService_DeleteAllForCompany_RemovesRateLimitStateDerivedFromCompanysChallenges(t *testing.T) {
	db := setupDB(t)
	exchanges := supplieraccess.NewMongoAccessExchangeRepository(db)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	deliveries := supplieraccess.NewMongoVerificationDeliveryRepository(db)
	rates := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	sessions := supplieraccess.NewMongoSupplierSessionRepository(db)
	bindings := supplieraccess.NewMongoSessionInvitationBindingRepository(db)
	ctx := context.Background()
	for _, ensure := range []func(context.Context) error{
		exchanges.EnsureIndexes, challenges.EnsureIndexes, deliveries.EnsureIndexes,
		rates.EnsureIndexes, sessions.EnsureIndexes, bindings.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			t.Fatalf("ensuring indexes: %v", err)
		}
	}

	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(encodedChallengeKey(92))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}
	tokens := supplieraccess.CryptographicOpaqueTokenGenerator{}
	limiter := supplieraccess.NewVerificationRateLimiter(rates, tokens)
	codeKeys, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{1: encodedChallengeKey(93)})
	if err != nil {
		t.Fatalf("constructing code keyring: %v", err)
	}
	svc := supplieraccess.NewService(
		supplieraccess.WithAccessExchangeStore(exchanges),
		supplieraccess.WithVerificationStores(challenges, deliveries),
		supplieraccess.WithVerificationSecurity(codeKeys, fingerprinter, limiter),
		supplieraccess.WithSessionStores(sessions, bindings),
	)

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	challengeA := challengeFixture(now, "company_a-challenge", "company_a-exchange", "company_a-operation")
	challengeA.CompanyID = "company_a"
	if _, err := challenges.CreateChallenge(ctx, challengeA); err != nil {
		t.Fatalf("create company_a challenge: %v", err)
	}
	challengeB := challengeFixture(now, "company_b-challenge", "company_b-exchange", "company_b-operation")
	challengeB.CompanyID = "company_b"
	if _, err := challenges.CreateChallenge(ctx, challengeB); err != nil {
		t.Fatalf("create company_b challenge: %v", err)
	}

	// Claim the same two rate scopes a real challenge-creation/resend flow
	// would, using the exact same fingerprint inputs the service holds.
	identityHashA, err := fingerprinter.FingerprintIdentity(
		challengeA.CompanyID, challengeA.SupplierID, challengeA.RecipientEmailNormalized,
		challengeA.InvitationID, challengeA.AccessGeneration)
	if err != nil {
		t.Fatalf("fingerprinting company_a identity: %v", err)
	}
	resendHashA, err := fingerprinter.FingerprintChallengeResend(challengeA.CompanyID, challengeA.ID)
	if err != nil {
		t.Fatalf("fingerprinting company_a resend: %v", err)
	}
	identityHashB, err := fingerprinter.FingerprintIdentity(
		challengeB.CompanyID, challengeB.SupplierID, challengeB.RecipientEmailNormalized,
		challengeB.InvitationID, challengeB.AccessGeneration)
	if err != nil {
		t.Fatalf("fingerprinting company_b identity: %v", err)
	}

	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity, identityHashA, "reservation-a-1", now); err != nil {
		t.Fatalf("claiming company_a identity rate: %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeResend, resendHashA, "reservation-a-2", now); err != nil {
		t.Fatalf("claiming company_a resend rate: %v", err)
	}
	if err := limiter.Claim(ctx, supplieraccess.VerificationRateScopeIdentity, identityHashB, "reservation-b-1", now); err != nil {
		t.Fatalf("claiming company_b identity rate: %v", err)
	}

	if _, err := rates.FindState(ctx, supplieraccess.VerificationRateScopeIdentity, identityHashA); err != nil {
		t.Fatalf("expected company_a identity rate state to exist before cleanup: %v", err)
	}
	if _, err := rates.FindState(ctx, supplieraccess.VerificationRateScopeResend, resendHashA); err != nil {
		t.Fatalf("expected company_a resend rate state to exist before cleanup: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	if _, err := rates.FindState(ctx, supplieraccess.VerificationRateScopeIdentity, identityHashA); err != supplieraccess.ErrVerificationRateLimitStateNotFound {
		t.Fatalf("expected company_a identity rate state to be deleted, got %v", err)
	}
	if _, err := rates.FindState(ctx, supplieraccess.VerificationRateScopeResend, resendHashA); err != supplieraccess.ErrVerificationRateLimitStateNotFound {
		t.Fatalf("expected company_a resend rate state to be deleted, got %v", err)
	}
	if _, err := rates.FindState(ctx, supplieraccess.VerificationRateScopeIdentity, identityHashB); err != nil {
		t.Fatalf("expected company_b identity rate state to be untouched, got %v", err)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupDB(t)
	exchanges := supplieraccess.NewMongoAccessExchangeRepository(db)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	deliveries := supplieraccess.NewMongoVerificationDeliveryRepository(db)
	rates := supplieraccess.NewMongoVerificationRateLimitRepository(db)
	sessions := supplieraccess.NewMongoSupplierSessionRepository(db)
	bindings := supplieraccess.NewMongoSessionInvitationBindingRepository(db)
	ctx := context.Background()
	for _, ensure := range []func(context.Context) error{
		exchanges.EnsureIndexes, challenges.EnsureIndexes, deliveries.EnsureIndexes,
		rates.EnsureIndexes, sessions.EnsureIndexes, bindings.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			t.Fatalf("ensuring indexes: %v", err)
		}
	}

	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(encodedChallengeKey(94))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}
	tokens := supplieraccess.CryptographicOpaqueTokenGenerator{}
	limiter := supplieraccess.NewVerificationRateLimiter(rates, tokens)
	codeKeys, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{1: encodedChallengeKey(95)})
	if err != nil {
		t.Fatalf("constructing code keyring: %v", err)
	}
	svc := supplieraccess.NewService(
		supplieraccess.WithAccessExchangeStore(exchanges),
		supplieraccess.WithVerificationStores(challenges, deliveries),
		supplieraccess.WithVerificationSecurity(codeKeys, fingerprinter, limiter),
		supplieraccess.WithSessionStores(sessions, bindings),
	)

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
