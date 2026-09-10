package supplieraccess

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	platformmail "github.com/shananth/renovation-platform/backend/internal/platform/mail"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

// AccessExchangeStore is the narrow persistence surface needed by the clicked
// invitation link. Later challenge operations extend the service with their own
// consumer-owned store capabilities.
type AccessExchangeStore interface {
	CreateExchange(
		ctx context.Context,
		exchange SupplierAccessExchange,
	) (SupplierAccessExchange, error)
}

type ChallengeAccessExchangeStore interface {
	FindExchangeByTokenHash(context.Context, string) (SupplierAccessExchange, error)
	ConsumeForChallenge(context.Context, string, string, time.Time) (
		SupplierAccessExchange, error)
}

type VerificationChallengeStore interface {
	CreateChallenge(context.Context, EmailVerificationChallenge) (
		EmailVerificationChallenge, error)
	FindChallenge(context.Context, string) (EmailVerificationChallenge, error)
	FindChallengeByExchange(context.Context, string) (
		EmailVerificationChallenge, error)
	FindChallengeByOperation(context.Context, string, string) (
		EmailVerificationChallenge, error)
	FindLatestChallenge(context.Context, string, string, int64) (
		EmailVerificationChallenge, error)
	SupersedeOlderChallenges(context.Context, EmailVerificationChallenge, time.Time) (
		[]string, error)
	RecordFailedConfirmation(context.Context, EmailVerificationChallenge, time.Time) (
		EmailVerificationChallenge, error)
	ConsumeForVerification(context.Context, EmailVerificationChallenge,
		VerificationSessionTarget, time.Time) (EmailVerificationChallenge, error)
}

type VerificationDeliveryStore interface {
	CreateAttempt(context.Context, VerificationDeliveryAttempt) (
		VerificationDeliveryAttempt, error)
	FindAttemptByOperation(context.Context, string, string, string) (
		VerificationDeliveryAttempt, error)
	TransitionPending(context.Context, string, string, VerificationDeliveryStatus,
		*VerificationDeliveryFailureCode, time.Time) (
		VerificationDeliveryAttempt, error)
	ObsoletePendingForChallenges(context.Context, string, []string, time.Time) error
}

type VerificationRateClaimer interface {
	Claim(context.Context, VerificationRateLimitScope, string, string, time.Time) error
}

type VerificationRateFingerprinter interface {
	FingerprintIdentity(string, string, string, string, int64) (string, error)
	FingerprintClientAddress(netip.Addr) (string, error)
	FingerprintChallengeResend(string, string) (string, error)
}

type SupplierSessionStore interface {
	CreateSession(context.Context, SupplierSession) (SupplierSession, error)
	FindSessionByTokenHash(context.Context, string) (SupplierSession, error)
	FindSession(context.Context, string, string) (SupplierSession, error)
	ReplaceSessionCAS(context.Context, SupplierSession, int64) (
		SupplierSession, error)
}

type SessionInvitationBindingStore interface {
	CreateBinding(context.Context, SupplierSessionInvitationBinding) (
		SupplierSessionInvitationBinding, error)
	FindBinding(context.Context, string, string, string) (
		SupplierSessionInvitationBinding, error)
	ListSessionBindings(context.Context, string, string) (
		[]SupplierSessionInvitationBinding, error)
	ReplaceBindingCAS(context.Context, SupplierSessionInvitationBinding, int64) (
		SupplierSessionInvitationBinding, error)
}

// SupplierAccessAuditRecorder is consumer-owned and primitive-only. Its
// signatures intentionally provide no place for browser credentials, codes,
// recipient email addresses, request data, or delivery-provider details.
type SupplierAccessAuditRecorder interface {
	RecordChallengeRequested(context.Context, string, string, string, string,
		int64, time.Time) error
	RecordChallengeDeliveryRetried(context.Context, string, string, string,
		string, string, int64, time.Time) error
	RecordVerificationFailed(context.Context, string, string, string, string,
		int64, string, time.Time) error
	RecordVerificationSucceeded(context.Context, string, string, string,
		string, string, int64, time.Time) error
	RecordSessionCreated(context.Context, string, string, string, string,
		string, int64, int64, time.Time) error
	RecordSessionReverified(context.Context, string, string, string, string,
		string, int64, int64, time.Time) error
	RecordSessionRenewed(context.Context, string, string, string, int64,
		time.Time, time.Time) error
	RecordSessionRevoked(context.Context, string, string, string, int64,
		time.Time) error
}

