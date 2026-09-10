package supplieraccess_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

type verificationServiceRig struct {
	*challengeServiceRig
	sessions    *supplieraccess.MongoSupplierSessionRepository
	bindings    *supplieraccess.MongoSessionInvitationBindingRepository
	sessionKeys *secrets.SupplierSessionTokenKeyring
	service     *supplieraccess.Service
}

type mappedInvitationAccess struct {
	snapshots map[string]supplieraccess.InvitationAccessSnapshot
}

func (m mappedInvitationAccess) ResolveInvitationAccess(
	_ context.Context, _ string, _ time.Time,
) (supplieraccess.InvitationAccessSnapshot, bool, error) {
	return supplieraccess.InvitationAccessSnapshot{}, false, nil
}

func (m mappedInvitationAccess) ValidateInvitationAccess(
	_ context.Context, companyID, invitationID string, _ time.Time,
) (supplieraccess.InvitationAccessSnapshot, bool, error) {
	snapshot, found := m.snapshots[invitationID]
	return snapshot, found && snapshot.CompanyID == companyID, nil
}

func (m mappedInvitationAccess) RecordInvitationViewed(
	_ context.Context, _, _ string, _ int64, _ time.Time) error {
	return nil
}

type barrierSessionStore struct {
	*supplieraccess.MongoSupplierSessionRepository
	tokenHash string
	arrivals  atomic.Int32
	release   chan struct{}
	closeOnce sync.Once
}

func (s *barrierSessionStore) FindSessionByTokenHash(
	ctx context.Context, tokenHash string,
) (supplieraccess.SupplierSession, error) {
	session, err := s.MongoSupplierSessionRepository.FindSessionByTokenHash(
		ctx, tokenHash)
	if err != nil || tokenHash != s.tokenHash {
		return session, err
	}
	if arrival := s.arrivals.Add(1); arrival <= 2 {
		if arrival == 2 {
			s.closeOnce.Do(func() { close(s.release) })
		}
		select {
		case <-s.release:
		case <-ctx.Done():
			return supplieraccess.SupplierSession{}, ctx.Err()
		case <-time.After(5 * time.Second):
			return supplieraccess.SupplierSession{},
				errors.New("test session-resolution barrier timed out")
		}
	}
	return session, nil
}

func newVerificationServiceRig(t *testing.T, now time.Time) *verificationServiceRig {
	t.Helper()
	challengeRig := newChallengeServiceRig(t, now)
	rig := &verificationServiceRig{
		challengeServiceRig: challengeRig,
		sessions: supplieraccess.NewMongoSupplierSessionRepository(
			challengeRig.db),
		bindings: supplieraccess.NewMongoSessionInvitationBindingRepository(
			challengeRig.db),
	}
	ctx := context.Background()
	if err := rig.sessions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring session indexes: %v", err)
	}
	if err := rig.bindings.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring binding indexes: %v", err)
	}
	sessionKeys, err := secrets.NewSupplierSessionTokenKeyring(
		1, map[int]string{
			1: base64.StdEncoding.EncodeToString(bytesOf(91, 32)),
		})
	if err != nil {
		t.Fatalf("constructing session keyring: %v", err)
	}
	rig.sessionKeys = sessionKeys
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	rig.service = supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, limiter),
		supplieraccess.WithVerificationMailer(rig.mailer),
		supplieraccess.WithSessionStores(rig.sessions, rig.bindings),
		supplieraccess.WithSessionSecurity(sessionKeys),
	)
	return rig
}

func verificationServiceWithAccess(t *testing.T, rig *verificationServiceRig,
	access mappedInvitationAccess,
	sessions supplieraccess.SupplierSessionStore) *supplieraccess.Service {
	t.Helper()
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	return supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(access, access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, limiter),
		supplieraccess.WithVerificationMailer(rig.mailer),
		supplieraccess.WithSessionStores(sessions, rig.bindings),
		supplieraccess.WithSessionSecurity(rig.sessionKeys),
	)
}

func (r *verificationServiceRig) createChallenge(
	t *testing.T, createdAt time.Time) (string, string) {
	t.Helper()
	result, err := r.service.CreateChallenge(context.Background(),
		supplieraccess.CreateChallengeInput{
			ExchangeToken: r.rawExchangeToken,
			OperationID:   "challenge-operation-for-verification",
			ClientAddress: mustAddress("198.51.100.31"),
			RequestedAt:   createdAt,
		})
	if err != nil {
		t.Fatalf("creating verification challenge: %v", err)
	}
	code := regexp.MustCompile(`[0-9]{6}`).FindString(
		r.mailer.messages[len(r.mailer.messages)-1].Body)
	if code == "" {
		t.Fatal("verification email did not contain a six-digit code")
	}
	return result.ChallengeID, code
}

