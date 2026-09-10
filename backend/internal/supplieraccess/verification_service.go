package supplieraccess

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

const maxConfirmationCASAttempts = 16

const (
	supplierSessionSlidingLifetime  = 30 * 24 * time.Hour
	supplierSessionAbsoluteLifetime = 90 * 24 * time.Hour
)

// Verification failure reasons are a bounded audit vocabulary. Public
// responses deliberately collapse every one of them to the same credential
// failure and never echo the submitted code.
const (
	VerificationFailureInvalidCode         = "invalid_code"
	VerificationFailureExpired             = "expired"
	VerificationFailureSuperseded          = "superseded"
	VerificationFailureAttemptLimitReached = "attempt_limit_reached"
	VerificationFailureAlreadyConsumed     = "already_consumed"
	VerificationFailureStaleGeneration     = "stale_generation"
)

type VerifyChallengeInput struct {
	ChallengeID           string
	Code                  string
	OperationID           string
	PresentedSessionToken string
	VerifiedAt            time.Time
}

type VerifyChallengeResult struct {
	SessionID         string
	InvitationID      string
	AccessGeneration  int64
	SessionToken      string
	CSRFToken         string
	SlidingExpiresAt  time.Time
	AbsoluteExpiresAt time.Time
}

func (s *Service) sessionVerificationConfigured() bool {
	return s.verificationConfigured() && s.sessions != nil &&
		s.bindings != nil && s.sessionKeys != nil
}

// VerifyChallenge treats every malformed or incorrect code as one online
// guess. Code shape is deliberately not rejected before the atomic attempt
// transition because format must not provide a free brute-force oracle.
func (s *Service) VerifyChallenge(ctx context.Context,
	input VerifyChallengeInput) (VerifyChallengeResult, error) {

	input.ChallengeID = strings.TrimSpace(input.ChallengeID)
	input.OperationID = strings.TrimSpace(input.OperationID)
	if !s.sessionVerificationConfigured() {
		return VerifyChallengeResult{}, ErrSupplierAccessNotConfigured
	}
	if !IsCanonicalOpaqueCredential(input.ChallengeID) {
		return VerifyChallengeResult{}, ErrInvalidSupplierCredential
	}
	if procurementlimits.ValidateID(input.OperationID) != nil || input.VerifiedAt.IsZero() {
		return VerifyChallengeResult{}, ErrInvalidVerificationRequest
	}

	for attempt := 0; attempt < maxConfirmationCASAttempts; attempt++ {
		challenge, err := s.challenges.FindChallenge(ctx, input.ChallengeID)
		if errors.Is(err, ErrVerificationChallengeNotFound) {
			return VerifyChallengeResult{}, ErrInvalidSupplierCredential
		}
		if err != nil {
			return VerifyChallengeResult{}, err
		}
		valid, err := s.verifySubmittedChallengeCode(challenge, input.Code)
		if err != nil {
			return VerifyChallengeResult{}, err
		}
		if challenge.ConsumedAt != nil {
			if !valid ||
				challenge.ConsumedOperationID != input.OperationID {
				return VerifyChallengeResult{}, ErrInvalidSupplierCredential
			}
			return s.recoverVerificationSession(
				ctx, challenge, input.VerifiedAt)
		}
		if !challenge.ExpiresAt.After(input.VerifiedAt) ||
			challenge.SupersededAt != nil || challenge.LockedAt != nil ||
			challenge.AttemptsRemaining < 1 {
			return VerifyChallengeResult{}, ErrInvalidSupplierCredential
		}
		if err := s.requireCurrentChallenge(ctx, challenge); err != nil {
			if errors.Is(err, ErrVerificationChallengeNotCurrent) {
				return VerifyChallengeResult{}, ErrInvalidSupplierCredential
			}
			return VerifyChallengeResult{}, err
		}

		if !valid {
			if updated, err := s.challenges.RecordFailedConfirmation(
				ctx, challenge, input.VerifiedAt); err == nil {
				reason := VerificationFailureInvalidCode
				if updated.LockedAt != nil || updated.AttemptsRemaining < 1 {
					reason = VerificationFailureAttemptLimitReached
				}
				if s.audit != nil {
					_ = s.audit.RecordVerificationFailed(
						ctx, updated.CompanyID, updated.SupplierID,
						updated.InvitationID, updated.ID,
						updated.AccessGeneration, reason, input.VerifiedAt)
				}
				return VerifyChallengeResult{}, ErrInvalidSupplierCredential
			} else if errors.Is(
				err, ErrVerificationChallengeNotConfirmable) {
				// A competing failed request may have consumed this revision.
				// Reload so this request either consumes its own attempt or
				// observes the authoritative lock/current-state change.
				continue
			} else {
				return VerifyChallengeResult{}, err
			}
		}

		if err := s.validateChallengeInvitation(
			ctx, challenge, input.VerifiedAt); err != nil {
			return VerifyChallengeResult{}, err
		}
		target, err := s.selectVerificationSessionTarget(
			ctx, challenge, input.OperationID,
			input.PresentedSessionToken, input.VerifiedAt)
		if err != nil {
			return VerifyChallengeResult{}, err
		}
		consumed, err := s.challenges.ConsumeForVerification(
			ctx, challenge, target, input.VerifiedAt)
		if errors.Is(err, ErrVerificationChallengeNotConfirmable) {
			// Another confirmation may have committed the recovery target.
			// Reload and allow only the matching operation plus correct code
			// to adopt it.
			continue
		}
		if err != nil {
			return VerifyChallengeResult{}, err
		}
		result, err := s.recoverVerificationSession(
			ctx, consumed, input.VerifiedAt)
		if err != nil {
			return VerifyChallengeResult{}, err
		}
		// Only this path won challenge consumption. Same-operation recovery
		// enters the consumed branch above and cannot duplicate the event.
		if s.audit != nil {
			_ = s.audit.RecordVerificationSucceeded(
				ctx, consumed.CompanyID, consumed.SupplierID,
				consumed.InvitationID, consumed.ID,
				consumed.SupplierSessionID, consumed.AccessGeneration,
				input.VerifiedAt)
		}
		return result, nil
	}
	return VerifyChallengeResult{}, ErrInvalidSupplierCredential
}

