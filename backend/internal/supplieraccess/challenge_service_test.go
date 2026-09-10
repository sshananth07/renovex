package supplieraccess_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	platformmail "github.com/shananth/renovation-platform/backend/internal/platform/mail"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

type fakeVerificationMailer struct {
	mu       sync.Mutex
	messages []platformmail.Message
	err      error
}

func encodedChallengeKey(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytesOf(fill, 32))
}

func (f *fakeVerificationMailer) Send(
	_ context.Context, message platformmail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, message)
	return f.err
}

type staticInvitationAccess struct {
	snapshot supplieraccess.InvitationAccessSnapshot
}

func (s staticInvitationAccess) ResolveInvitationAccess(
	_ context.Context, _ string, _ time.Time,
) (supplieraccess.InvitationAccessSnapshot, bool, error) {
	return s.snapshot, true, nil
}

func (s staticInvitationAccess) ValidateInvitationAccess(
	_ context.Context, _, _ string, _ time.Time,
) (supplieraccess.InvitationAccessSnapshot, bool, error) {
	return s.snapshot, true, nil
}

func (s staticInvitationAccess) RecordInvitationViewed(
	_ context.Context, _, _ string, _ int64, _ time.Time,
) error {
	return nil
}

type countingVerificationChallengeStore struct {
	*supplieraccess.MongoVerificationChallengeRepository
	findCalls int
}

func (s *countingVerificationChallengeStore) FindChallenge(
	ctx context.Context, challengeID string,
) (supplieraccess.EmailVerificationChallenge, error) {
	s.findCalls++
	return s.MongoVerificationChallengeRepository.FindChallenge(ctx, challengeID)
}

type challengeServiceRig struct {
	db               *mongo.Database
	service          *supplieraccess.Service
	exchanges        *supplieraccess.MongoAccessExchangeRepository
	challenges       *supplieraccess.MongoVerificationChallengeRepository
	deliveries       *supplieraccess.MongoVerificationDeliveryRepository
	rates            *supplieraccess.MongoVerificationRateLimitRepository
	access           *fakeInvitationAccess
	mailer           *fakeVerificationMailer
	codeKeys         *secrets.SupplierVerificationCodeKeyring
	fingerprints     *secrets.SupplierRateLimitFingerprinter
	rawExchangeToken string
	exchange         supplieraccess.SupplierAccessExchange
}

func newChallengeServiceRig(t *testing.T, now time.Time) *challengeServiceRig {
	t.Helper()
	db := setupDB(t)
	rig := &challengeServiceRig{
		db:               db,
		exchanges:        supplieraccess.NewMongoAccessExchangeRepository(db),
		challenges:       supplieraccess.NewMongoVerificationChallengeRepository(db),
		deliveries:       supplieraccess.NewMongoVerificationDeliveryRepository(db),
		rates:            supplieraccess.NewMongoVerificationRateLimitRepository(db),
		mailer:           &fakeVerificationMailer{},
		rawExchangeToken: opaqueToken(111),
	}
	ctx := context.Background()
	for name, ensure := range map[string]func(context.Context) error{
		"exchange":  rig.exchanges.EnsureIndexes,
		"challenge": rig.challenges.EnsureIndexes,
		"delivery":  rig.deliveries.EnsureIndexes,
		"rate":      rig.rates.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			t.Fatalf("ensuring %s indexes: %v", name, err)
		}
	}
	rig.exchange = exchangeFixture(
		now, opaqueToken(101),
		supplieraccess.HashAccessExchangeToken(rig.rawExchangeToken))
	rig.exchange.NormalizedRecipientEmail = "recipient@supplier.test"
	if _, err := rig.exchanges.CreateExchange(ctx, rig.exchange); err != nil {
		t.Fatalf("creating exchange: %v", err)
	}
	rig.access = &fakeInvitationAccess{
		found: true,
		snapshot: supplieraccess.InvitationAccessSnapshot{
			CompanyID: rig.exchange.CompanyID, SupplierID: rig.exchange.SupplierID,
			InvitationID:              rig.exchange.InvitationID,
			NormalizedRecipientEmail:  rig.exchange.NormalizedRecipientEmail,
			AccessGeneration:          rig.exchange.AccessGeneration,
			CurrentIssuedRFQVersionID: "version-2",
		},
	}
	codeKeys, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{1: encodedChallengeKey(31)})
	if err != nil {
		t.Fatalf("constructing code keyring: %v", err)
	}
	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(
		encodedChallengeKey(32))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}
	rig.codeKeys = codeKeys
	rig.fingerprints = fingerprinter
	tokens := supplieraccess.CryptographicOpaqueTokenGenerator{}
	limiter := supplieraccess.NewVerificationRateLimiter(rig.rates, tokens)
	rig.service = supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(tokens),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(codeKeys, fingerprinter, limiter),
		supplieraccess.WithVerificationMailer(rig.mailer),
	)
	return rig
}