func (r *verificationServiceRig) createAdditionalChallenge(
	t *testing.T, createdAt time.Time, fill byte,
	operationID string) (string, string) {
	return r.createChallengeForInvitation(t, createdAt, fill, operationID,
		r.exchange.InvitationID, r.access.snapshot.AccessGeneration)
}

func (r *verificationServiceRig) createChallengeForInvitation(
	t *testing.T, createdAt time.Time, fill byte, operationID,
	invitationID string, accessGeneration int64) (string, string) {
	t.Helper()
	rawExchangeToken := opaqueToken(fill)
	exchange := exchangeFixture(createdAt, opaqueToken(fill+1),
		supplieraccess.HashAccessExchangeToken(rawExchangeToken))
	exchange.NormalizedRecipientEmail = r.exchange.NormalizedRecipientEmail
	exchange.InvitationID = invitationID
	exchange.AccessGeneration = accessGeneration
	if _, err := r.exchanges.CreateExchange(
		context.Background(), exchange); err != nil {
		t.Fatalf("creating additional exchange: %v", err)
	}
	result, err := r.service.CreateChallenge(context.Background(),
		supplieraccess.CreateChallengeInput{
			ExchangeToken: rawExchangeToken,
			OperationID:   operationID,
			ClientAddress: mustAddress("198.51.100.31"),
			RequestedAt:   createdAt,
		})
	if err != nil {
		t.Fatalf("creating additional challenge: %v", err)
	}
	code := regexp.MustCompile(`[0-9]{6}`).FindString(
		r.mailer.messages[len(r.mailer.messages)-1].Body)
	if code == "" {
		t.Fatal("additional verification email had no six-digit code")
	}
	return result.ChallengeID, code
}

func mustAddress(value string) (address netip.Addr) {
	return netip.MustParseAddr(value)
}

func collectionCount(t *testing.T, db *mongo.Database,
	collection string) int64 {
	t.Helper()
	count, err := db.Collection(collection).CountDocuments(
		context.Background(), bson.M{})
	if err != nil {
		t.Fatalf("counting %s: %v", collection, err)
	}
	return count
}

func TestVerifyChallengeCountsEveryInvalidFormatAndLocksOnFifthFailure(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, _ := rig.createChallenge(t, now)
	submissions := []string{
		"00000",
		"１２３４５６",
		"123 45",
		"999999\n",
		"abcdef",
	}
	for index, code := range submissions {
		_, err := rig.service.VerifyChallenge(context.Background(),
			supplieraccess.VerifyChallengeInput{
				ChallengeID: challengeID,
				Code:        code,
				OperationID: "verify-invalid-format",
				VerifiedAt:  now.Add(time.Duration(index+1) * time.Second),
			})
		if !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
			t.Fatalf("submission %d error = %v", index+1, err)
		}
	}

	locked, err := rig.challenges.FindChallenge(
		context.Background(), challengeID)
	if err != nil {
		t.Fatalf("loading locked challenge: %v", err)
	}
	if locked.AttemptsRemaining != 0 || locked.LockedAt == nil ||
		locked.Revision != 6 {
		t.Fatalf("locked challenge = %#v", locked)
	}
	if count := collectionCount(t, rig.db, "supplier_sessions"); count != 0 {
		t.Fatalf("invalid codes created %d sessions", count)
	}
	if count := collectionCount(
		t, rig.db, "supplier_session_invitation_bindings"); count != 0 {
		t.Fatalf("invalid codes created %d bindings", count)
	}

	_, err = rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: challengeID,
			Code:        "123456",
			OperationID: "verify-after-lock",
			VerifiedAt:  now.Add(6 * time.Second),
		})
	if !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("post-lock error = %v", err)
	}
	unchanged, _ := rig.challenges.FindChallenge(
		context.Background(), challengeID)
	if unchanged.Revision != locked.Revision ||
		unchanged.AttemptsRemaining != locked.AttemptsRemaining {
		t.Fatalf("post-lock request mutated challenge: %#v", unchanged)
	}
}

