package supplieraccess

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

const maxProtectedSessionCASAttempts = 16

// AuthorizeInvitationAccessInput intentionally accepts no tenant or Supplier
// identifier. Those identities come only from the authenticated session.
type AuthorizeInvitationAccessInput struct {
	SessionToken string
	InvitationID string
	AccessedAt   time.Time
}

// BootstrapSupplierSessionInput intentionally contains no invitation
// identifier. The current invitation is derived exclusively from bindings
// owned by the authenticated Supplier session.
type BootstrapSupplierSessionInput struct {
	SessionToken string
	AccessedAt   time.Time
}

// AuthorizedInvitationAccess is the narrow identity Phase E and later
// Supplier routes may trust after all session, binding and invitation checks.
//
// CSRFToken is populated ONLY by BootstrapSupplierSession (the
// /supplier-access/session bootstrap the frontend calls after a page
// reload) — every other caller (ordinary reads and CSRF-protected
// mutations, via authorizeInvitationAccess) leaves it as the zero value,
// since they have no reason to hand a fresh consumer of this struct a
// second copy of a value already re-derivable, and widening it there would
// exceed what this specific gap requires.
type AuthorizedInvitationAccess struct {
	SessionID                 string
	CompanyID                 string
	SupplierID                string
	NormalizedRecipientEmail  string
	InvitationID              string
	AccessGeneration          int64
	CurrentIssuedRFQVersionID string
	SessionCookieRenewal      SupplierSessionCookieRenewal
	CSRFToken                 string
}

// SupplierSessionCookieRenewal carries the unchanged browser credential and
// the authoritative renewed expiry. The HTTP boundary owns cookie formatting.
type SupplierSessionCookieRenewal struct {
	Token     string
	ExpiresAt time.Time
}

type AuthorizeInvitationMutationInput struct {
	AuthorizeInvitationAccessInput
	CSRFCookie string
	CSRFHeader string
}

func (s *Service) protectedAccessConfigured() bool {
	return s.sessions != nil && s.bindings != nil &&
		s.sessionKeys != nil && s.validator != nil
}

// AuthorizeInvitationAccess validates all three authorization layers on every
// request: the browser session, its exact invitation-generation binding, and
// the current invitation snapshot owned by rfqissuance.
func (s *Service) AuthorizeInvitationAccess(ctx context.Context,
	input AuthorizeInvitationAccessInput) (AuthorizedInvitationAccess, error) {

	return s.authorizeInvitationAccess(ctx, input, nil)
}

