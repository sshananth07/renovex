package supplieraccess

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	platformmail "github.com/shananth/renovation-platform/backend/internal/platform/mail"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

const (
	verificationChallengeLifetime = 10 * time.Minute
	verificationChallengeAttempts = 5
)

type CreateChallengeInput struct {
	ExchangeToken string
	OperationID   string
	ClientAddress netip.Addr
	RequestedAt   time.Time
}

type ResendChallengeInput struct {
	ChallengeID string
	OperationID string
	RequestedAt time.Time
}

type VerificationChallengeResult struct {
	ChallengeID    string
	ExpiresAt      time.Time
	DeliveryStatus VerificationDeliveryStatus
}

func (s *Service) verificationConfigured() bool {
	return s.validator != nil && s.challengeExchanges != nil &&
		s.challenges != nil && s.deliveries != nil && s.codeKeys != nil &&
		s.fingerprints != nil && s.rates != nil && s.mailer != nil &&
		s.tokens != nil
}

// CreateChallenge turns one live access exchange into one single-use challenge.
// Rate reservations and authoritative records precede synchronous email.
func (s *Service) CreateChallenge(ctx context.Context,
	input CreateChallengeInput) (VerificationChallengeResult, error) {

	input.OperationID = strings.TrimSpace(input.OperationID)
	if !s.verificationConfigured() {
		return VerificationChallengeResult{}, ErrSupplierAccessNotConfigured
	}
	if !IsCanonicalOpaqueCredential(input.ExchangeToken) {
		// A malformed exchange is still a credential failure. Keeping it out
		// of MongoDB is an input-hardening detail, not public information.
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}
	if procurementlimits.ValidateID(input.OperationID) != nil || !input.ClientAddress.IsValid() ||
		input.RequestedAt.IsZero() {
		return VerificationChallengeResult{}, ErrInvalidVerificationRequest
	}

	exchange, err := s.challengeExchanges.FindExchangeByTokenHash(
		ctx, HashAccessExchangeToken(input.ExchangeToken))
	if errors.Is(err, ErrAccessExchangeNotFound) {
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	if err := s.revalidateExchange(ctx, exchange, input.RequestedAt); err != nil {
		return VerificationChallengeResult{}, err
	}

	// A challenge may already exist after a crash between challenge insertion,
	// exchange consumption, delivery intent, and status recording.
	existing, findErr := s.challenges.FindChallengeByExchange(ctx, exchange.ID)
	if findErr == nil {
		if existing.ChallengeOperationID != input.OperationID ||
			!challengeMatchesExchange(existing, exchange) {
			return VerificationChallengeResult{}, &ChallengeOperationConflictError{ExistingChallengeID: existing.ID}
		}
		return s.recoverCreatedChallenge(ctx, exchange, existing, input.RequestedAt)
	}
	if !errors.Is(findErr, ErrVerificationChallengeNotFound) {
		return VerificationChallengeResult{}, findErr
	}
	// Expiry prevents a new challenge, but it must not strand a still-live
	// challenge already committed from this exchange. The unique ExchangeID
	// lookup above is the recovery authority for that same operation.
	if !exchange.ExpiresAt.After(input.RequestedAt) {
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}
	if exchange.ConsumedAt != nil || exchange.ChallengeID != nil {
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}

	if err := s.claimChallengeCreationRates(
		ctx, exchange, input.ClientAddress, input.RequestedAt); err != nil {
		return VerificationChallengeResult{}, err
	}
	if byOperation, operationErr := s.challenges.FindChallengeByOperation(
		ctx, exchange.CompanyID, input.OperationID); operationErr == nil {
		if byOperation.ExchangeID != exchange.ID ||
			!challengeMatchesExchange(byOperation, exchange) {
			return VerificationChallengeResult{}, &ChallengeOperationConflictError{ExistingChallengeID: byOperation.ID}
		}
		return s.recoverCreatedChallenge(
			ctx, exchange, byOperation, input.RequestedAt)
	} else if !errors.Is(operationErr, ErrVerificationChallengeNotFound) {
		return VerificationChallengeResult{}, operationErr
	}

	challenge, err := s.buildChallenge(exchange, input.OperationID, input.RequestedAt)
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	created, err := s.challenges.CreateChallenge(ctx, challenge)
	if err != nil {
		if errors.Is(err, ErrExchangeChallengeAlreadyExists) {
			winner, loadErr := s.challenges.FindChallengeByExchange(ctx, exchange.ID)
			if loadErr != nil {
				return VerificationChallengeResult{}, loadErr
			}
			if winner.ChallengeOperationID != input.OperationID ||
				!challengeMatchesExchange(winner, exchange) {
				return VerificationChallengeResult{}, &ChallengeOperationConflictError{ExistingChallengeID: winner.ID}
			}
			return s.recoverCreatedChallenge(ctx, exchange, winner, input.RequestedAt)
		}
		if errors.Is(err, ErrChallengeOperationAlreadyUsed) {
			winner, loadErr := s.challenges.FindChallengeByOperation(
				ctx, exchange.CompanyID, input.OperationID)
			if loadErr != nil {
				return VerificationChallengeResult{}, loadErr
			}
			if winner.ExchangeID != exchange.ID ||
				!challengeMatchesExchange(winner, exchange) {
				return VerificationChallengeResult{}, &ChallengeOperationConflictError{ExistingChallengeID: winner.ID}
			}
			return s.recoverCreatedChallenge(ctx, exchange, winner, input.RequestedAt)
		}
		return VerificationChallengeResult{}, err
	}
	// Only the insert winner records this transition. Recovery through either
	// unique key observes the same challenge without manufacturing an event.
	if s.audit != nil {
		_ = s.audit.RecordChallengeRequested(
			ctx, created.CompanyID, created.SupplierID, created.InvitationID,
			created.ID, created.AccessGeneration, created.CreatedAt)
	}
	return s.recoverCreatedChallenge(ctx, exchange, created, input.RequestedAt)
}

func (s *Service) revalidateExchange(ctx context.Context,
	exchange SupplierAccessExchange, at time.Time) error {

	current, found, err := s.validator.ValidateInvitationAccess(
		ctx, exchange.CompanyID, exchange.InvitationID, at)
	if err != nil {
		return err
	}
	if !found || current.CompanyID != exchange.CompanyID ||
		current.SupplierID != exchange.SupplierID ||
		current.InvitationID != exchange.InvitationID ||
		current.NormalizedRecipientEmail != exchange.NormalizedRecipientEmail ||
		current.AccessGeneration != exchange.AccessGeneration {
		return ErrInvalidSupplierCredential
	}
	return nil
}

func (s *Service) claimChallengeCreationRates(ctx context.Context,
	exchange SupplierAccessExchange, clientAddress netip.Addr, at time.Time) error {

	clientHash, err := s.fingerprints.FingerprintClientAddress(clientAddress)
	if err != nil {
		return ErrInvalidVerificationRequest
	}
	// Client scope is first by design. An abusive address cannot consume the
	// victim invitation's stricter identity allowance.
	if err := s.rates.Claim(ctx, VerificationRateScopeClient,
		clientHash, exchange.ID, at); err != nil {
		return err
	}
	identityHash, err := s.fingerprints.FingerprintIdentity(
		exchange.CompanyID, exchange.SupplierID,
		exchange.NormalizedRecipientEmail, exchange.InvitationID,
		exchange.AccessGeneration)
	if err != nil {
		return err
	}
	return s.rates.Claim(ctx, VerificationRateScopeIdentity,
		identityHash, exchange.ID, at)
}

func (s *Service) buildChallenge(exchange SupplierAccessExchange,
	operationID string, createdAt time.Time) (EmailVerificationChallenge, error) {

	challengeID, err := s.tokens.Generate()
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	keyVersion := s.codeKeys.ActiveVersion()
	context := secrets.SupplierVerificationCodeContext{
		ChallengeID: challengeID, CompanyID: exchange.CompanyID,
		SupplierID: exchange.SupplierID, InvitationID: exchange.InvitationID,
		AccessGeneration:         exchange.AccessGeneration,
		NormalizedRecipientEmail: exchange.NormalizedRecipientEmail,
	}
	code, err := s.codeKeys.DeriveCode(keyVersion, context)
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	verifier, err := s.codeKeys.DeriveCodeVerifier(
		keyVersion, challengeID, code)
	if err != nil {
		return EmailVerificationChallenge{}, err
	}
	return EmailVerificationChallenge{
		ID: challengeID, ExchangeID: exchange.ID,
		CompanyID: exchange.CompanyID, SupplierID: exchange.SupplierID,
		RecipientEmailNormalized: exchange.NormalizedRecipientEmail,
		InvitationID:             exchange.InvitationID,
		AccessGeneration:         exchange.AccessGeneration,
		ChallengeOperationID:     operationID,
		CodeVerifier:             verifier, CodeKeyVersion: keyVersion,
		AttemptsRemaining: verificationChallengeAttempts,
		ExpiresAt:         createdAt.Add(verificationChallengeLifetime),
		Revision:          1, CreatedAt: createdAt, SchemaVersion: 1,
	}, nil
}

func challengeMatchesExchange(challenge EmailVerificationChallenge,
	exchange SupplierAccessExchange) bool {
	return challenge.ExchangeID == exchange.ID &&
		challenge.CompanyID == exchange.CompanyID &&
		challenge.SupplierID == exchange.SupplierID &&
		challenge.RecipientEmailNormalized == exchange.NormalizedRecipientEmail &&
		challenge.InvitationID == exchange.InvitationID &&
		challenge.AccessGeneration == exchange.AccessGeneration
}

func (s *Service) recoverCreatedChallenge(ctx context.Context,
	exchange SupplierAccessExchange, challenge EmailVerificationChallenge,
	at time.Time) (VerificationChallengeResult, error) {

	if !challenge.ExpiresAt.After(at) || challenge.ConsumedAt != nil ||
		challenge.LockedAt != nil {
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}
	supersededIDs, err := s.challenges.SupersedeOlderChallenges(
		ctx, challenge, at)
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	if err := s.deliveries.ObsoletePendingForChallenges(
		ctx, challenge.CompanyID, supersededIDs, at); err != nil {
		return VerificationChallengeResult{}, err
	}
	if err := s.requireCurrentChallenge(ctx, challenge); err != nil {
		return VerificationChallengeResult{}, err
	}
	if _, err := s.challengeExchanges.ConsumeForChallenge(
		ctx, exchange.ID, challenge.ID, at); err != nil {
		return VerificationChallengeResult{}, err
	}
	return s.ensureChallengeDelivery(
		ctx, challenge, challenge.ChallengeOperationID, at)
}

func (s *Service) requireCurrentChallenge(ctx context.Context,
	challenge EmailVerificationChallenge) error {

	latest, err := s.challenges.FindLatestChallenge(
		ctx, challenge.CompanyID, challenge.InvitationID,
		challenge.AccessGeneration)
	if err != nil {
		return err
	}
	if latest.ID != challenge.ID || challenge.SupersededAt != nil {
		return ErrVerificationChallengeNotCurrent
	}
	return nil
}

// ResendChallenge sends the same recoverable code through a new immutable
// delivery attempt. It never changes attempts, expiry, verifier, or revision.
func (s *Service) ResendChallenge(ctx context.Context,
	input ResendChallengeInput) (VerificationChallengeResult, error) {

	input.ChallengeID = strings.TrimSpace(input.ChallengeID)
	input.OperationID = strings.TrimSpace(input.OperationID)
	if !s.verificationConfigured() {
		return VerificationChallengeResult{}, ErrSupplierAccessNotConfigured
	}
	if !IsCanonicalOpaqueCredential(input.ChallengeID) {
		// Handle syntax is checked before global lookup, but malformed and
		// unknown handles remain the same public credential failure.
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}
	if procurementlimits.ValidateID(input.OperationID) != nil || input.RequestedAt.IsZero() {
		return VerificationChallengeResult{}, ErrInvalidVerificationRequest
	}
	challenge, err := s.challenges.FindChallenge(ctx, input.ChallengeID)
	if errors.Is(err, ErrVerificationChallengeNotFound) {
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	if !challenge.ExpiresAt.After(input.RequestedAt) ||
		challenge.SupersededAt != nil || challenge.ConsumedAt != nil ||
		challenge.LockedAt != nil {
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}
	if err := s.requireCurrentChallenge(ctx, challenge); err != nil {
		if errors.Is(err, ErrVerificationChallengeNotCurrent) {
			return VerificationChallengeResult{}, ErrInvalidSupplierCredential
		}
		return VerificationChallengeResult{}, err
	}
	current, found, err := s.validator.ValidateInvitationAccess(
		ctx, challenge.CompanyID, challenge.InvitationID, input.RequestedAt)
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	if !found || current.CompanyID != challenge.CompanyID ||
		current.SupplierID != challenge.SupplierID ||
		current.NormalizedRecipientEmail != challenge.RecipientEmailNormalized ||
		current.AccessGeneration != challenge.AccessGeneration {
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	}

	if existing, findErr := s.deliveries.FindAttemptByOperation(
		ctx, challenge.CompanyID, challenge.ID, input.OperationID); findErr == nil {
		return s.resumeDelivery(ctx, challenge, existing, input.RequestedAt)
	} else if !errors.Is(findErr, ErrVerificationDeliveryAttemptNotFound) {
		return VerificationChallengeResult{}, findErr
	}
	// The first resend may not occur until one minute after challenge creation;
	// this protects the initial delivery without counting it against five
	// actual resend operations.
	firstAllowed := challenge.CreatedAt.Add(time.Minute)
	if input.RequestedAt.Before(firstAllowed) {
		return VerificationChallengeResult{},
			&VerificationRateLimitExceeded{RetryAt: firstAllowed}
	}
	resendHash, err := s.fingerprints.FingerprintChallengeResend(
		challenge.CompanyID, challenge.ID)
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	if err := s.rates.Claim(ctx, VerificationRateScopeResend,
		resendHash, input.OperationID, input.RequestedAt); err != nil {
		return VerificationChallengeResult{}, err
	}
	return s.ensureChallengeDelivery(
		ctx, challenge, input.OperationID, input.RequestedAt, true)
}

func (s *Service) ensureChallengeDelivery(ctx context.Context,
	challenge EmailVerificationChallenge, operationID string,
	at time.Time, auditRetry ...bool) (VerificationChallengeResult, error) {

	attempt, err := s.deliveries.FindAttemptByOperation(
		ctx, challenge.CompanyID, challenge.ID, operationID)
	if err == nil {
		return s.resumeDelivery(ctx, challenge, attempt, at)
	}
	if !errors.Is(err, ErrVerificationDeliveryAttemptNotFound) {
		return VerificationChallengeResult{}, err
	}
	attemptID, err := s.tokens.Generate()
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	attempt, err = s.deliveries.CreateAttempt(ctx, VerificationDeliveryAttempt{
		ID: attemptID, CompanyID: challenge.CompanyID,
		ChallengeID: challenge.ID, OperationID: operationID,
		Status: VerificationDeliveryPending, CreatedAt: at, UpdatedAt: at,
	})
	inserted := err == nil
	if errors.Is(err, ErrVerificationDeliveryOperationAlreadyUsed) {
		attempt, err = s.deliveries.FindAttemptByOperation(
			ctx, challenge.CompanyID, challenge.ID, operationID)
	}
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	if inserted && len(auditRetry) > 0 && auditRetry[0] && s.audit != nil {
		// The immutable attempt insert is the retry transition. Mail success or
		// failure is delivery history, not a second retry event.
		_ = s.audit.RecordChallengeDeliveryRetried(
			ctx, challenge.CompanyID, challenge.SupplierID,
			challenge.InvitationID, challenge.ID, attempt.ID,
			challenge.AccessGeneration, at)
	}
	return s.resumeDelivery(ctx, challenge, attempt, at)
}

func (s *Service) resumeDelivery(ctx context.Context,
	challenge EmailVerificationChallenge, attempt VerificationDeliveryAttempt,
	at time.Time) (VerificationChallengeResult, error) {

	result := VerificationChallengeResult{
		ChallengeID: challenge.ID, ExpiresAt: challenge.ExpiresAt,
		DeliveryStatus: attempt.Status,
	}
	switch attempt.Status {
	case VerificationDeliverySent:
		return result, nil
	case VerificationDeliveryFailed:
		return result, ErrVerificationMailDeliveryFailed
	case VerificationDeliveryObsolete:
		return VerificationChallengeResult{}, ErrInvalidSupplierCredential
	case VerificationDeliveryPending:
	default:
		return VerificationChallengeResult{}, ErrInvalidVerificationRequest
	}
	if err := s.requireCurrentChallenge(ctx, challenge); err != nil {
		return VerificationChallengeResult{}, err
	}
	codeContext := secrets.SupplierVerificationCodeContext{
		ChallengeID: challenge.ID, CompanyID: challenge.CompanyID,
		SupplierID: challenge.SupplierID, InvitationID: challenge.InvitationID,
		AccessGeneration:         challenge.AccessGeneration,
		NormalizedRecipientEmail: challenge.RecipientEmailNormalized,
	}
	code, err := s.codeKeys.DeriveCode(challenge.CodeKeyVersion, codeContext)
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	valid, err := s.codeKeys.VerifyCode(
		challenge.CodeKeyVersion, codeContext, code, challenge.CodeVerifier)
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	if !valid {
		return VerificationChallengeResult{}, fmt.Errorf(
			"supplieraccess: persisted verification code verifier is invalid")
	}

	sendErr := s.mailer.Send(ctx, platformmail.Message{
		To:      challenge.RecipientEmailNormalized,
		Subject: "Supplier access verification code",
		Body: fmt.Sprintf(
			"Your supplier access verification code is %s.\n\n"+
				"This code expires in 10 minutes.", code),
	})
	if sendErr != nil {
		failure := VerificationDeliveryFailureCode("send_failed")
		updated, updateErr := s.deliveries.TransitionPending(
			ctx, challenge.CompanyID, attempt.ID,
			VerificationDeliveryFailed, &failure, at)
		if updateErr == nil {
			result.DeliveryStatus = updated.Status
		}
		return result, ErrVerificationMailDeliveryFailed
	}
	updated, err := s.deliveries.TransitionPending(
		ctx, challenge.CompanyID, attempt.ID,
		VerificationDeliverySent, nil, at)
	if errors.Is(err, ErrVerificationDeliveryNotPending) {
		updated, err = s.deliveries.FindAttemptByOperation(
			ctx, challenge.CompanyID, challenge.ID, attempt.OperationID)
	}
	if err != nil {
		return VerificationChallengeResult{}, err
	}
	result.DeliveryStatus = updated.Status
	return result, nil
}