func TestVerifyChallengeCreatesOneHashOnlySessionAndGenerationBinding(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	verifiedAt := now.Add(time.Minute)

	result, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: challengeID,
			Code:        code,
			OperationID: "verify-new-browser",
			VerifiedAt:  verifiedAt,
		})
	if err != nil {
		t.Fatalf("verifying challenge: %v", err)
	}
	if !supplieraccess.IsCanonicalOpaqueCredential(result.SessionID) ||
		!supplieraccess.IsCanonicalOpaqueCredential(result.SessionToken) ||
		!supplieraccess.IsCanonicalOpaqueCredential(result.CSRFToken) ||
		result.InvitationID != rig.exchange.InvitationID ||
		result.AccessGeneration != rig.exchange.AccessGeneration ||
		!result.SlidingExpiresAt.Equal(verifiedAt.Add(30*24*time.Hour)) ||
		!result.AbsoluteExpiresAt.Equal(verifiedAt.Add(90*24*time.Hour)) {
		t.Fatalf("verification result = %#v", result)
	}

	tokenHash := secrets.HashSupplierSessionToken(result.SessionToken)
	session, err := rig.sessions.FindSessionByTokenHash(
		context.Background(), tokenHash)
	if err != nil {
		t.Fatalf("loading session by returned token: %v", err)
	}
	if session.ID != result.SessionID || session.TokenGeneration != 1 ||
		session.TokenKeyVersion != rig.sessionKeys.ActiveVersion() ||
		session.CreatedFromChallengeID != challengeID ||
		session.LastVerifiedChallengeID != challengeID ||
		!session.LastVerifiedAt.Equal(verifiedAt) ||
		!session.SlidingExpiresAt.Equal(result.SlidingExpiresAt) ||
		!session.AbsoluteExpiresAt.Equal(result.AbsoluteExpiresAt) ||
		session.Revision != 1 {
		t.Fatalf("created session = %#v", session)
	}
	binding, err := rig.bindings.FindBinding(context.Background(),
		session.CompanyID, session.ID, rig.exchange.InvitationID)
	if err != nil {
		t.Fatalf("loading created binding: %v", err)
	}
	if binding.SupplierID != session.SupplierID ||
		binding.NormalizedRecipientEmail !=
			session.RecipientEmailNormalized ||
		binding.AccessGeneration != rig.exchange.AccessGeneration ||
		binding.Revision != 1 {
		t.Fatalf("created binding = %#v", binding)
	}
	consumed, err := rig.challenges.FindChallenge(
		context.Background(), challengeID)
	if err != nil {
		t.Fatalf("loading consumed challenge: %v", err)
	}
	if consumed.ConsumedAt == nil ||
		consumed.ConsumedOperationID != "verify-new-browser" ||
		consumed.SupplierSessionID != session.ID ||
		consumed.TargetTokenGeneration != 1 ||
		consumed.SessionTokenKeyVersion != session.TokenKeyVersion {
		t.Fatalf("challenge recovery target = %#v", consumed)
	}

	var persisted []byte
	for _, collection := range []string{
		"supplier_sessions", "supplier_session_invitation_bindings",
		"email_verification_challenges",
	} {
		var documents []bson.M
		cursor, findErr := rig.db.Collection(collection).Find(
			context.Background(), bson.M{})
		if findErr != nil {
			t.Fatalf("reading %s: %v", collection, findErr)
		}
		if findErr = cursor.All(context.Background(), &documents); findErr != nil {
			t.Fatalf("decoding %s: %v", collection, findErr)
		}
		encoded, _ := json.Marshal(documents)
		persisted = append(persisted, encoded...)
	}
	for _, secretValue := range []string{result.SessionToken, result.CSRFToken} {
		if strings.Contains(string(persisted), secretValue) {
			t.Fatalf("raw session credential persisted: %q", secretValue)
		}
	}
}

