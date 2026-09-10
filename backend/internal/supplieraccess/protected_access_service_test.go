package supplieraccess_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

type countingSessionLookupStore struct {
	supplieraccess.SupplierSessionStore
	findByTokenHashCalls atomic.Int32
	session              *supplieraccess.SupplierSession
}

func (s *countingSessionLookupStore) FindSessionByTokenHash(
	ctx context.Context, tokenHash string,
) (supplieraccess.SupplierSession, error) {
	s.findByTokenHashCalls.Add(1)
	if s.session != nil {
		return *s.session, nil
	}
	return s.SupplierSessionStore.FindSessionByTokenHash(ctx, tokenHash)
}

func verifySupplierSession(t *testing.T, rig *verificationServiceRig,
	now time.Time) supplieraccess.VerifyChallengeResult {
	t.Helper()
	challengeID, code := rig.createChallenge(t, now)
	verified, err := rig.service.VerifyChallenge(
		context.Background(), supplieraccess.VerifyChallengeInput{
			ChallengeID: challengeID,
			Code:        code,
			OperationID: "verify-for-protected-access",
			VerifiedAt:  now.Add(time.Second),
		})
	if err != nil {
		t.Fatalf("verifying session: %v", err)
	}
	return verified
}

func TestProtectedAccessAuthorizesOnlyTheVerifiedInvitationBinding(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)

	authorized, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   now.Add(time.Minute),
		})
	if err != nil {
		t.Fatalf("authorizing protected access: %v", err)
	}
	if authorized.SessionID != verified.SessionID ||
		authorized.CompanyID != rig.exchange.CompanyID ||
		authorized.SupplierID != rig.exchange.SupplierID ||
		authorized.NormalizedRecipientEmail !=
			rig.exchange.NormalizedRecipientEmail ||
		authorized.InvitationID != rig.exchange.InvitationID ||
		authorized.AccessGeneration != rig.exchange.AccessGeneration ||
		authorized.CurrentIssuedRFQVersionID != "version-2" {
		t.Fatalf("authorized access = %#v", authorized)
	}
}

func TestProtectedAccessGenerationChangeInvalidatesOnlyAffectedInvitation(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading verified session: %v", err)
	}

	snapshotA := rig.access.snapshot
	snapshotB := snapshotA
	snapshotB.InvitationID = "invitation-b"
	snapshotB.AccessGeneration = 7
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		snapshotA.InvitationID: snapshotA,
		snapshotB.InvitationID: snapshotB,
	}}
	service := verificationServiceWithAccess(t, rig, access, rig.sessions)
	if _, err := rig.bindings.CreateBinding(
		context.Background(), supplieraccess.SupplierSessionInvitationBinding{
			ID:                       "binding-b",
			CompanyID:                session.CompanyID,
			SupplierSessionID:        session.ID,
			SupplierID:               session.SupplierID,
			NormalizedRecipientEmail: session.RecipientEmailNormalized,
			InvitationID:             snapshotB.InvitationID,
			AccessGeneration:         snapshotB.AccessGeneration,
			BoundAt:                  now,
			LastValidatedAt:          now,
			Revision:                 1,
		}); err != nil {
		t.Fatalf("creating invitation B binding: %v", err)
	}

	for _, invitationID := range []string{
		snapshotA.InvitationID,
		snapshotB.InvitationID,
	} {
		if _, err := service.AuthorizeInvitationAccess(
			context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
				SessionToken: verified.SessionToken,
				InvitationID: invitationID,
				AccessedAt:   now.Add(time.Minute),
			}); err != nil {
			t.Fatalf("authorizing %s before rotation: %v", invitationID, err)
		}
	}

	rotatedA := snapshotA
	rotatedA.AccessGeneration++
	access.snapshots[snapshotA.InvitationID] = rotatedA
	if _, err := service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: snapshotA.InvitationID,
			AccessedAt:   now.Add(2 * time.Minute),
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("rotated invitation A error = %v", err)
	}
	if authorizedB, err := service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: snapshotB.InvitationID,
			AccessedAt:   now.Add(2 * time.Minute),
		}); err != nil || authorizedB.InvitationID != snapshotB.InvitationID {
		t.Fatalf("invitation B after A rotation = %#v, %v", authorizedB, err)
	}
}