func (s *Service) selectVerificationSessionTarget(ctx context.Context,
	challenge EmailVerificationChallenge, operationID, presentedToken string,
	at time.Time) (VerificationSessionTarget, error) {

	if IsCanonicalOpaqueCredential(presentedToken) {
		session, err := s.sessions.FindSessionByTokenHash(
			ctx, secrets.HashSupplierSessionToken(presentedToken))
		if err == nil &&
			secrets.VerifySupplierSessionToken(
				presentedToken, session.TokenHash) &&
			session.CompanyID == challenge.CompanyID &&
			session.SupplierID == challenge.SupplierID &&
			session.RecipientEmailNormalized ==
				challenge.RecipientEmailNormalized &&
			session.RevokedAt == nil &&
			session.SlidingExpiresAt.After(at) &&
			session.AbsoluteExpiresAt.After(at) {
			return VerificationSessionTarget{
				OperationID: operationID, SupplierSessionID: session.ID,
				TokenGeneration: session.TokenGeneration + 1,
				TokenKeyVersion: s.sessionKeys.ActiveVersion(),
			}, nil
		}
		if err != nil && !errors.Is(err, ErrSupplierSessionNotFound) {
			// A real repository outage cannot be downgraded into "new
			// browser"; doing so would duplicate sessions during an outage.
			return VerificationSessionTarget{}, err
		}
	}

	sessionID, err := s.tokens.Generate()
	if err != nil {
		return VerificationSessionTarget{}, err
	}
	if !IsCanonicalOpaqueCredential(sessionID) {
		return VerificationSessionTarget{}, ErrSupplierAccessNotConfigured
	}
	return VerificationSessionTarget{
		OperationID: operationID, SupplierSessionID: sessionID,
		TokenGeneration: 1,
		TokenKeyVersion: s.sessionKeys.ActiveVersion(),
	}, nil
}

func (s *Service) verifySubmittedChallengeCode(
	challenge EmailVerificationChallenge, submitted string) (bool, error) {
	return s.codeKeys.VerifyCode(challenge.CodeKeyVersion,
		secrets.SupplierVerificationCodeContext{
			ChallengeID: challenge.ID, CompanyID: challenge.CompanyID,
			SupplierID: challenge.SupplierID, InvitationID: challenge.InvitationID,
			AccessGeneration:         challenge.AccessGeneration,
			NormalizedRecipientEmail: challenge.RecipientEmailNormalized,
		}, submitted, challenge.CodeVerifier)
}

