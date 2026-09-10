package supplieraccess

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

type LogoutSupplierSessionInput struct {
	SessionToken string
	CSRFCookie   string
	CSRFHeader   string
	LoggedOutAt  time.Time
}

// LogoutSupplierSession revokes the authenticated browser session while
// retaining invitation bindings as history. The active session boundary makes
// every one of those bindings unusable immediately.
func (s *Service) LogoutSupplierSession(ctx context.Context,
	input LogoutSupplierSessionInput) error {

	if s.sessions == nil || s.sessionKeys == nil {
		return ErrSupplierAccessNotConfigured
	}
	if input.LoggedOutAt.IsZero() {
		return ErrInvalidSupplierCredential
	}
	if !matchingCSRFPair(input.CSRFCookie, input.CSRFHeader) {
		return ErrSupplierCSRFRejected
	}
	// An unusable session credential has no authoritative session to revoke.
	// Matching double-submit values still make logout idempotently successful,
	// allowing the HTTP boundary to clear both browser cookies.
	if !IsCanonicalOpaqueCredential(input.SessionToken) {
		return nil
	}

	session, err := s.sessions.FindSessionByTokenHash(
		ctx, secrets.HashSupplierSessionToken(input.SessionToken))
	if errors.Is(err, ErrSupplierSessionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	for attempt := 0; attempt < maxProtectedSessionCASAttempts; attempt++ {
		if !secrets.VerifySupplierSessionToken(
			input.SessionToken, session.TokenHash) {
			return nil
		}
		// Expired and already-revoked sessions are intentionally
		// indistinguishable from unknown sessions during logout.
		if session.RevokedAt != nil ||
			!session.SlidingExpiresAt.After(input.LoggedOutAt) ||
			!session.AbsoluteExpiresAt.After(input.LoggedOutAt) {
			return nil
		}
		if err := s.validateSessionCSRF(
			session, input.CSRFCookie, input.CSRFHeader); err != nil {
			return err
		}

		candidate := session
		revokedAt := input.LoggedOutAt
		candidate.RevokedAt = &revokedAt
		candidate.Revision = session.Revision + 1
		if updated, err := s.sessions.ReplaceSessionCAS(
			ctx, candidate, session.Revision); err == nil {
			if s.audit != nil {
				_ = s.audit.RecordSessionRevoked(
					ctx, updated.CompanyID, updated.SupplierID, updated.ID,
					updated.TokenGeneration, revokedAt)
			}
			return nil
		} else if !errors.Is(err, ErrSupplierSessionConflict) {
			return err
		}

		session, err = s.sessions.FindSessionByTokenHash(
			ctx, secrets.HashSupplierSessionToken(input.SessionToken))
		if errors.Is(err, ErrSupplierSessionNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return ErrSupplierSessionConflict
}

func (s *Service) validateSessionCSRF(session SupplierSession,
	csrfCookie, csrfHeader string) error {

	if !matchingCSRFPair(csrfCookie, csrfHeader) {
		return ErrSupplierCSRFRejected
	}
	expected, err := s.sessionKeys.DeriveCSRFToken(
		session.TokenKeyVersion, supplierSessionTokenContext(session))
	if err != nil {
		return err
	}
	if !constantTimeStringEqual(csrfCookie, expected) ||
		!constantTimeStringEqual(csrfHeader, expected) {
		return ErrSupplierCSRFRejected
	}
	return nil
}

func matchingCSRFPair(csrfCookie, csrfHeader string) bool {
	return IsCanonicalOpaqueCredential(csrfCookie) &&
		IsCanonicalOpaqueCredential(csrfHeader) &&
		constantTimeStringEqual(csrfCookie, csrfHeader)
}

func constantTimeStringEqual(left, right string) bool {
	return subtle.ConstantTimeCompare(
		[]byte(left), []byte(right)) == 1
}

func supplierSessionTokenContext(
	session SupplierSession) secrets.SupplierSessionTokenContext {
	return secrets.SupplierSessionTokenContext{
		SessionID: session.ID, TokenGeneration: session.TokenGeneration,
		CompanyID: session.CompanyID, SupplierID: session.SupplierID,
		NormalizedRecipientEmail: session.RecipientEmailNormalized,
	}
}