func TestProtectedAccessDeniesUnboundOrReplacedRecipient(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)

	snapshotA := rig.access.snapshot
	unbound := snapshotA
	unbound.InvitationID = "unbound-invitation"
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		snapshotA.InvitationID: snapshotA,
		unbound.InvitationID:   unbound,
	}}
	service := verificationServiceWithAccess(t, rig, access, rig.sessions)
	if _, err := service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: unbound.InvitationID,
			AccessedAt:   now.Add(time.Minute),
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("unbound invitation error = %v", err)
	}

	replaced := snapshotA
	replaced.NormalizedRecipientEmail = "replacement@supplier.test"
	access.snapshots[snapshotA.InvitationID] = replaced
	if _, err := service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: snapshotA.InvitationID,
			AccessedAt:   now.Add(time.Minute),
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("replaced recipient error = %v", err)
	}
}

func TestProtectedAccessRenewsServerAndBrowserSessionExpiryTogether(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	accessedAt := now.Add(24 * time.Hour)
	wantExpiry := accessedAt.Add(30 * 24 * time.Hour)

	authorized, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   accessedAt,
		})
	if err != nil {
		t.Fatalf("authorizing protected access: %v", err)
	}
	if authorized.SessionCookieRenewal.Token != verified.SessionToken ||
		!authorized.SessionCookieRenewal.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("browser renewal = %#v, want token unchanged and expiry %s",
			authorized.SessionCookieRenewal, wantExpiry)
	}

	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading renewed session: %v", err)
	}
	if !session.SlidingExpiresAt.Equal(wantExpiry) ||
		!session.LastUsedAt.Equal(accessedAt) ||
		session.TokenGeneration != 1 ||
		session.TokenHash !=
			secrets.HashSupplierSessionToken(verified.SessionToken) ||
		session.Revision != 2 {
		t.Fatalf("renewed session = %#v", session)
	}
}

func TestProtectedAccessRenewalCapsAtAbsoluteExpiryAndFailsAtBoundary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	for _, accessedAt := range []time.Time{
		now.Add(20 * 24 * time.Hour),
		now.Add(40 * 24 * time.Hour),
	} {
		if _, err := rig.service.AuthorizeInvitationAccess(
			context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
				SessionToken: verified.SessionToken,
				InvitationID: rig.exchange.InvitationID,
				AccessedAt:   accessedAt,
			}); err != nil {
			t.Fatalf("authorizing continued activity at %s: %v",
				accessedAt, err)
		}
	}
	accessedAt := verified.AbsoluteExpiresAt.
		Add(-30 * 24 * time.Hour).
		Add(time.Second)

	authorized, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   accessedAt,
		})
	if err != nil {
		t.Fatalf("authorizing before absolute expiry: %v", err)
	}
	if !authorized.SessionCookieRenewal.ExpiresAt.Equal(
		verified.AbsoluteExpiresAt) {
		t.Fatalf("renewal expiry = %s, want absolute cap %s",
			authorized.SessionCookieRenewal.ExpiresAt,
			verified.AbsoluteExpiresAt)
	}
	beforeBoundary, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading capped session: %v", err)
	}

	if _, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   verified.AbsoluteExpiresAt,
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("exact absolute-expiry error = %v", err)
	}
	afterBoundary, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("reloading capped session: %v", err)
	}
	if afterBoundary.Revision != beforeBoundary.Revision ||
		!afterBoundary.LastUsedAt.Equal(beforeBoundary.LastUsedAt) {
		t.Fatalf("expired access mutated session:\nbefore=%#v\nafter=%#v",
			beforeBoundary, afterBoundary)
	}
}

func TestFailedProtectedAccessDoesNotRenewSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	before, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session before denied access: %v", err)
	}

	if _, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: "unbound-invitation",
			AccessedAt:   now.Add(24 * time.Hour),
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("unbound access error = %v", err)
	}
	after, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session after denied access: %v", err)
	}
	if after.Revision != before.Revision ||
		!after.SlidingExpiresAt.Equal(before.SlidingExpiresAt) ||
		!after.LastUsedAt.Equal(before.LastUsedAt) {
		t.Fatalf("denied access renewed session:\nbefore=%#v\nafter=%#v",
			before, after)
	}
}