func TestVerifyChallengeSameOperationRecoveryRequiresCodeAndReturnsSameToken(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	input := supplieraccess.VerifyChallengeInput{
		ChallengeID: challengeID,
		Code:        code,
		OperationID: "verify-recover",
		VerifiedAt:  now.Add(time.Minute),
	}
	first, err := rig.service.VerifyChallenge(context.Background(), input)
	if err != nil {
		t.Fatalf("first verification: %v", err)
	}
	input.VerifiedAt = input.VerifiedAt.Add(time.Second)
	recovered, err := rig.service.VerifyChallenge(context.Background(), input)
	if err != nil {
		t.Fatalf("same-operation recovery: %v", err)
	}
	if recovered.SessionID != first.SessionID ||
		recovered.SessionToken != first.SessionToken ||
		recovered.CSRFToken != first.CSRFToken ||
		!recovered.SlidingExpiresAt.Equal(first.SlidingExpiresAt) ||
		!recovered.AbsoluteExpiresAt.Equal(first.AbsoluteExpiresAt) {
		t.Fatalf("recovery changed result:\nfirst=%#v\nretry=%#v",
			first, recovered)
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 1 ||
		collectionCount(
			t, rig.db, "supplier_session_invitation_bindings") != 1 {
		t.Fatal("same-operation recovery created duplicate session or binding")
	}

	input.Code = "000000"
	if input.Code == code {
		input.Code = "999999"
	}
	if _, err := rig.service.VerifyChallenge(
		context.Background(), input); !errors.Is(
		err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("same operation with wrong code error = %v", err)
	}
	input.Code = code
	input.OperationID = "different-operation"
	if _, err := rig.service.VerifyChallenge(
		context.Background(), input); !errors.Is(
		err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("different operation recovery error = %v", err)
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 1 {
		t.Fatal("invalid recovery created another session")
	}
}

func TestVerifyChallengeRotatesOnlyThePresentedMatchingSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	firstChallengeID, firstCode := rig.createChallenge(t, now)
	first, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: firstChallengeID,
			Code:        firstCode,
			OperationID: "verify-first-session",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("creating first session: %v", err)
	}
	secondChallengeID, secondCode := rig.createAdditionalChallenge(
		t, now.Add(time.Minute), 171, "challenge-operation-reverify")
	reverifiedAt := now.Add(2 * time.Minute)
	rotated, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID:           secondChallengeID,
			Code:                  secondCode,
			OperationID:           "verify-presented-session",
			PresentedSessionToken: first.SessionToken,
			VerifiedAt:            reverifiedAt,
		})
	if err != nil {
		t.Fatalf("re-verifying presented session: %v", err)
	}
	if rotated.SessionID != first.SessionID ||
		rotated.SessionToken == first.SessionToken ||
		rotated.CSRFToken == first.CSRFToken ||
		!rotated.SlidingExpiresAt.Equal(
			reverifiedAt.Add(30*24*time.Hour)) ||
		!rotated.AbsoluteExpiresAt.Equal(
			reverifiedAt.Add(90*24*time.Hour)) {
		t.Fatalf("rotated session result = %#v", rotated)
	}
	if _, err := rig.sessions.FindSessionByTokenHash(context.Background(),
		secrets.HashSupplierSessionToken(first.SessionToken)); !errors.Is(
		err, supplieraccess.ErrSupplierSessionNotFound) {
		t.Fatalf("old session token still resolves: %v", err)
	}
	session, err := rig.sessions.FindSessionByTokenHash(context.Background(),
		secrets.HashSupplierSessionToken(rotated.SessionToken))
	if err != nil {
		t.Fatalf("new session token lookup: %v", err)
	}
	if session.TokenGeneration != 2 ||
		session.CreatedFromChallengeID != firstChallengeID ||
		session.LastVerifiedChallengeID != secondChallengeID ||
		!session.LastVerifiedAt.Equal(reverifiedAt) ||
		session.Revision != 2 {
		t.Fatalf("rotated session = %#v", session)
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 1 {
		t.Fatal("matching presented session created another device session")
	}
}

func TestVerifyChallengeWithoutPresentedTokenCreatesASecondDeviceSession(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	firstChallengeID, firstCode := rig.createChallenge(t, now)
	first, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: firstChallengeID, Code: firstCode,
			OperationID: "verify-device-one",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("verifying first device: %v", err)
	}
	secondChallengeID, secondCode := rig.createAdditionalChallenge(
		t, now.Add(time.Minute), 181, "challenge-operation-device-two")
	second, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: secondChallengeID, Code: secondCode,
			OperationID: "verify-device-two",
			VerifiedAt:  now.Add(2 * time.Minute),
		})
	if err != nil {
		t.Fatalf("verifying second device: %v", err)
	}
	if second.SessionID == first.SessionID ||
		second.SessionToken == first.SessionToken {
		t.Fatalf("second browser inherited first session: %#v", second)
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 2 {
		t.Fatal("missing presented token did not create a separate session")
	}
}