type recordingRateClaimer struct {
	scopes []supplieraccess.VerificationRateLimitScope
	err    error
}

func (r *recordingRateClaimer) Claim(_ context.Context,
	scope supplieraccess.VerificationRateLimitScope, _, _ string, _ time.Time) error {
	r.scopes = append(r.scopes, scope)
	return r.err
}

func TestChallengeCreationClaimsClientScopeBeforeVictimIdentityScope(t *testing.T) {
	now := time.Date(2026, 7, 30, 11, 30, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	rates := &recordingRateClaimer{
		err: &supplieraccess.VerificationRateLimitExceeded{
			RetryAt: now.Add(time.Minute),
		},
	}
	service := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, rates),
		supplieraccess.WithVerificationMailer(rig.mailer),
	)

	_, err := service.CreateChallenge(context.Background(),
		supplieraccess.CreateChallengeInput{
			ExchangeToken: rig.rawExchangeToken,
			OperationID:   "challenge-operation-1",
			ClientAddress: netip.MustParseAddr("198.51.100.20"),
			RequestedAt:   now,
		})
	if !errors.Is(err, supplieraccess.ErrVerificationRateLimited) {
		t.Fatalf("client rejection error = %v", err)
	}
	if len(rates.scopes) != 1 ||
		rates.scopes[0] != supplieraccess.VerificationRateScopeClient {
		t.Fatalf("rate scopes = %v, want only client_address", rates.scopes)
	}
	count, err := rig.db.Collection("email_verification_challenges").
		CountDocuments(context.Background(), bson.M{})
	if err != nil || count != 0 {
		t.Fatalf("challenge count/error = %d/%v, want 0/nil", count, err)
	}
}

func TestCreateChallengePersistsVerifierBeforeSendingAndConsumesExchange(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	ctx := context.Background()

	result, err := rig.service.CreateChallenge(ctx, supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "challenge-operation-1",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now,
	})
	if err != nil {
		t.Fatalf("creating challenge: %v", err)
	}
	if result.ChallengeID == "" ||
		result.DeliveryStatus != supplieraccess.VerificationDeliverySent ||
		!result.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("challenge result = %#v", result)
	}
	challenge, err := rig.challenges.FindChallenge(ctx, result.ChallengeID)
	if err != nil {
		t.Fatalf("loading challenge: %v", err)
	}
	if challenge.AttemptsRemaining != 5 || challenge.CodeKeyVersion != 1 ||
		challenge.CodeVerifier == "" || challenge.Revision != 1 {
		t.Fatalf("persisted challenge = %#v", challenge)
	}
	if rig.access.validateCalls != 1 ||
		rig.access.gotValidate.companyID != rig.exchange.CompanyID ||
		rig.access.gotValidate.invitationID != rig.exchange.InvitationID ||
		!rig.access.gotValidate.at.Equal(now) {
		t.Fatalf("invitation revalidation = %#v", rig.access.gotValidate)
	}
	if len(rig.mailer.messages) != 1 {
		t.Fatalf("mail count = %d, want 1", len(rig.mailer.messages))
	}
	code := regexp.MustCompile(`[0-9]{6}`).FindString(rig.mailer.messages[0].Body)
	if code == "" {
		t.Fatalf("mail body has no six-digit code: %q", rig.mailer.messages[0].Body)
	}
	if rig.mailer.messages[0].To != rig.exchange.NormalizedRecipientEmail {
		t.Fatalf("mail recipient = %q", rig.mailer.messages[0].To)
	}
	if challenge.CodeVerifier == code {
		t.Fatal("raw code was persisted in the verifier field")
	}

	var rawChallenge bson.M
	if err := rig.db.Collection("email_verification_challenges").FindOne(
		ctx, bson.M{"_id": challenge.ID}).Decode(&rawChallenge); err != nil {
		t.Fatalf("loading raw challenge document: %v", err)
	}
	encoded, _ := json.Marshal(rawChallenge)
	if regexp.MustCompile(`"` + code + `"`).Match(encoded) {
		t.Fatalf("raw code persisted in challenge document: %s", encoded)
	}
	delivery, err := rig.deliveries.FindAttemptByOperation(
		ctx, challenge.CompanyID, challenge.ID, challenge.ChallengeOperationID)
	if err != nil || delivery.Status != supplieraccess.VerificationDeliverySent {
		t.Fatalf("delivery/error = %#v/%v", delivery, err)
	}
	exchange, err := rig.exchanges.FindExchangeByTokenHash(
		ctx, rig.exchange.ExchangeTokenHash)
	if err != nil || exchange.ChallengeID == nil ||
		*exchange.ChallengeID != challenge.ID || exchange.ConsumedAt == nil {
		t.Fatalf("consumed exchange/error = %#v/%v", exchange, err)
	}
}