// OpaqueTokenGenerator supplies cryptographically random, canonical 256-bit
// base64url values for public handles and browser credentials.
type OpaqueTokenGenerator interface {
	Generate() (string, error)
}

// Service owns all public Supplier verification/session workflows. Domain
// dependencies enter through narrow capabilities; it never reads another
// module's repository.
type Service struct {
	invitations        InvitationAccessResolver
	validator          InvitationAccessValidator
	views              InvitationViewRecorder
	exchanges          AccessExchangeStore
	challengeExchanges ChallengeAccessExchangeStore
	tokens             OpaqueTokenGenerator
	challenges         VerificationChallengeStore
	deliveries         VerificationDeliveryStore
	codeKeys           *secrets.SupplierVerificationCodeKeyring
	fingerprints       VerificationRateFingerprinter
	rates              VerificationRateClaimer
	mailer             platformmail.EmailSender

	// outcomes is set after construction: awards is built later, because it
	// depends on capabilities this module's siblings provide (§8A.1).
	outcomes          SupplierOutcomeSource
	trustedProxyCIDRs []netip.Prefix
	sessions          SupplierSessionStore
	bindings          SessionInvitationBindingStore
	sessionKeys       *secrets.SupplierSessionTokenKeyring
	audit             SupplierAccessAuditRecorder
}

type ServiceOption func(*Service)

func NewService(options ...ServiceOption) *Service {
	service := &Service{tokens: CryptographicOpaqueTokenGenerator{}}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithInvitationAccess(
	resolver InvitationAccessResolver,
	recorder InvitationViewRecorder,
) ServiceOption {
	return func(service *Service) {
		service.invitations = resolver
		if validator, ok := resolver.(InvitationAccessValidator); ok {
			service.validator = validator
		}
		service.views = recorder
	}
}

func WithAccessExchangeStore(store AccessExchangeStore) ServiceOption {
	return func(service *Service) {
		service.exchanges = store
		if challengeStore, ok := store.(ChallengeAccessExchangeStore); ok {
			service.challengeExchanges = challengeStore
		}
	}
}

func WithOpaqueTokenGenerator(generator OpaqueTokenGenerator) ServiceOption {
	return func(service *Service) {
		service.tokens = generator
	}
}

func WithVerificationStores(challenges VerificationChallengeStore,
	deliveries VerificationDeliveryStore) ServiceOption {
	return func(service *Service) {
		service.challenges = challenges
		service.deliveries = deliveries
	}
}

func WithVerificationSecurity(codeKeys *secrets.SupplierVerificationCodeKeyring,
	fingerprints VerificationRateFingerprinter,
	rates VerificationRateClaimer) ServiceOption {
	return func(service *Service) {
		service.codeKeys = codeKeys
		service.fingerprints = fingerprints
		service.rates = rates
	}
}

func WithVerificationMailer(mailer platformmail.EmailSender) ServiceOption {
	return func(service *Service) {
		service.mailer = mailer
	}
}

func WithSessionStores(sessions SupplierSessionStore,
	bindings SessionInvitationBindingStore) ServiceOption {
	return func(service *Service) {
		service.sessions = sessions
		service.bindings = bindings
	}
}

func WithSessionSecurity(
	sessionKeys *secrets.SupplierSessionTokenKeyring) ServiceOption {
	return func(service *Service) {
		service.sessionKeys = sessionKeys
	}
}

func WithAuditRecorder(recorder SupplierAccessAuditRecorder) ServiceOption {
	return func(service *Service) {
		service.audit = recorder
	}
}

// WithTrustedProxyCIDRs declares the only direct peers whose forwarding
// headers may influence verification rate limiting. The copied slice keeps
// later caller mutations from silently changing the service's trust boundary.
func WithTrustedProxyCIDRs(prefixes []netip.Prefix) ServiceOption {
	return func(service *Service) {
		service.trustedProxyCIDRs = append([]netip.Prefix(nil), prefixes...)
	}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of any of this module's public repository interfaces. Only the real
// Mongo repositories implement it; a fake used in unrelated tests simply does
// not satisfy this interface and is unaffected.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// companyChallengeLister is the unexported capability used to enumerate a
// company's challenges before they are deleted, so their rate-limit
// fingerprints can be re-derived (see rateLimitHashDeleter).
type companyChallengeLister interface {
	ListChallengesForCompany(ctx context.Context, companyID string) (
		[]EmailVerificationChallenge, error)
}

// rateLimitHashDeleter is the unexported capability used to remove one
// VerificationRateLimitState by its exact scope and hash, since that
// collection has no companyId field to bulk-delete by (see the Mongo
// repository's DeleteByScopeAndHash doc comment).
type rateLimitHashDeleter interface {
	DeleteByScopeAndHash(ctx context.Context, scope VerificationRateLimitScope,
		scopeKeyHash string) error
}

// DeleteAllForCompany permanently removes every AccessExchange,
// VerificationChallenge, VerificationDelivery, SupplierSession, and
// SessionInvitationBinding owned by companyID, across all six of this
// module's collections. Development-tool use only (demoseed reset, design
// spec §6.6) — no production code path calls this. Idempotent: calling it
// when nothing remains for companyID is a no-op success, not an error.
//
// VerificationRateLimit is the one collection this cannot bulk-delete by
// companyID, because it stores no such field — only an irreversible HMAC
// hash of each rate scope's identity inputs (companyID among them, but not
// recoverable from the hash alone). Instead, this method lists the
// company's own challenges (each one already recording the exact
// SupplierID/InvitationID/RecipientEmail/AccessGeneration/ID that
// FingerprintIdentity and FingerprintChallengeResend were called with when
// the challenge was created) and re-derives those two hashes per challenge to
// delete the matching rate-limit documents directly. The third scope,
// FingerprintClientAddress, has no companyID component at all — it is
// deliberately left alone and relies on its own TTL index to expire.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	if s.fingerprints != nil {
		if err := s.deleteRateLimitsForCompany(ctx, companyID); err != nil {
			return err
		}
	}

	exchangeDeleter, ok := s.exchanges.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("supplieraccess: exchange store %T does not support DeleteAllForCompany", s.exchanges)
	}
	if err := exchangeDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	challengeDeleter, ok := s.challenges.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("supplieraccess: challenge store %T does not support DeleteAllForCompany", s.challenges)
	}
	if err := challengeDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	deliveryDeleter, ok := s.deliveries.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("supplieraccess: verification delivery store %T does not support DeleteAllForCompany", s.deliveries)
	}
	if err := deliveryDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	sessionDeleter, ok := s.sessions.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("supplieraccess: session store %T does not support DeleteAllForCompany", s.sessions)
	}
	if err := sessionDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	bindingDeleter, ok := s.bindings.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("supplieraccess: binding store %T does not support DeleteAllForCompany", s.bindings)
	}
	return bindingDeleter.DeleteAllForCompany(ctx, companyID)
}

