package supplieroffers

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service struct {
	access      SupplierOfferAccessAuthorizer
	issuedRFQ   IssuedRFQSource
	audit       SupplierOfferAuditRecorder
	chains      SupplierOfferChainRepository
	drafts      SupplierOfferDraftRepository
	versions    SupplierOfferVersionRepository
	eligibility SubmissionEligibilityRepository
	withdrawals WithdrawalRepository
}

// SupplierOfferVersionRepository is the narrow immutable-version surface
// copy-forward and submission consume.
type SupplierOfferVersionRepository interface {
	FindVersion(ctx context.Context, companyID, versionID string) (
		SupplierOfferVersion, bool, error)
}

// SupplierOfferChainRepository is the narrow persistence capability needed by
// draft creation. Later submission methods extend their own explicit boundary
// instead of exposing a generic repository escape hatch.
type SupplierOfferChainRepository interface {
	EnsureOfferChain(
		ctx context.Context,
		companyID string,
		invitationID string,
		issuedRFQVersionID string,
	) (SupplierOfferChain, error)
}

type SupplierOfferDraftRepository interface {
	EnsureUnfinishedDraft(
		ctx context.Context,
		candidate SupplierOfferDraft,
	) (SupplierOfferDraft, bool, error)
}

type ServiceOption func(*Service)