func TestVerifyChallengeNeverResurrectsPresentedRevokedSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	firstChallengeID, firstCode := rig.createChallenge(t, now)
	first, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: firstChallengeID, Code: firstCode,
			OperationID: "verify-before-revoke",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, first.SessionID)
	if err != nil {
		t.Fatalf("loading session to revoke: %v", err)
	}
	revokedAt := now.Add(30 * time.Second)
	session.RevokedAt = &revokedAt
	session.Revision++
	if _, err := rig.sessions.ReplaceSessionCAS(
		context.Background(), session, session.Revision-1); err != nil {
		t.Fatalf("revoking session: %v", err)
	}

	secondChallengeID, secondCode := rig.createAdditionalChallenge(
		t, now.Add(time.Minute), 191, "challenge-operation-after-revoke")
	second, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: secondChallengeID, Code: secondCode,
			OperationID:           "verify-after-revoke",
			PresentedSessionToken: first.SessionToken,
			VerifiedAt:            now.Add(2 * time.Minute),
		})
	if err != nil {
		t.Fatalf("verifying after revocation: %v", err)
	}
	if second.SessionID == first.SessionID {
		t.Fatal("revoked session was resurrected")
	}
	revoked, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, first.SessionID)
	if err != nil || revoked.RevokedAt == nil ||
		revoked.TokenGeneration != 1 || revoked.Revision != 2 {
		t.Fatalf("revoked session changed: %#v/%v", revoked, err)
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 2 {
		t.Fatal("verification did not create a replacement browser session")
	}
}

func TestFreshVerificationAdvancesExistingBindingToNewInvitationGeneration(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	firstChallengeID, firstCode := rig.createChallenge(t, now)
	first, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: firstChallengeID, Code: firstCode,
			OperationID: "verify-generation-two",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("verifying first generation: %v", err)
	}
	before, err := rig.bindings.FindBinding(context.Background(),
		rig.exchange.CompanyID, first.SessionID, rig.exchange.InvitationID)
	if err != nil {
		t.Fatalf("loading first binding: %v", err)
	}

	rig.access.snapshot.AccessGeneration = 3
	secondChallengeID, secondCode := rig.createChallengeForInvitation(
		t, now.Add(time.Minute), 201, "challenge-operation-generation-three",
		rig.exchange.InvitationID, 3)
	reverifiedAt := now.Add(2 * time.Minute)
	second, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: secondChallengeID, Code: secondCode,
			OperationID:           "verify-generation-three",
			PresentedSessionToken: first.SessionToken,
			VerifiedAt:            reverifiedAt,
		})
	if err != nil {
		t.Fatalf("verifying new invitation generation: %v", err)
	}
	if second.SessionID != first.SessionID ||
		second.AccessGeneration != 3 {
		t.Fatalf("reverification result = %#v", second)
	}
	after, err := rig.bindings.FindBinding(context.Background(),
		rig.exchange.CompanyID, first.SessionID, rig.exchange.InvitationID)
	if err != nil {
		t.Fatalf("loading advanced binding: %v", err)
	}
	if after.ID != before.ID || after.AccessGeneration != 3 ||
		after.Revision != before.Revision+1 ||
		!after.BoundAt.Equal(before.BoundAt) ||
		!after.LastValidatedAt.Equal(reverifiedAt) {
		t.Fatalf("advanced binding:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestInvitationRotationDuringVerificationReturnsNoSessionCredential(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	original := rig.access.snapshot
	var validations int
	rig.access.validateFn = func(_, _ string, _ time.Time) (
		supplieraccess.InvitationAccessSnapshot, bool, error) {
		validations++
		if validations == 1 {
			return original, true, nil
		}
		rotated := original
		rotated.AccessGeneration++
		return rotated, true, nil
	}

	result, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: challengeID, Code: code,
			OperationID: "verify-racing-rotation",
			VerifiedAt:  now.Add(time.Minute),
		})
	if !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("rotation race error = %v", err)
	}
	if result.SessionToken != "" || result.CSRFToken != "" ||
		result.SessionID != "" {
		t.Fatalf("rotation race returned credential: %#v", result)
	}
	consumed, err := rig.challenges.FindChallenge(
		context.Background(), challengeID)
	if err != nil || consumed.ConsumedAt == nil {
		t.Fatalf("challenge was not authoritatively consumed: %#v/%v",
			consumed, err)
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 1 ||
		collectionCount(
			t, rig.db, "supplier_session_invitation_bindings") != 1 {
		t.Fatal("rotation race fixture did not reach final validation boundary")
	}
}