func (s *Service) validateChallengeInvitation(ctx context.Context,
	challenge EmailVerificationChallenge, at time.Time) error {

	current, found, err := s.validator.ValidateInvitationAccess(
		ctx, challenge.CompanyID, challenge.InvitationID, at)
	if err != nil {
		return err
	}
	if !found || current.CompanyID != challenge.CompanyID ||
		current.SupplierID != challenge.SupplierID ||
		current.InvitationID != challenge.InvitationID ||
		current.NormalizedRecipientEmail !=
			challenge.RecipientEmailNormalized ||
		current.AccessGeneration != challenge.AccessGeneration {
		return ErrInvalidSupplierCredential
	}
	return nil
}

func (s *Service) recoverVerificationSession(ctx context.Context,
	challenge EmailVerificationChallenge,
	validatedAt time.Time) (VerifyChallengeResult, error) {

	if challenge.ConsumedAt == nil ||
		challenge.ConsumedOperationID == "" ||
		challenge.SupplierSessionID == "" ||
		challenge.TargetTokenGeneration < 1 ||
		challenge.SessionTokenKeyVersion < 1 {
		return VerifyChallengeResult{}, ErrInvalidSupplierCredential
	}
	if challenge.TargetTokenGeneration == 1 {
		return s.recoverNewVerificationSession(ctx, challenge, validatedAt)
	}
	return s.recoverExistingVerificationSession(
		ctx, challenge, validatedAt)
}

func (s *Service) recoverNewVerificationSession(ctx context.Context,
	challenge EmailVerificationChallenge,
	validatedAt time.Time) (VerifyChallengeResult, error) {

	credentialContext := secrets.SupplierSessionTokenContext{
		SessionID:       challenge.SupplierSessionID,
		TokenGeneration: challenge.TargetTokenGeneration,
		CompanyID:       challenge.CompanyID, SupplierID: challenge.SupplierID,
		NormalizedRecipientEmail: challenge.RecipientEmailNormalized,
	}
	rawToken, err := s.sessionKeys.DeriveSessionToken(
		challenge.SessionTokenKeyVersion, credentialContext)
	if err != nil {
		return VerifyChallengeResult{}, err
	}
	csrfToken, err := s.sessionKeys.DeriveCSRFToken(
		challenge.SessionTokenKeyVersion, credentialContext)
	if err != nil {
		return VerifyChallengeResult{}, err
	}
	verifiedAt := *challenge.ConsumedAt
	expected := SupplierSession{
		ID: challenge.SupplierSessionID, CompanyID: challenge.CompanyID,
		SupplierID:               challenge.SupplierID,
		RecipientEmailNormalized: challenge.RecipientEmailNormalized,
		TokenHash:                secrets.HashSupplierSessionToken(rawToken),
		TokenGeneration:          challenge.TargetTokenGeneration,
		TokenKeyVersion:          challenge.SessionTokenKeyVersion,
		CreatedFromChallengeID:   challenge.ID,
		LastVerifiedChallengeID:  challenge.ID,
		LastVerifiedAt:           verifiedAt,
		SlidingExpiresAt: verifiedAt.Add(
			supplierSessionSlidingLifetime),
		AbsoluteExpiresAt: verifiedAt.Add(
			supplierSessionAbsoluteLifetime),
		LastUsedAt: verifiedAt, Revision: 1, SchemaVersion: 1,
	}
	session, err := s.sessions.FindSession(
		ctx, challenge.CompanyID, challenge.SupplierSessionID)
	inserted := false
	if errors.Is(err, ErrSupplierSessionNotFound) {
		session, err = s.sessions.CreateSession(ctx, expected)
		inserted = err == nil
		if errors.Is(err, ErrSupplierSessionAlreadyExists) {
			session, err = s.sessions.FindSession(
				ctx, challenge.CompanyID, challenge.SupplierSessionID)
		}
	}
	if err != nil {
		return VerifyChallengeResult{}, err
	}
	if !sessionMatchesCreatedRecovery(session, expected) ||
		session.RevokedAt != nil {
		return VerifyChallengeResult{}, ErrInvalidSupplierCredential
	}
	if inserted && s.audit != nil {
		_ = s.audit.RecordSessionCreated(
			ctx, challenge.CompanyID, challenge.SupplierID,
			challenge.InvitationID, challenge.ID, session.ID,
			challenge.AccessGeneration, session.TokenGeneration, verifiedAt)
	}

	if err := s.ensureVerifiedInvitationBinding(
		ctx, challenge, session, verifiedAt); err != nil {
		return VerifyChallengeResult{}, err
	}
	// Invitation state may rotate between challenge consumption and the two
	// module-owned writes. This final re-read is the fail-closed authorization
	// boundary; any stale session/binding remains harmless history.
	if err := s.validateChallengeInvitation(
		ctx, challenge, validatedAt); err != nil {
		return VerifyChallengeResult{}, err
	}
	return VerifyChallengeResult{
		SessionID: session.ID, InvitationID: challenge.InvitationID,
		AccessGeneration: challenge.AccessGeneration,
		SessionToken:     rawToken, CSRFToken: csrfToken,
		SlidingExpiresAt:  session.SlidingExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt,
	}, nil
}