func TestResendUsesSameCodeWithoutChangingChallengeAndIsOperationIdempotent(t *testing.T) {
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	ctx := context.Background()
	created, err := rig.service.CreateChallenge(ctx, supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "challenge-operation-1",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now,
	})
	if err != nil {
		t.Fatalf("creating challenge: %v", err)
	}
	before, _ := rig.challenges.FindChallenge(ctx, created.ChallengeID)
	originalBody := rig.mailer.messages[0].Body

	if _, err := rig.service.ResendChallenge(ctx, supplieraccess.ResendChallengeInput{
		ChallengeID: created.ChallengeID,
		OperationID: "resend-operation-1",
		RequestedAt: now.Add(59 * time.Second),
	}); !errors.Is(err, supplieraccess.ErrVerificationRateLimited) {
		t.Fatalf("resend before cooldown error = %v", err)
	}
	if len(rig.mailer.messages) != 1 {
		t.Fatal("cooldown rejection sent email")
	}
	result, err := rig.service.ResendChallenge(ctx, supplieraccess.ResendChallengeInput{
		ChallengeID: created.ChallengeID,
		OperationID: "resend-operation-1",
		RequestedAt: now.Add(time.Minute),
	})
	if err != nil || result.DeliveryStatus != supplieraccess.VerificationDeliverySent {
		t.Fatalf("resend result/error = %#v/%v", result, err)
	}
	if len(rig.mailer.messages) != 2 ||
		rig.mailer.messages[1].Body != originalBody {
		t.Fatalf("resend changed code/body: %#v", rig.mailer.messages)
	}
	after, _ := rig.challenges.FindChallenge(ctx, created.ChallengeID)
	if after.CodeVerifier != before.CodeVerifier ||
		after.AttemptsRemaining != before.AttemptsRemaining ||
		!after.ExpiresAt.Equal(before.ExpiresAt) ||
		after.Revision != before.Revision {
		t.Fatalf("resend mutated challenge:\nbefore=%#v\nafter=%#v", before, after)
	}

	if _, err := rig.service.ResendChallenge(ctx, supplieraccess.ResendChallengeInput{
		ChallengeID: created.ChallengeID,
		OperationID: "resend-operation-1",
		RequestedAt: now.Add(61 * time.Second),
	}); err != nil {
		t.Fatalf("same resend operation retry: %v", err)
	}
	if len(rig.mailer.messages) != 2 {
		t.Fatalf("same operation resent mail; count = %d", len(rig.mailer.messages))
	}
}

func TestResendRejectsMalformedChallengeHandleBeforeMongoDB(t *testing.T) {
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	challenges := &countingVerificationChallengeStore{
		MongoVerificationChallengeRepository: rig.challenges,
	}
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	service := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, limiter),
		supplieraccess.WithVerificationMailer(rig.mailer),
	)

	_, malformedErr := service.ResendChallenge(context.Background(),
		supplieraccess.ResendChallengeInput{
			ChallengeID: "not-a-canonical-handle",
			OperationID: "resend-malformed",
			RequestedAt: now,
		})
	if !errors.Is(malformedErr, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("malformed challenge error = %v", malformedErr)
	}
	if challenges.findCalls != 0 {
		t.Fatalf("malformed challenge reached MongoDB %d times",
			challenges.findCalls)
	}

	_, unknownErr := service.ResendChallenge(context.Background(),
		supplieraccess.ResendChallengeInput{
			ChallengeID: opaqueToken(231),
			OperationID: "resend-unknown",
			RequestedAt: now,
		})
	if !errors.Is(unknownErr, supplieraccess.ErrInvalidSupplierCredential) ||
		challenges.findCalls != 1 {
		t.Fatalf("unknown challenge error/find calls = %v/%d",
			unknownErr, challenges.findCalls)
	}
}