// BootstrapSupplierSession resolves the most recently verified invitation
// bound to an active Supplier session. It then performs the same current
// invitation and generation checks as every invitation-scoped read.
func (s *Service) BootstrapSupplierSession(ctx context.Context,
	input BootstrapSupplierSessionInput) (AuthorizedInvitationAccess, error) {

	if !s.protectedAccessConfigured() {
		return AuthorizedInvitationAccess{}, ErrSupplierAccessNotConfigured
	}
	if !IsCanonicalOpaqueCredential(input.SessionToken) ||
		input.AccessedAt.IsZero() {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}

	session, err := s.sessions.FindSessionByTokenHash(
		ctx, secrets.HashSupplierSessionToken(input.SessionToken))
	if errors.Is(err, ErrSupplierSessionNotFound) {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	if !activeSupplierSession(session, input.SessionToken, input.AccessedAt) {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}

	bindings, err := s.bindings.ListSessionBindings(
		ctx, session.CompanyID, session.ID)
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	if len(bindings) == 0 {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}
	binding := bindings[0]
	for _, candidate := range bindings[1:] {
		if candidate.LastValidatedAt.After(binding.LastValidatedAt) ||
			(candidate.LastValidatedAt.Equal(binding.LastValidatedAt) &&
				candidate.BoundAt.After(binding.BoundAt)) ||
			(candidate.LastValidatedAt.Equal(binding.LastValidatedAt) &&
				candidate.BoundAt.Equal(binding.BoundAt) &&
				candidate.ID > binding.ID) {
			binding = candidate
		}
	}
	if binding.CompanyID != session.CompanyID ||
		binding.SupplierSessionID != session.ID ||
		binding.SupplierID != session.SupplierID ||
		binding.NormalizedRecipientEmail != session.RecipientEmailNormalized {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}

	snapshot, found, err := s.validator.ValidateInvitationAccess(
		ctx, session.CompanyID, binding.InvitationID, input.AccessedAt)
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	if !found || snapshot.CompanyID != session.CompanyID ||
		snapshot.SupplierID != session.SupplierID ||
		snapshot.NormalizedRecipientEmail != session.RecipientEmailNormalized ||
		snapshot.InvitationID != binding.InvitationID ||
		snapshot.AccessGeneration != binding.AccessGeneration {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}

	session, err = s.renewSupplierSession(
		ctx, input.SessionToken, session, input.AccessedAt)
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	// Re-derives the SAME value VerifyChallenge originally computed and set
	// in the supplier_csrf cookie: DeriveCSRFToken is a pure function of the
	// session's own identity and credential generation (never changed by
	// renewSupplierSession above), so no new secret material or storage is
	// introduced here — this recovers the frontend's lost in-memory copy
	// after a page reload, it does not mint a second CSRF mechanism.
	csrfToken, err := s.sessionKeys.DeriveCSRFToken(
		session.TokenKeyVersion, supplierSessionTokenContext(session))
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	return AuthorizedInvitationAccess{
		SessionID: session.ID, CompanyID: session.CompanyID,
		SupplierID:                session.SupplierID,
		NormalizedRecipientEmail:  session.RecipientEmailNormalized,
		InvitationID:              snapshot.InvitationID,
		AccessGeneration:          snapshot.AccessGeneration,
		CurrentIssuedRFQVersionID: snapshot.CurrentIssuedRFQVersionID,
		SessionCookieRenewal: SupplierSessionCookieRenewal{
			Token: input.SessionToken, ExpiresAt: session.SlidingExpiresAt,
		},
		CSRFToken: csrfToken,
	}, nil
}

// AuthorizeInvitationMutation adds the reusable CSRF layer used by logout and
// every later Supplier mutation route. It otherwise shares the exact protected
// invitation authorization path with read requests.
func (s *Service) AuthorizeInvitationMutation(ctx context.Context,
	input AuthorizeInvitationMutationInput) (
	AuthorizedInvitationAccess, error) {

	csrf := &supplierCSRFValues{
		cookie: input.CSRFCookie,
		header: input.CSRFHeader,
	}
	return s.authorizeInvitationAccess(
		ctx, input.AuthorizeInvitationAccessInput, csrf)
}

type supplierCSRFValues struct {
	cookie string
	header string
}

func (s *Service) authorizeInvitationAccess(ctx context.Context,
	input AuthorizeInvitationAccessInput,
	csrf *supplierCSRFValues) (AuthorizedInvitationAccess, error) {

	input.InvitationID = strings.TrimSpace(input.InvitationID)
	if !s.protectedAccessConfigured() {
		return AuthorizedInvitationAccess{}, ErrSupplierAccessNotConfigured
	}
	if !IsCanonicalOpaqueCredential(input.SessionToken) ||
		input.InvitationID == "" || input.AccessedAt.IsZero() {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}

	session, err := s.sessions.FindSessionByTokenHash(
		ctx, secrets.HashSupplierSessionToken(input.SessionToken))
	if errors.Is(err, ErrSupplierSessionNotFound) {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	if !activeSupplierSession(session, input.SessionToken, input.AccessedAt) {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}
	if csrf != nil {
		if err := s.validateSessionCSRF(
			session, csrf.cookie, csrf.header); err != nil {
			return AuthorizedInvitationAccess{}, err
		}
	}

	binding, err := s.bindings.FindBinding(
		ctx, session.CompanyID, session.ID, input.InvitationID)
	if errors.Is(err, ErrSessionInvitationBindingNotFound) {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}

	snapshot, found, err := s.validator.ValidateInvitationAccess(
		ctx, session.CompanyID, input.InvitationID, input.AccessedAt)
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	// Identity and generation are compared field-for-field. A session alone
	// never grants Supplier-wide access, and stale bindings fail closed without
	// requiring reverse calls from rfqissuance.
	if !found ||
		binding.CompanyID != session.CompanyID ||
		binding.SupplierSessionID != session.ID ||
		binding.SupplierID != session.SupplierID ||
		binding.NormalizedRecipientEmail !=
			session.RecipientEmailNormalized ||
		binding.InvitationID != input.InvitationID ||
		snapshot.CompanyID != session.CompanyID ||
		snapshot.SupplierID != session.SupplierID ||
		snapshot.NormalizedRecipientEmail !=
			session.RecipientEmailNormalized ||
		snapshot.InvitationID != input.InvitationID ||
		binding.AccessGeneration != snapshot.AccessGeneration {
		return AuthorizedInvitationAccess{}, ErrInvalidSupplierCredential
	}

	session, err = s.renewSupplierSession(
		ctx, input.SessionToken, session, input.AccessedAt)
	if err != nil {
		return AuthorizedInvitationAccess{}, err
	}
	return AuthorizedInvitationAccess{
		SessionID: session.ID, CompanyID: session.CompanyID,
		SupplierID:                session.SupplierID,
		NormalizedRecipientEmail:  session.RecipientEmailNormalized,
		InvitationID:              snapshot.InvitationID,
		AccessGeneration:          snapshot.AccessGeneration,
		CurrentIssuedRFQVersionID: snapshot.CurrentIssuedRFQVersionID,
		SessionCookieRenewal: SupplierSessionCookieRenewal{
			Token: input.SessionToken, ExpiresAt: session.SlidingExpiresAt,
		},
	}, nil
}

func activeSupplierSession(session SupplierSession, rawToken string,
	at time.Time) bool {
	return secrets.VerifySupplierSessionToken(rawToken, session.TokenHash) &&
		session.RevokedAt == nil &&
		session.SlidingExpiresAt.After(at) &&
		session.AbsoluteExpiresAt.After(at)
}

func (s *Service) renewSupplierSession(ctx context.Context, rawToken string,
	session SupplierSession, accessedAt time.Time) (SupplierSession, error) {

	desiredExpiry := accessedAt.Add(supplierSessionSlidingLifetime)
	if desiredExpiry.After(session.AbsoluteExpiresAt) {
		desiredExpiry = session.AbsoluteExpiresAt
	}
	for attempt := 0; attempt < maxProtectedSessionCASAttempts; attempt++ {
		if !activeSupplierSession(session, rawToken, accessedAt) {
			return SupplierSession{}, ErrInvalidSupplierCredential
		}
		// An equal-or-later expiry means a competing protected request already
		// performed at least this renewal. Adopting it avoids writing an older
		// LastUsedAt or shortening the browser/server lifetime.
		if !session.SlidingExpiresAt.Before(desiredExpiry) {
			return session, nil
		}

		candidate := session
		candidate.SlidingExpiresAt = desiredExpiry
		candidate.LastUsedAt = accessedAt
		candidate.Revision = session.Revision + 1
		updated, err := s.sessions.ReplaceSessionCAS(
			ctx, candidate, session.Revision)
		if err == nil {
			if s.audit != nil {
				_ = s.audit.RecordSessionRenewed(
					ctx, updated.CompanyID, updated.SupplierID, updated.ID,
					updated.TokenGeneration, updated.SlidingExpiresAt, accessedAt)
			}
			return updated, nil
		}
		if !errors.Is(err, ErrSupplierSessionConflict) {
			return SupplierSession{}, err
		}

		// Reload through the presented token. A concurrent verification that
		// rotated the credential makes the old hash disappear and must fail
		// this request closed instead of restoring the obsolete generation.
		session, err = s.sessions.FindSessionByTokenHash(
			ctx, secrets.HashSupplierSessionToken(rawToken))
		if errors.Is(err, ErrSupplierSessionNotFound) {
			return SupplierSession{}, ErrInvalidSupplierCredential
		}
		if err != nil {
			return SupplierSession{}, err
		}
	}
	return SupplierSession{}, ErrSupplierSessionConflict
}