func (s *Service) recoverExistingVerificationSession(ctx context.Context,
	challenge EmailVerificationChallenge,
	validatedAt time.Time) (VerifyChallengeResult, error) {

	credentialContext := secrets.SupplierSessionTokenContext{
		SessionID:       challenge.SupplierSessionID,
		TokenGeneration: challenge.TargetTokenGeneration,
		CompanyID:       challenge.CompanyID, SupplierID: challenge.SupplierID,
		NormalizedRecipientEmail: challenge.RecipientEmailNormalized,
	}
	rawToken, err := s.sessionKeys.DeriveSessionToken(
		challenge.SessionTokenKeyVersion, credentialContext)
	if err != nil {
		return VerifyChallengeResult{}, err
	}
	csrfToken, err := s.sessionKeys.DeriveCSRFToken(
		challenge.SessionTokenKeyVersion, credentialContext)
	if err != nil {
		return VerifyChallengeResult{}, err
	}
	tokenHash := secrets.HashSupplierSessionToken(rawToken)
	verifiedAt := *challenge.ConsumedAt

	var session SupplierSession
	rotated := false
	for attempt := 0; attempt < maxConfirmationCASAttempts; attempt++ {
		session, err = s.sessions.FindSession(
			ctx, challenge.CompanyID, challenge.SupplierSessionID)
		if err != nil {
			if errors.Is(err, ErrSupplierSessionNotFound) {
				return VerifyChallengeResult{}, ErrInvalidSupplierCredential
			}
			return VerifyChallengeResult{}, err
		}
		if session.CompanyID != challenge.CompanyID ||
			session.SupplierID != challenge.SupplierID ||
			session.RecipientEmailNormalized !=
				challenge.RecipientEmailNormalized ||
			session.RevokedAt != nil {
			return VerifyChallengeResult{}, ErrInvalidSupplierCredential
		}
		if session.TokenGeneration == challenge.TargetTokenGeneration {
			if session.TokenKeyVersion !=
				challenge.SessionTokenKeyVersion ||
				session.TokenHash != tokenHash {
				return VerifyChallengeResult{},
					ErrInvalidSupplierCredential
			}
			break
		}
		if session.TokenGeneration !=
			challenge.TargetTokenGeneration-1 {
			// A later verification already advanced this browser. Never
			// reproduce the obsolete credential recorded by this challenge.
			return VerifyChallengeResult{}, ErrInvalidSupplierCredential
		}
		candidate := session
		candidate.TokenHash = tokenHash
		candidate.TokenGeneration = challenge.TargetTokenGeneration
		candidate.TokenKeyVersion = challenge.SessionTokenKeyVersion
		candidate.LastVerifiedChallengeID = challenge.ID
		candidate.LastVerifiedAt = verifiedAt
		candidate.SlidingExpiresAt = verifiedAt.Add(
			supplierSessionSlidingLifetime)
		candidate.AbsoluteExpiresAt = verifiedAt.Add(
			supplierSessionAbsoluteLifetime)
		candidate.LastUsedAt = verifiedAt
		candidate.Revision = session.Revision + 1
		session, err = s.sessions.ReplaceSessionCAS(
			ctx, candidate, session.Revision)
		if err == nil {
			rotated = true
			break
		}
		if errors.Is(err, ErrSupplierSessionConflict) {
			continue
		}
		return VerifyChallengeResult{}, err
	}
	if session.TokenGeneration != challenge.TargetTokenGeneration ||
		session.TokenKeyVersion != challenge.SessionTokenKeyVersion ||
		session.TokenHash != tokenHash {
		return VerifyChallengeResult{}, ErrInvalidSupplierCredential
	}
	if rotated && s.audit != nil {
		_ = s.audit.RecordSessionReverified(
			ctx, challenge.CompanyID, challenge.SupplierID,
			challenge.InvitationID, challenge.ID, session.ID,
			challenge.AccessGeneration, session.TokenGeneration, verifiedAt)
	}

	if err := s.ensureVerifiedInvitationBinding(
		ctx, challenge, session, verifiedAt); err != nil {
		return VerifyChallengeResult{}, err
	}
	if err := s.validateChallengeInvitation(
		ctx, challenge, validatedAt); err != nil {
		return VerifyChallengeResult{}, err
	}
	return VerifyChallengeResult{
		SessionID: session.ID, InvitationID: challenge.InvitationID,
		AccessGeneration: challenge.AccessGeneration,
		SessionToken:     rawToken, CSRFToken: csrfToken,
		SlidingExpiresAt:  session.SlidingExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt,
	}, nil
}