func TestChallengeCreationRecoversCrashAfterChallengeInsert(t *testing.T) {
	now := time.Date(2026, 7, 30, 14, 0, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	ctx := context.Background()
	input := supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "challenge-operation-1",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now,
	}
	first, err := rig.service.CreateChallenge(ctx, input)
	if err != nil {
		t.Fatalf("creating baseline challenge: %v", err)
	}

	// Model a process crash after challenge insertion but before exchange
	// consumption and delivery-intent persistence.
	if _, err := rig.db.Collection("supplier_access_exchanges").UpdateOne(
		ctx, bson.M{"_id": rig.exchange.ID},
		bson.M{"$unset": bson.M{"consumedAt": "", "challengeId": ""}}); err != nil {
		t.Fatalf("rewinding exchange to crash point: %v", err)
	}
	if _, err := rig.db.Collection("verification_delivery_attempts").DeleteMany(
		ctx, bson.M{"challengeId": first.ChallengeID}); err != nil {
		t.Fatalf("removing post-crash delivery artifact: %v", err)
	}
	rig.mailer.messages = nil
	input.RequestedAt = now.Add(time.Second)

	recovered, err := rig.service.CreateChallenge(ctx, input)
	if err != nil {
		t.Fatalf("recovering challenge: %v", err)
	}
	if recovered.ChallengeID != first.ChallengeID ||
		len(rig.mailer.messages) != 1 {
		t.Fatalf("recovery result/messages = %#v/%d", recovered, len(rig.mailer.messages))
	}
	count, err := rig.db.Collection("email_verification_challenges").
		CountDocuments(ctx, bson.M{"exchangeId": rig.exchange.ID})
	if err != nil || count != 1 {
		t.Fatalf("challenge count/error = %d/%v, want 1/nil", count, err)
	}
	exchange, _ := rig.exchanges.FindExchangeByTokenHash(
		ctx, rig.exchange.ExchangeTokenHash)
	if exchange.ConsumedAt == nil || exchange.ChallengeID == nil ||
		*exchange.ChallengeID != first.ChallengeID {
		t.Fatalf("exchange did not converge: %#v", exchange)
	}
}

func TestChallengeCreationIdempotentlyRecoversAfterExchangeExpiry(t *testing.T) {
	// Keep the physical TTL deadline ahead of wall-clock time. Authorization
	// uses the supplied logical instant; MongoDB TTL remains housekeeping.
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	ctx := context.Background()
	input := supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "challenge-operation-expiry-recovery",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now,
	}
	first, err := rig.service.CreateChallenge(ctx, input)
	if err != nil {
		t.Fatalf("creating challenge: %v", err)
	}
	// The exchange expires before its derived challenge here. Once challenge
	// creation is authoritative, same-operation recovery must follow that
	// challenge rather than resurrecting or re-consuming the exchange.
	exchangeExpiry := now.Add(time.Minute)
	if _, err := rig.db.Collection("supplier_access_exchanges").UpdateOne(
		ctx, bson.M{"_id": rig.exchange.ID},
		bson.M{"$set": bson.M{"expiresAt": exchangeExpiry}}); err != nil {
		t.Fatalf("shortening exchange expiry: %v", err)
	}
	input.RequestedAt = exchangeExpiry

	recovered, err := rig.service.CreateChallenge(ctx, input)
	if err != nil {
		t.Fatalf("recovering at exact exchange expiry: %v", err)
	}
	if recovered.ChallengeID != first.ChallengeID ||
		len(rig.mailer.messages) != 1 {
		t.Fatalf("recovery result/messages = %#v/%d",
			recovered, len(rig.mailer.messages))
	}
}