func TestOneSessionCanBindToInvitationsAAndB(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeA, codeA := rig.createChallenge(t, now)
	first, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: challengeA, Code: codeA,
			OperationID: "verify-invitation-a",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("verifying invitation A: %v", err)
	}
	snapshotA := rig.access.snapshot
	snapshotB := snapshotA
	snapshotB.InvitationID = "invitation-b"
	snapshotB.AccessGeneration = 1
	rig.access.validateFn = func(_, invitationID string, _ time.Time) (
		supplieraccess.InvitationAccessSnapshot, bool, error) {
		switch invitationID {
		case snapshotA.InvitationID:
			return snapshotA, true, nil
		case snapshotB.InvitationID:
			return snapshotB, true, nil
		default:
			return supplieraccess.InvitationAccessSnapshot{}, false, nil
		}
	}
	challengeB, codeB := rig.createChallengeForInvitation(
		t, now.Add(time.Minute), 211, "challenge-operation-invitation-b",
		snapshotB.InvitationID, snapshotB.AccessGeneration)
	second, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: challengeB, Code: codeB,
			OperationID:           "verify-invitation-b",
			PresentedSessionToken: first.SessionToken,
			VerifiedAt:            now.Add(2 * time.Minute),
		})
	if err != nil {
		t.Fatalf("verifying invitation B: %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("invitation B created a different session: %#v", second)
	}
	bindings, err := rig.bindings.ListSessionBindings(
		context.Background(), snapshotA.CompanyID, first.SessionID)
	if err != nil {
		t.Fatalf("listing session bindings: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("binding count = %d, want 2", len(bindings))
	}
	generations := map[string]int64{}
	for _, binding := range bindings {
		generations[binding.InvitationID] = binding.AccessGeneration
	}
	if generations[snapshotA.InvitationID] != snapshotA.AccessGeneration ||
		generations[snapshotB.InvitationID] != snapshotB.AccessGeneration {
		t.Fatalf("binding generations = %#v", generations)
	}
}

func TestConcurrentDifferentOperationsHaveOneChallengeConsumer(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		rig.access.snapshot.InvitationID: rig.access.snapshot,
	}}
	service := verificationServiceWithAccess(t, rig, access, rig.sessions)

	start := make(chan struct{})
	var successful atomic.Int32
	var rejected atomic.Int32
	var wait sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			_, err := service.VerifyChallenge(context.Background(),
				supplieraccess.VerifyChallengeInput{
					ChallengeID: challengeID, Code: code,
					OperationID: "verify-concurrent-" +
						string(rune('a'+worker)),
					VerifiedAt: now.Add(time.Minute),
				})
			switch {
			case err == nil:
				successful.Add(1)
			case errors.Is(err, supplieraccess.ErrInvalidSupplierCredential):
				rejected.Add(1)
			default:
				t.Errorf("verification %d error: %v", worker, err)
			}
		}(worker)
	}
	close(start)
	wait.Wait()

	if successful.Load() != 1 || rejected.Load() != 1 {
		t.Fatalf("successful/rejected = %d/%d, want 1/1",
			successful.Load(), rejected.Load())
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 1 ||
		collectionCount(
			t, rig.db, "supplier_session_invitation_bindings") != 1 {
		t.Fatal("concurrent challenge confirmation created duplicate recovery state")
	}
}