func NewService(options ...ServiceOption) *Service {
	service := &Service{}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithSupplierOfferAccessAuthorizer(
	authorizer SupplierOfferAccessAuthorizer,
) ServiceOption {
	return func(service *Service) { service.access = authorizer }
}

func WithIssuedRFQSource(source IssuedRFQSource) ServiceOption {
	return func(service *Service) { service.issuedRFQ = source }
}

func WithSupplierOfferAuditRecorder(audit SupplierOfferAuditRecorder) ServiceOption {
	return func(service *Service) { service.audit = audit }
}

func WithSupplierOfferChainRepository(
	repository SupplierOfferChainRepository,
) ServiceOption {
	return func(service *Service) { service.chains = repository }
}

func WithSupplierOfferDraftRepository(
	repository SupplierOfferDraftRepository,
) ServiceOption {
	return func(service *Service) { service.drafts = repository }
}

func WithSupplierOfferVersionRepository(
	repository SupplierOfferVersionRepository,
) ServiceOption {
	return func(service *Service) { service.versions = repository }
}

type SupplierOfferMutationContextInput struct {
	SessionToken string
	InvitationID string
	CSRFCookie   string
	CSRFHeader   string
	AccessedAt   time.Time
}

type SupplierOfferReadContextInput struct {
	SessionToken string
	InvitationID string
	AccessedAt   time.Time
}

type AuthorizedSupplierOfferContext struct {
	Access AuthorizedSupplierOfferAccess
	RFQ    IssuedRFQSnapshot
}

// CreateOrGetActiveDraft creates the invitation/version-specific draft from
// authoritative Phase D and RFQ identities. The request contains no Company,
// Supplier, recipient or issued-version field that could override them.
func (service *Service) CreateOrGetActiveDraft(
	ctx context.Context,
	input SupplierOfferMutationContextInput,
) (SupplierOfferDraft, error) {
	if service.chains == nil || service.drafts == nil {
		return SupplierOfferDraft{}, ErrSupplierOffersNotConfigured
	}
	authorized, err := service.ResolveMutationContext(ctx, input)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	if authorized.RFQ.Currency != Phase1Currency {
		return SupplierOfferDraft{}, ErrUnsupportedCurrency
	}

	chain, err := service.chains.EnsureOfferChain(
		ctx,
		authorized.Access.CompanyID,
		authorized.Access.InvitationID,
		authorized.RFQ.ID,
	)
	if err != nil {
		return SupplierOfferDraft{}, err
	}

	lines := make([]SupplierOfferDraftLine, 0, len(authorized.RFQ.Lines))
	for _, issuedLine := range authorized.RFQ.Lines {
		if strings.TrimSpace(issuedLine.ID) == "" {
			return SupplierOfferDraft{}, ErrSupplierOfferAccessInvalid
		}
		lines = append(lines, SupplierOfferDraftLine{
			ID:             bson.NewObjectID().Hex(),
			RFQLineID:      issuedLine.ID,
			ResponseStatus: OfferLineUnanswered,
		})
	}

	candidate := SupplierOfferDraft{
		ID:                 bson.NewObjectID().Hex(),
		CompanyID:          authorized.Access.CompanyID,
		OfferChainID:       chain.ID,
		InvitationID:       authorized.Access.InvitationID,
		IssuedRFQVersionID: authorized.RFQ.ID,
		RecipientIdentity:  authorized.Access.RecipientIdentity,
		Currency:           authorized.RFQ.Currency,
		Status:             DraftActive,
		Lines:              lines,
		Tax: SupplierOfferTax{
			Mode: TaxModeNotApplicable,
		},
		Revision:      1,
		CreatedAt:     input.AccessedAt,
		UpdatedAt:     input.AccessedAt,
		SchemaVersion: SupplierOfferDraftSchemaVersion,
	}
	persisted, _, err := service.drafts.EnsureUnfinishedDraft(ctx, candidate)
	if err != nil {
		return SupplierOfferDraft{}, err
	}

	// An unfinished document is reusable only for the exact authorized owner
	// and RFQ identity. A recipient replacement is handled by its explicit
	// archive CAS rather than silently transferring this mutable draft.
	if persisted.Status != DraftActive ||
		persisted.CompanyID != candidate.CompanyID ||
		persisted.OfferChainID != candidate.OfferChainID ||
		persisted.InvitationID != candidate.InvitationID ||
		persisted.IssuedRFQVersionID != candidate.IssuedRFQVersionID ||
		persisted.RecipientIdentity != candidate.RecipientIdentity ||
		persisted.Currency != candidate.Currency {
		return SupplierOfferDraft{}, ErrOfferDraftConflict
	}
	return persisted, nil
}

// ResolveReadContext deliberately calls the read authorization capability.
// Keeping it separate from mutation means adding a read route cannot weaken
// the CSRF requirement on state-changing routes.
func (service *Service) ResolveReadContext(
	ctx context.Context,
	input SupplierOfferReadContextInput,
) (AuthorizedSupplierOfferContext, error) {
	if service.access == nil || service.issuedRFQ == nil {
		return AuthorizedSupplierOfferContext{}, ErrSupplierOffersNotConfigured
	}
	access, err := service.access.AuthorizeSupplierOfferRead(
		ctx,
		SupplierOfferReadAuthorization{
			SessionToken: input.SessionToken,
			InvitationID: input.InvitationID,
			AccessedAt:   input.AccessedAt,
		},
	)
	if err != nil {
		return AuthorizedSupplierOfferContext{}, err
	}
	return service.resolveAuthorizedContext(
		ctx, access, input.SessionToken, input.InvitationID, input.AccessedAt)
}

// ResolveMutationContext delegates session, binding, invitation and CSRF checks
// to Phase D, then loads only the RFQ identity returned by that authorization.
func (service *Service) ResolveMutationContext(
	ctx context.Context,
	input SupplierOfferMutationContextInput,
) (AuthorizedSupplierOfferContext, error) {
	if service.access == nil || service.issuedRFQ == nil {
		return AuthorizedSupplierOfferContext{}, ErrSupplierOffersNotConfigured
	}

	access, err := service.access.AuthorizeSupplierOfferMutation(
		ctx,
		SupplierOfferMutationAuthorization{
			SupplierOfferReadAuthorization: SupplierOfferReadAuthorization{
				SessionToken: input.SessionToken,
				InvitationID: input.InvitationID,
				AccessedAt:   input.AccessedAt,
			},
			CSRFCookie: input.CSRFCookie,
			CSRFHeader: input.CSRFHeader,
		},
	)
	if err != nil {
		return AuthorizedSupplierOfferContext{}, err
	}
	return service.resolveAuthorizedContext(
		ctx, access, input.SessionToken, input.InvitationID, input.AccessedAt)
}

func (service *Service) resolveAuthorizedContext(
	ctx context.Context,
	access AuthorizedSupplierOfferAccess,
	presentedSessionToken string,
	requestedInvitationID string,
	accessedAt time.Time,
) (AuthorizedSupplierOfferContext, error) {
	if !validAuthorizedOfferAccess(
		access, presentedSessionToken, requestedInvitationID, accessedAt) {
		return AuthorizedSupplierOfferContext{}, ErrSupplierOfferAccessInvalid
	}

	rfq, found, err := service.issuedRFQ.GetIssuedRFQForOffer(
		ctx, access.CompanyID, access.CurrentIssuedRFQVersionID)
	if err != nil {
		return AuthorizedSupplierOfferContext{}, err
	}
	if !found {
		return AuthorizedSupplierOfferContext{}, ErrIssuedRFQNotFound
	}
	if rfq.CompanyID != access.CompanyID ||
		rfq.ID != access.CurrentIssuedRFQVersionID ||
		strings.TrimSpace(rfq.RFQChainID) == "" {
		return AuthorizedSupplierOfferContext{}, ErrSupplierOfferAccessInvalid
	}
	return AuthorizedSupplierOfferContext{Access: access, RFQ: rfq}, nil
}

func validAuthorizedOfferAccess(
	access AuthorizedSupplierOfferAccess,
	presentedSessionToken string,
	requestedInvitationID string,
	accessedAt time.Time,
) bool {
	return strings.TrimSpace(access.SessionID) != "" &&
		strings.TrimSpace(access.CompanyID) != "" &&
		strings.TrimSpace(access.SupplierID) != "" &&
		strings.TrimSpace(access.RecipientIdentity) != "" &&
		access.InvitationID == strings.TrimSpace(requestedInvitationID) &&
		access.AccessGeneration > 0 &&
		strings.TrimSpace(access.CurrentIssuedRFQVersionID) != "" &&
		!accessedAt.IsZero() &&
		access.SessionCookieRenewal.ExpiresAt.After(accessedAt) &&
		subtle.ConstantTimeCompare(
			[]byte(access.SessionCookieRenewal.Token),
			[]byte(presentedSessionToken),
		) == 1
}

func WithSupplierOfferWithdrawalRepository(
	repository WithdrawalRepository,
) ServiceOption {
	return func(service *Service) { service.withdrawals = repository }
}

func WithSupplierOfferEligibilityRepository(
	repository SubmissionEligibilityRepository,
) ServiceOption {
	return func(service *Service) { service.eligibility = repository }
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of any of this module's public repository interfaces. Only the real
// Mongo repositories implement it; a fake used in unrelated tests simply does
// not satisfy this interface and is unaffected.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every SupplierOfferChain,
// SupplierOfferDraft, SupplierOfferVersion, SupplierOfferEligibility, and
// SupplierOfferWithdrawal owned by companyID, across all five of this
// module's collections. Development-tool use only (demoseed reset, design
// spec §6.6) — no production code path calls this. Idempotent: calling it
// when nothing remains for companyID is a no-op success, not an error.
func (service *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	if service.chains != nil {
		deleter, ok := service.chains.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("supplieroffers: chain repository %T does not support DeleteAllForCompany", service.chains)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.drafts != nil {
		deleter, ok := service.drafts.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("supplieroffers: draft repository %T does not support DeleteAllForCompany", service.drafts)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.versions != nil {
		deleter, ok := service.versions.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("supplieroffers: version repository %T does not support DeleteAllForCompany", service.versions)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.eligibility != nil {
		deleter, ok := service.eligibility.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("supplieroffers: eligibility repository %T does not support DeleteAllForCompany", service.eligibility)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.withdrawals != nil {
		deleter, ok := service.withdrawals.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("supplieroffers: withdrawal repository %T does not support DeleteAllForCompany", service.withdrawals)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	return nil
}