func TestConcurrentProtectedAccessAdoptsOneRenewal(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	barrier := &barrierSessionStore{
		MongoSupplierSessionRepository: rig.sessions,
		tokenHash: secrets.HashSupplierSessionToken(
			verified.SessionToken),
		release: make(chan struct{}),
	}
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		rig.access.snapshot.InvitationID: rig.access.snapshot,
	}}
	service := verificationServiceWithAccess(t, rig, access, barrier)
	accessedAt := now.Add(24 * time.Hour)
	wantExpiry := accessedAt.Add(30 * 24 * time.Hour)

	type outcome struct {
		result supplieraccess.AuthorizedInvitationAccess
		err    error
	}
	outcomes := make(chan outcome, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.AuthorizeInvitationAccess(
				context.Background(),
				supplieraccess.AuthorizeInvitationAccessInput{
					SessionToken: verified.SessionToken,
					InvitationID: rig.exchange.InvitationID,
					AccessedAt:   accessedAt,
				})
			outcomes <- outcome{result: result, err: err}
		}()
	}
	wait.Wait()
	close(outcomes)
	for outcome := range outcomes {
		if outcome.err != nil ||
			outcome.result.SessionCookieRenewal.Token !=
				verified.SessionToken ||
			!outcome.result.SessionCookieRenewal.ExpiresAt.Equal(
				wantExpiry) {
			t.Fatalf("concurrent renewal outcome = %#v, %v",
				outcome.result, outcome.err)
		}
	}
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading concurrently renewed session: %v", err)
	}
	if session.Revision != 2 ||
		!session.SlidingExpiresAt.Equal(wantExpiry) {
		t.Fatalf("concurrently renewed session = %#v", session)
	}
}

func TestSupplierSessionLogoutRevokesSessionAndLeavesBindingsAsHistory(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	loggedOutAt := now.Add(time.Minute)

	err := rig.service.LogoutSupplierSession(
		context.Background(), supplieraccess.LogoutSupplierSessionInput{
			SessionToken: verified.SessionToken,
			CSRFCookie:   verified.CSRFToken,
			CSRFHeader:   verified.CSRFToken,
			LoggedOutAt:  loggedOutAt,
		})
	if err != nil {
		t.Fatalf("logging out supplier session: %v", err)
	}
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading revoked session: %v", err)
	}
	if session.RevokedAt == nil ||
		!session.RevokedAt.Equal(loggedOutAt) ||
		session.Revision != 2 {
		t.Fatalf("logged-out session = %#v", session)
	}
	if count := collectionCount(
		t, rig.db, "supplier_session_invitation_bindings"); count != 1 {
		t.Fatalf("logout rewrote historical bindings: count = %d", count)
	}
	if _, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   loggedOutAt.Add(time.Second),
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("post-logout access error = %v", err)
	}
}

func TestSupplierSessionLogoutRejectsMissingOrMismatchedCSRFWithoutMutation(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	cases := []struct {
		name   string
		cookie string
		header string
	}{
		{name: "missing header", cookie: verified.CSRFToken},
		{name: "mismatch", cookie: verified.CSRFToken, header: opaqueToken(201)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := rig.service.LogoutSupplierSession(
				context.Background(),
				supplieraccess.LogoutSupplierSessionInput{
					SessionToken: verified.SessionToken,
					CSRFCookie:   testCase.cookie,
					CSRFHeader:   testCase.header,
					LoggedOutAt:  now.Add(time.Minute),
				})
			if !errors.Is(err, supplieraccess.ErrSupplierCSRFRejected) {
				t.Fatalf("logout error = %v", err)
			}
			session, findErr := rig.sessions.FindSession(
				context.Background(), rig.exchange.CompanyID,
				verified.SessionID)
			if findErr != nil {
				t.Fatalf("loading session after CSRF rejection: %v", findErr)
			}
			if session.RevokedAt != nil || session.Revision != 1 {
				t.Fatalf("CSRF rejection mutated session = %#v", session)
			}
		})
	}
}