func TestConcurrentDifferentChallengeOperationsProduceOneWinner(t *testing.T) {
	now := time.Date(2026, 7, 30, 14, 45, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	access := staticInvitationAccess{snapshot: rig.access.snapshot}
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	mailers := []*fakeVerificationMailer{{}, {}}
	services := make([]*supplieraccess.Service, 2)
	for index := range services {
		services[index] = supplieraccess.NewService(
			supplieraccess.WithInvitationAccess(access, access),
			supplieraccess.WithAccessExchangeStore(rig.exchanges),
			supplieraccess.WithOpaqueTokenGenerator(
				supplieraccess.CryptographicOpaqueTokenGenerator{}),
			supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
			supplieraccess.WithVerificationSecurity(
				rig.codeKeys, rig.fingerprints, limiter),
			supplieraccess.WithVerificationMailer(mailers[index]),
		)
	}

	start := make(chan struct{})
	errs := make(chan error, len(services))
	var successful atomic.Int32
	var wait sync.WaitGroup
	for index, service := range services {
		wait.Add(1)
		go func(index int, service *supplieraccess.Service) {
			defer wait.Done()
			<-start
			_, err := service.CreateChallenge(context.Background(),
				supplieraccess.CreateChallengeInput{
					ExchangeToken: rig.rawExchangeToken,
					OperationID:   "concurrent-operation-" + string(rune('a'+index)),
					ClientAddress: netip.MustParseAddr("198.51.100.20"),
					RequestedAt:   now,
				})
			if err == nil {
				successful.Add(1)
			} else {
				errs <- err
			}
		}(index, service)
	}
	close(start)
	wait.Wait()
	close(errs)

	if successful.Load() != 1 || len(errs) != 1 {
		t.Fatalf("successful/errors = %d/%d, want 1/1",
			successful.Load(), len(errs))
	}
	for err := range errs {
		if !errors.Is(err, supplieraccess.ErrChallengeOperationConflict) {
			t.Fatalf("losing operation error = %v", err)
		}
	}
	count, err := rig.db.Collection("email_verification_challenges").
		CountDocuments(context.Background(), bson.M{"exchangeId": rig.exchange.ID})
	if err != nil || count != 1 {
		t.Fatalf("challenge count/error = %d/%v, want 1/nil", count, err)
	}
	sent := len(mailers[0].messages) + len(mailers[1].messages)
	if sent != 1 {
		t.Fatalf("physical mail count = %d, want 1", sent)
	}
}

func TestPendingChallengeDeliveryRecoverySendsThePersistedIntent(t *testing.T) {
	now := time.Date(2026, 7, 30, 14, 50, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	ctx := context.Background()
	input := supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "challenge-operation-pending-recovery",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now,
	}
	first, err := rig.service.CreateChallenge(ctx, input)
	if err != nil {
		t.Fatalf("creating challenge: %v", err)
	}
	if _, err := rig.db.Collection("verification_delivery_attempts").DeleteMany(
		ctx, bson.M{"challengeId": first.ChallengeID}); err != nil {
		t.Fatalf("removing completed delivery: %v", err)
	}
	pending := verificationDeliveryFixture(
		now, opaqueToken(151), input.OperationID)
	pending.ChallengeID = first.ChallengeID
	if _, err := rig.deliveries.CreateAttempt(ctx, pending); err != nil {
		t.Fatalf("staging pending delivery: %v", err)
	}
	rig.mailer.messages = nil
	input.RequestedAt = now.Add(time.Second)

	recovered, err := rig.service.CreateChallenge(ctx, input)
	if err != nil || recovered.DeliveryStatus !=
		supplieraccess.VerificationDeliverySent {
		t.Fatalf("pending recovery result/error = %#v/%v", recovered, err)
	}
	if len(rig.mailer.messages) != 1 {
		t.Fatalf("pending recovery mail count = %d, want 1",
			len(rig.mailer.messages))
	}
	attempt, err := rig.deliveries.FindAttemptByOperation(
		ctx, rig.exchange.CompanyID, first.ChallengeID, input.OperationID)
	if err != nil || attempt.Status != supplieraccess.VerificationDeliverySent {
		t.Fatalf("recovered attempt/error = %#v/%v", attempt, err)
	}
}

func TestFailedInitialDeliveryRequiresExplicitResendWithTheSameCode(t *testing.T) {
	now := time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	ctx := context.Background()
	providerErr := errors.New("provider unavailable with unrestricted detail")
	rig.mailer.err = providerErr
	input := supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "challenge-operation-1",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now,
	}

	first, err := rig.service.CreateChallenge(ctx, input)
	if !errors.Is(err, supplieraccess.ErrVerificationMailDeliveryFailed) {
		t.Fatalf("initial send error = %v", err)
	}
	if first.ChallengeID == "" || len(rig.mailer.messages) != 1 {
		t.Fatalf("failed result/messages = %#v/%d", first, len(rig.mailer.messages))
	}
	originalBody := rig.mailer.messages[0].Body
	attempt, findErr := rig.deliveries.FindAttemptByOperation(
		ctx, rig.exchange.CompanyID, first.ChallengeID, input.OperationID)
	if findErr != nil ||
		attempt.Status != supplieraccess.VerificationDeliveryFailed ||
		attempt.FailureCode == nil || string(*attempt.FailureCode) != "send_failed" {
		t.Fatalf("failed attempt/error = %#v/%v", attempt, findErr)
	}
	rawAttempt := bson.M{}
	if err := rig.db.Collection("verification_delivery_attempts").FindOne(
		ctx, bson.M{"_id": attempt.ID}).Decode(&rawAttempt); err != nil {
		t.Fatalf("loading raw delivery attempt: %v", err)
	}
	rawJSON, _ := json.Marshal(rawAttempt)
	if regexp.MustCompile("provider unavailable|unrestricted detail").Match(rawJSON) {
		t.Fatalf("provider detail persisted: %s", rawJSON)
	}

	rig.mailer.err = nil
	input.RequestedAt = now.Add(time.Second)
	if _, err := rig.service.CreateChallenge(ctx, input); !errors.Is(
		err, supplieraccess.ErrVerificationMailDeliveryFailed) {
		t.Fatalf("same create operation error = %v", err)
	}
	if len(rig.mailer.messages) != 1 {
		t.Fatal("same failed operation silently resent email")
	}
	resent, err := rig.service.ResendChallenge(ctx, supplieraccess.ResendChallengeInput{
		ChallengeID: first.ChallengeID,
		OperationID: "resend-operation-1",
		RequestedAt: now.Add(time.Minute),
	})
	if err != nil || resent.DeliveryStatus != supplieraccess.VerificationDeliverySent {
		t.Fatalf("explicit resend result/error = %#v/%v", resent, err)
	}
	if len(rig.mailer.messages) != 2 ||
		rig.mailer.messages[1].Body != originalBody {
		t.Fatalf("explicit resend changed the recoverable code: %#v", rig.mailer.messages)
	}
}