func TestConcurrentInvitationChallengesAdoptOneSessionRotation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	initialChallenge, initialCode := rig.createChallenge(t, now)
	initial, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: initialChallenge, Code: initialCode,
			OperationID: "verify-initial-session",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("creating initial session: %v", err)
	}

	snapshotA := rig.access.snapshot
	snapshotB := snapshotA
	snapshotB.InvitationID = "invitation-b"
	snapshotB.AccessGeneration = 1
	rig.access.validateFn = func(_, invitationID string, _ time.Time) (
		supplieraccess.InvitationAccessSnapshot, bool, error) {
		switch invitationID {
		case snapshotA.InvitationID:
			return snapshotA, true, nil
		case snapshotB.InvitationID:
			return snapshotB, true, nil
		default:
			return supplieraccess.InvitationAccessSnapshot{}, false, nil
		}
	}
	challengeA, codeA := rig.createChallengeForInvitation(
		t, now.Add(time.Minute), 221, "challenge-operation-concurrent-a",
		snapshotA.InvitationID, snapshotA.AccessGeneration)
	challengeB, codeB := rig.createChallengeForInvitation(
		t, now.Add(time.Minute), 231, "challenge-operation-concurrent-b",
		snapshotB.InvitationID, snapshotB.AccessGeneration)

	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		snapshotA.InvitationID: snapshotA,
		snapshotB.InvitationID: snapshotB,
	}}
	sessions := &barrierSessionStore{
		MongoSupplierSessionRepository: rig.sessions,
		tokenHash:                      secrets.HashSupplierSessionToken(initial.SessionToken),
		release:                        make(chan struct{}),
	}
	service := verificationServiceWithAccess(t, rig, access, sessions)
	inputs := []supplieraccess.VerifyChallengeInput{
		{
			ChallengeID: challengeA, Code: codeA,
			OperationID:           "verify-concurrent-a",
			PresentedSessionToken: initial.SessionToken,
			VerifiedAt:            now.Add(2 * time.Minute),
		},
		{
			ChallengeID: challengeB, Code: codeB,
			OperationID:           "verify-concurrent-b",
			PresentedSessionToken: initial.SessionToken,
			VerifiedAt:            now.Add(2 * time.Minute),
		},
	}
	results := make(chan supplieraccess.VerifyChallengeResult, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, input := range inputs {
		wait.Add(1)
		go func(input supplieraccess.VerifyChallengeInput) {
			defer wait.Done()
			result, err := service.VerifyChallenge(
				context.Background(), input)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(input)
	}
	wait.Wait()
	close(results)
	close(errs)
	if len(errs) != 0 {
		t.Fatalf("concurrent re-verification error = %v", <-errs)
	}
	var returned []supplieraccess.VerifyChallengeResult
	for result := range results {
		returned = append(returned, result)
	}
	if len(returned) != 2 ||
		returned[0].SessionID != initial.SessionID ||
		returned[1].SessionID != initial.SessionID ||
		returned[0].SessionToken != returned[1].SessionToken ||
		returned[0].CSRFToken != returned[1].CSRFToken {
		t.Fatalf("concurrent rotation results = %#v", returned)
	}
	session, err := rig.sessions.FindSession(
		context.Background(), snapshotA.CompanyID, initial.SessionID)
	if err != nil || session.TokenGeneration != 2 ||
		session.Revision != 2 {
		t.Fatalf("converged session = %#v/%v", session, err)
	}
	bindings, err := rig.bindings.ListSessionBindings(
		context.Background(), snapshotA.CompanyID, initial.SessionID)
	if err != nil || len(bindings) != 2 {
		t.Fatalf("concurrent bindings = %#v/%v", bindings, err)
	}
}

func TestConcurrentSameOperationConvergesOnOneNewSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		rig.access.snapshot.InvitationID: rig.access.snapshot,
	}}
	service := verificationServiceWithAccess(t, rig, access, rig.sessions)

	start := make(chan struct{})
	results := make(chan supplieraccess.VerifyChallengeResult, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := service.VerifyChallenge(context.Background(),
				supplieraccess.VerifyChallengeInput{
					ChallengeID: challengeID, Code: code,
					OperationID: "verify-same-operation",
					VerifiedAt:  now.Add(time.Minute),
				})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errs)
	if len(errs) != 0 {
		t.Fatalf("same-operation concurrency error = %v", <-errs)
	}
	var returned []supplieraccess.VerifyChallengeResult
	for result := range results {
		returned = append(returned, result)
	}
	if len(returned) != 2 ||
		returned[0].SessionID != returned[1].SessionID ||
		returned[0].SessionToken != returned[1].SessionToken ||
		returned[0].CSRFToken != returned[1].CSRFToken {
		t.Fatalf("same-operation results = %#v", returned)
	}
	if collectionCount(t, rig.db, "supplier_sessions") != 1 ||
		collectionCount(
			t, rig.db, "supplier_session_invitation_bindings") != 1 {
		t.Fatal("same-operation concurrency created duplicate recovery state")
	}
}