// deleteRateLimitsForCompany re-derives and deletes the identity- and
// resend-scoped rate-limit documents companyID's own challenges would have
// claimed. See DeleteAllForCompany's doc comment for why this collection
// cannot be bulk-deleted by companyID directly.
func (s *Service) deleteRateLimitsForCompany(ctx context.Context, companyID string) error {
	lister, ok := s.challenges.(companyChallengeLister)
	if !ok {
		return fmt.Errorf("supplieraccess: challenge store %T does not support ListChallengesForCompany", s.challenges)
	}
	rateDeleter, ok := s.rates.(rateLimitHashDeleter)
	if !ok {
		return fmt.Errorf("supplieraccess: rate limit store %T does not support DeleteByScopeAndHash", s.rates)
	}

	challenges, err := lister.ListChallengesForCompany(ctx, companyID)
	if err != nil {
		return err
	}
	for _, challenge := range challenges {
		identityHash, err := s.fingerprints.FingerprintIdentity(
			challenge.CompanyID, challenge.SupplierID,
			challenge.RecipientEmailNormalized, challenge.InvitationID,
			challenge.AccessGeneration)
		if err != nil {
			return err
		}
		if err := rateDeleter.DeleteByScopeAndHash(ctx,
			VerificationRateScopeIdentity, identityHash); err != nil {
			return err
		}

		resendHash, err := s.fingerprints.FingerprintChallengeResend(
			challenge.CompanyID, challenge.ID)
		if err != nil {
			return err
		}
		if err := rateDeleter.DeleteByScopeAndHash(ctx,
			VerificationRateScopeResend, resendHash); err != nil {
			return err
		}
	}
	return nil
}