func TestSupplierSessionLogoutIsNeutralForUnknownExpiredAndRevokedSessions(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	arbitraryMatchingCSRF := opaqueToken(202)

	if err := rig.service.LogoutSupplierSession(
		context.Background(), supplieraccess.LogoutSupplierSessionInput{
			SessionToken: opaqueToken(203),
			CSRFCookie:   arbitraryMatchingCSRF,
			CSRFHeader:   arbitraryMatchingCSRF,
			LoggedOutAt:  now.Add(time.Minute),
		}); err != nil {
		t.Fatalf("unknown-session logout: %v", err)
	}

	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session for expiry fixture: %v", err)
	}
	expiredAt := now.Add(2 * time.Minute)
	session.SlidingExpiresAt = expiredAt
	session.Revision++
	if _, err := rig.sessions.ReplaceSessionCAS(
		context.Background(), session, session.Revision-1); err != nil {
		t.Fatalf("expiring session fixture: %v", err)
	}
	if err := rig.service.LogoutSupplierSession(
		context.Background(), supplieraccess.LogoutSupplierSessionInput{
			SessionToken: verified.SessionToken,
			CSRFCookie:   arbitraryMatchingCSRF,
			CSRFHeader:   arbitraryMatchingCSRF,
			LoggedOutAt:  expiredAt,
		}); err != nil {
		t.Fatalf("expired-session logout: %v", err)
	}
	expired, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading expired session: %v", err)
	}
	if expired.RevokedAt != nil || expired.Revision != 2 {
		t.Fatalf("expired logout mutated session = %#v", expired)
	}

	revokedAt := now.Add(time.Minute)
	expired.SlidingExpiresAt = now.Add(30 * 24 * time.Hour)
	expired.RevokedAt = &revokedAt
	expired.Revision++
	if _, err := rig.sessions.ReplaceSessionCAS(
		context.Background(), expired, expired.Revision-1); err != nil {
		t.Fatalf("revoking session fixture: %v", err)
	}
	if err := rig.service.LogoutSupplierSession(
		context.Background(), supplieraccess.LogoutSupplierSessionInput{
			SessionToken: verified.SessionToken,
			CSRFCookie:   arbitraryMatchingCSRF,
			CSRFHeader:   arbitraryMatchingCSRF,
			LoggedOutAt:  now.Add(3 * time.Minute),
		}); err != nil {
		t.Fatalf("already-revoked logout: %v", err)
	}
	revoked, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading revoked session: %v", err)
	}
	if revoked.Revision != 3 ||
		revoked.RevokedAt == nil ||
		!revoked.RevokedAt.Equal(revokedAt) {
		t.Fatalf("repeated logout mutated revoked session = %#v", revoked)
	}
}

func TestUnknownSessionLogoutStillRejectsMismatchedCSRF(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	err := rig.service.LogoutSupplierSession(
		context.Background(), supplieraccess.LogoutSupplierSessionInput{
			SessionToken: opaqueToken(204),
			CSRFCookie:   opaqueToken(205),
			CSRFHeader:   opaqueToken(206),
			LoggedOutAt:  now,
		})
	if !errors.Is(err, supplieraccess.ErrSupplierCSRFRejected) {
		t.Fatalf("unknown-session mismatched CSRF error = %v", err)
	}
}

func TestProtectedInvitationMutationRequiresServerDerivedCSRFBeforeRenewal(
	t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	accessedAt := now.Add(24 * time.Hour)

	before, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session before mutation authorization: %v", err)
	}
	if _, err := rig.service.AuthorizeInvitationMutation(
		context.Background(), supplieraccess.AuthorizeInvitationMutationInput{
			AuthorizeInvitationAccessInput: supplieraccess.AuthorizeInvitationAccessInput{
				SessionToken: verified.SessionToken,
				InvitationID: rig.exchange.InvitationID,
				AccessedAt:   accessedAt,
			},
			CSRFCookie: verified.CSRFToken,
			CSRFHeader: opaqueToken(207),
		}); !errors.Is(err, supplieraccess.ErrSupplierCSRFRejected) {
		t.Fatalf("mismatched mutation CSRF error = %v", err)
	}
	afterRejection, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session after CSRF rejection: %v", err)
	}
	if afterRejection.Revision != before.Revision ||
		!afterRejection.SlidingExpiresAt.Equal(before.SlidingExpiresAt) {
		t.Fatalf("CSRF rejection renewed session:\nbefore=%#v\nafter=%#v",
			before, afterRejection)
	}

	authorized, err := rig.service.AuthorizeInvitationMutation(
		context.Background(), supplieraccess.AuthorizeInvitationMutationInput{
			AuthorizeInvitationAccessInput: supplieraccess.AuthorizeInvitationAccessInput{
				SessionToken: verified.SessionToken,
				InvitationID: rig.exchange.InvitationID,
				AccessedAt:   accessedAt,
			},
			CSRFCookie: verified.CSRFToken,
			CSRFHeader: verified.CSRFToken,
		})
	if err != nil {
		t.Fatalf("authorizing protected mutation: %v", err)
	}
	if authorized.InvitationID != rig.exchange.InvitationID ||
		authorized.SessionCookieRenewal.Token != verified.SessionToken ||
		!authorized.SessionCookieRenewal.ExpiresAt.Equal(
			accessedAt.Add(30*24*time.Hour)) {
		t.Fatalf("authorized mutation = %#v", authorized)
	}
}