func TestNewerChallengeImmediatelyInvalidatesOlderChallengeResend(t *testing.T) {
	now := time.Date(2026, 7, 30, 16, 0, 0, 0, time.UTC)
	rig := newChallengeServiceRig(t, now)
	ctx := context.Background()
	older, err := rig.service.CreateChallenge(ctx, supplieraccess.CreateChallengeInput{
		ExchangeToken: rig.rawExchangeToken,
		OperationID:   "challenge-operation-1",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now,
	})
	if err != nil {
		t.Fatalf("creating older challenge: %v", err)
	}

	newRawExchangeToken := opaqueToken(121)
	newExchange := exchangeFixture(now.Add(time.Minute), opaqueToken(122),
		supplieraccess.HashAccessExchangeToken(newRawExchangeToken))
	newExchange.NormalizedRecipientEmail = rig.exchange.NormalizedRecipientEmail
	if _, err := rig.exchanges.CreateExchange(ctx, newExchange); err != nil {
		t.Fatalf("creating newer exchange: %v", err)
	}
	newer, err := rig.service.CreateChallenge(ctx, supplieraccess.CreateChallengeInput{
		ExchangeToken: newRawExchangeToken,
		OperationID:   "challenge-operation-2",
		ClientAddress: netip.MustParseAddr("198.51.100.20"),
		RequestedAt:   now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("creating newer challenge: %v", err)
	}
	if newer.ChallengeID == older.ChallengeID {
		t.Fatal("new exchange reused the older challenge")
	}
	if _, err := rig.service.ResendChallenge(ctx, supplieraccess.ResendChallengeInput{
		ChallengeID: older.ChallengeID,
		OperationID: "resend-old",
		RequestedAt: now.Add(2 * time.Minute),
	}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("older challenge resend error = %v", err)
	}
}