func TestConsumedChallengeRecoveryRemainsStableAfterChallengeExpiry(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	input := supplieraccess.VerifyChallengeInput{
		ChallengeID: challengeID, Code: code,
		OperationID: "verify-before-challenge-expiry",
		VerifiedAt:  now.Add(time.Minute),
	}
	first, err := rig.service.VerifyChallenge(context.Background(), input)
	if err != nil {
		t.Fatalf("initial verification: %v", err)
	}
	input.VerifiedAt = now.Add(11 * time.Minute)
	recovered, err := rig.service.VerifyChallenge(context.Background(), input)
	if err != nil {
		t.Fatalf("recovering after challenge expiry: %v", err)
	}
	if recovered.SessionID != first.SessionID ||
		recovered.SessionToken != first.SessionToken ||
		!recovered.SlidingExpiresAt.Equal(first.SlidingExpiresAt) ||
		!recovered.AbsoluteExpiresAt.Equal(first.AbsoluteExpiresAt) {
		t.Fatalf("post-expiry recovery changed result: %#v", recovered)
	}
}

func TestOlderConsumedOperationCannotReturnTokenAfterLaterVerification(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	firstChallenge, firstCode := rig.createChallenge(t, now)
	first, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: firstChallenge, Code: firstCode,
			OperationID: "verify-token-generation-one",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("creating initial session: %v", err)
	}
	secondChallenge, secondCode := rig.createAdditionalChallenge(
		t, now.Add(time.Minute), 241, "challenge-operation-token-two")
	secondInput := supplieraccess.VerifyChallengeInput{
		ChallengeID: secondChallenge, Code: secondCode,
		OperationID:           "verify-token-generation-two",
		PresentedSessionToken: first.SessionToken,
		VerifiedAt:            now.Add(2 * time.Minute),
	}
	second, err := rig.service.VerifyChallenge(
		context.Background(), secondInput)
	if err != nil {
		t.Fatalf("rotating to token generation two: %v", err)
	}
	thirdChallenge, thirdCode := rig.createAdditionalChallenge(
		t, now.Add(2*time.Minute), 251, "challenge-operation-token-three")
	if _, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: thirdChallenge, Code: thirdCode,
			OperationID:           "verify-token-generation-three",
			PresentedSessionToken: second.SessionToken,
			VerifiedAt:            now.Add(3 * time.Minute),
		}); err != nil {
		t.Fatalf("rotating to token generation three: %v", err)
	}

	secondInput.VerifiedAt = now.Add(4 * time.Minute)
	result, err := rig.service.VerifyChallenge(
		context.Background(), secondInput)
	if !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("obsolete operation error = %v", err)
	}
	if result.SessionToken != "" || result.CSRFToken != "" {
		t.Fatalf("obsolete operation returned token: %#v", result)
	}
}

func TestReverificationUsesActiveSessionKeyVersion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	firstChallenge, firstCode := rig.createChallenge(t, now)
	first, err := rig.service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: firstChallenge, Code: firstCode,
			OperationID: "verify-session-key-one",
			VerifiedAt:  now.Add(10 * time.Second),
		})
	if err != nil {
		t.Fatalf("creating version-one session: %v", err)
	}
	secondChallenge, secondCode := rig.createAdditionalChallenge(
		t, now.Add(time.Minute), 61, "challenge-operation-session-key-two")
	rotatedKeys, err := secrets.NewSupplierSessionTokenKeyring(
		2, map[int]string{
			1: base64.StdEncoding.EncodeToString(bytesOf(91, 32)),
			2: base64.StdEncoding.EncodeToString(bytesOf(101, 32)),
		})
	if err != nil {
		t.Fatalf("constructing rotated session keyring: %v", err)
	}
	rig.sessionKeys = rotatedKeys
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		rig.access.snapshot.InvitationID: rig.access.snapshot,
	}}
	service := verificationServiceWithAccess(t, rig, access, rig.sessions)
	second, err := service.VerifyChallenge(context.Background(),
		supplieraccess.VerifyChallengeInput{
			ChallengeID: secondChallenge, Code: secondCode,
			OperationID:           "verify-session-key-two",
			PresentedSessionToken: first.SessionToken,
			VerifiedAt:            now.Add(2 * time.Minute),
		})
	if err != nil {
		t.Fatalf("re-verifying with active key v2: %v", err)
	}
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, first.SessionID)
	if err != nil || session.TokenKeyVersion != 2 ||
		session.TokenGeneration != 2 {
		t.Fatalf("rotated-key session = %#v/%v", session, err)
	}
	if second.SessionToken == first.SessionToken ||
		!secrets.VerifySupplierSessionToken(
			second.SessionToken, session.TokenHash) {
		t.Fatal("session key rotation did not produce the current credential")
	}
}