func sessionMatchesCreatedRecovery(
	session, expected SupplierSession) bool {
	return session.ID == expected.ID &&
		session.CompanyID == expected.CompanyID &&
		session.SupplierID == expected.SupplierID &&
		session.RecipientEmailNormalized ==
			expected.RecipientEmailNormalized &&
		session.TokenHash == expected.TokenHash &&
		session.TokenGeneration == expected.TokenGeneration &&
		session.TokenKeyVersion == expected.TokenKeyVersion &&
		session.CreatedFromChallengeID ==
			expected.CreatedFromChallengeID &&
		session.LastVerifiedChallengeID ==
			expected.LastVerifiedChallengeID &&
		session.LastVerifiedAt.Equal(expected.LastVerifiedAt) &&
		session.SlidingExpiresAt.Equal(expected.SlidingExpiresAt) &&
		session.AbsoluteExpiresAt.Equal(expected.AbsoluteExpiresAt)
}

func (s *Service) ensureVerifiedInvitationBinding(ctx context.Context,
	challenge EmailVerificationChallenge, session SupplierSession,
	boundAt time.Time) error {

	for attempt := 0; attempt < maxConfirmationCASAttempts; attempt++ {
		binding, err := s.bindings.FindBinding(
			ctx, challenge.CompanyID, session.ID, challenge.InvitationID)
		if err == nil {
			if binding.SupplierID != session.SupplierID ||
				binding.NormalizedRecipientEmail !=
					session.RecipientEmailNormalized {
				return ErrInvalidSupplierCredential
			}
			if binding.AccessGeneration == challenge.AccessGeneration {
				return nil
			}
			if binding.AccessGeneration > challenge.AccessGeneration {
				return ErrInvalidSupplierCredential
			}
			// A binding advances only after successful verification of this
			// exact invitation generation. Its original BoundAt remains
			// history while LastValidatedAt records the fresh proof.
			candidate := binding
			candidate.AccessGeneration = challenge.AccessGeneration
			candidate.LastValidatedAt = boundAt
			candidate.Revision = binding.Revision + 1
			if _, err := s.bindings.ReplaceBindingCAS(
				ctx, candidate, binding.Revision); err == nil {
				return nil
			} else if errors.Is(
				err, ErrSessionInvitationBindingConflict) {
				continue
			} else {
				return err
			}
		}
		if !errors.Is(err, ErrSessionInvitationBindingNotFound) {
			return err
		}
		bindingID, err := s.tokens.Generate()
		if err != nil {
			return err
		}
		if !IsCanonicalOpaqueCredential(bindingID) {
			return ErrSupplierAccessNotConfigured
		}
		_, err = s.bindings.CreateBinding(ctx,
			SupplierSessionInvitationBinding{
				ID: bindingID, CompanyID: challenge.CompanyID,
				SupplierSessionID: session.ID, SupplierID: session.SupplierID,
				NormalizedRecipientEmail: session.RecipientEmailNormalized,
				InvitationID:             challenge.InvitationID,
				AccessGeneration:         challenge.AccessGeneration,
				BoundAt:                  boundAt, LastValidatedAt: boundAt, Revision: 1,
			})
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrSessionInvitationBindingAlreadyExists) {
			continue
		}
		return err
	}
	return ErrSessionInvitationBindingConflict
}