func TestMalformedSessionCredentialDoesNotReachMongo(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	store := &countingSessionLookupStore{
		SupplierSessionStore: rig.sessions,
	}
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		rig.access.snapshot.InvitationID: rig.access.snapshot,
	}}
	service := verificationServiceWithAccess(t, rig, access, store)

	if _, err := service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: "malformed",
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   now,
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("malformed session error = %v", err)
	}
	if calls := store.findByTokenHashCalls.Load(); calls != 0 {
		t.Fatalf("malformed credential reached Mongo %d times", calls)
	}
}

func TestSessionAuthenticationConstantTimeChecksLoadedTokenHash(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading verified session: %v", err)
	}
	store := &countingSessionLookupStore{
		SupplierSessionStore: rig.sessions,
		session:              &session,
	}
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		rig.access.snapshot.InvitationID: rig.access.snapshot,
	}}
	service := verificationServiceWithAccess(t, rig, access, store)

	if _, err := service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: opaqueToken(208),
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   now.Add(time.Minute),
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("mismatched loaded hash error = %v", err)
	}
	if calls := store.findByTokenHashCalls.Load(); calls != 1 {
		t.Fatalf("session hash lookups = %d, want 1", calls)
	}
	unchanged, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("reloading session: %v", err)
	}
	if unchanged.Revision != session.Revision {
		t.Fatalf("mismatched hash mutated session = %#v", unchanged)
	}
}

func TestProtectedAccessFailsAtExactSlidingExpiry(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	before, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session: %v", err)
	}

	if _, err := rig.service.AuthorizeInvitationAccess(
		context.Background(), supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: verified.SessionToken,
			InvitationID: rig.exchange.InvitationID,
			AccessedAt:   verified.SlidingExpiresAt,
		}); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("exact sliding-expiry error = %v", err)
	}
	after, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("reloading session: %v", err)
	}
	if after.Revision != before.Revision {
		t.Fatalf("exact-boundary denial mutated session = %#v", after)
	}
}

func TestConcurrentSupplierLogoutConvergesOnOneRevocation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	barrier := &barrierSessionStore{
		MongoSupplierSessionRepository: rig.sessions,
		tokenHash: secrets.HashSupplierSessionToken(
			verified.SessionToken),
		release: make(chan struct{}),
	}
	access := mappedInvitationAccess{snapshots: map[string]supplieraccess.InvitationAccessSnapshot{
		rig.access.snapshot.InvitationID: rig.access.snapshot,
	}}
	service := verificationServiceWithAccess(t, rig, access, barrier)
	loggedOutAt := now.Add(time.Minute)

	errorsSeen := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsSeen <- service.LogoutSupplierSession(
				context.Background(),
				supplieraccess.LogoutSupplierSessionInput{
					SessionToken: verified.SessionToken,
					CSRFCookie:   verified.CSRFToken,
					CSRFHeader:   verified.CSRFToken,
					LoggedOutAt:  loggedOutAt,
				})
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent logout error = %v", err)
		}
	}
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading concurrently revoked session: %v", err)
	}
	if session.Revision != 2 ||
		session.RevokedAt == nil ||
		!session.RevokedAt.Equal(loggedOutAt) {
		t.Fatalf("concurrently revoked session = %#v", session)
	}
}
