package awards

import (
	"context"
	"fmt"
	"time"
)

// Service is the awards module's application boundary.
//
// It is deliberately role-agnostic: route-level role authorization runs at the
// HTTP boundary before the call (identity.AuthorizedPrincipal, §1A.1). CompanyID
// is always supplied by the caller from the authenticated principal and is
// never read from request content, so a caller cannot widen its own tenancy.
//
// Capabilities are injected by option so each checkpoint wires only what it
// needs and a missing dependency fails closed rather than silently degrading.
type Service struct {
	issuedRFQ      IssuedRFQSource
	offers         OfferVersionSource
	eligibility    OfferEligibilityClaimant
	mailer         AwardNotificationMailer
	invitationLink InvitationLinkSource
	audit          AwardAuditRecorder

	chains     AwardChainRepository
	drafts     AwardDraftRepository
	lineClaims AwardLineClaimRepository
	revisions  AwardRevisionRepository
	outcomes   AwardOutcomeRepository
	deliveries AwardDeliveryRepository

	acknowledgements AwardAcknowledgementRepository
}

type ServiceOption func(*Service)

func NewService(options ...ServiceOption) *Service {
	service := &Service{}
	for _, option := range options {
		option(service)
	}
	return service
}

func WithIssuedRFQSource(source IssuedRFQSource) ServiceOption {
	return func(service *Service) { service.issuedRFQ = source }
}

func WithOfferVersionSource(source OfferVersionSource) ServiceOption {
	return func(service *Service) { service.offers = source }
}

func WithOfferEligibilityClaimant(claimant OfferEligibilityClaimant) ServiceOption {
	return func(service *Service) { service.eligibility = claimant }
}

func WithAwardNotificationMailer(mailer AwardNotificationMailer) ServiceOption {
	return func(service *Service) { service.mailer = mailer }
}

func WithInvitationLinkSource(source InvitationLinkSource) ServiceOption {
	return func(service *Service) { service.invitationLink = source }
}

func WithAwardAuditRecorder(recorder AwardAuditRecorder) ServiceOption {
	return func(service *Service) { service.audit = recorder }
}

// GetComparison projects every offer version answering an issued RFQ version.
//
// A missing issued version and another company's issued version are the same
// bounded not-found, so the response never confirms that an identifier exists
// in some other tenant.
func (service *Service) GetComparison(
	ctx context.Context,
	companyID string,
	issuedRFQVersionID string,
	observedAt time.Time,
) (Comparison, error) {
	if service.issuedRFQ == nil || service.offers == nil {
		return Comparison{}, ErrAwardsNotConfigured
	}

	issued, found, err := service.issuedRFQ.GetIssuedRFQForAward(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return Comparison{}, err
	}
	if !found {
		return Comparison{}, ErrIssuedRFQNotFound
	}

	// An infrastructure failure propagates rather than becoming an empty
	// comparison: "no Supplier responded" and "the read failed" must never look
	// alike to a contractor about to award.
	versions, err := service.offers.ListOfferVersionsForIssuedRFQVersion(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return Comparison{}, err
	}

	return BuildComparison(ComparisonInput{
		IssuedRFQ:     issued,
		OfferVersions: versions,
		ObservedAt:    observedAt,
	})
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of any of this module's public repository interfaces. Only the real
// Mongo repositories implement it; a fake used in unrelated tests simply does
// not satisfy this interface and is unaffected.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every AwardDecisionChain,
// AwardDraft, AwardLineClaim, AwardRevision, AwardOutcome,
// AwardOutcomeDelivery, and AwardOutcomeAcknowledgement owned by companyID,
// across all seven of this module's collections. Development-tool use only
// (demoseed reset, design spec §6.6) — no production code path calls this.
// Idempotent: calling it when nothing remains for companyID is a no-op
// success, not an error.
//
// Every field is nil-guarded rather than assumed set, matching this module's
// own established convention (every other Service method checks its
// dependencies before use and fails closed with ErrAwardsNotConfigured-style
// sentinels rather than silently skipping work).
func (service *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	if service.chains != nil {
		deleter, ok := service.chains.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("awards: chain repository %T does not support DeleteAllForCompany", service.chains)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.drafts != nil {
		deleter, ok := service.drafts.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("awards: draft repository %T does not support DeleteAllForCompany", service.drafts)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.lineClaims != nil {
		deleter, ok := service.lineClaims.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("awards: line claim repository %T does not support DeleteAllForCompany", service.lineClaims)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.revisions != nil {
		deleter, ok := service.revisions.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("awards: revision repository %T does not support DeleteAllForCompany", service.revisions)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.outcomes != nil {
		deleter, ok := service.outcomes.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("awards: outcome repository %T does not support DeleteAllForCompany", service.outcomes)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.deliveries != nil {
		deleter, ok := service.deliveries.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("awards: delivery repository %T does not support DeleteAllForCompany", service.deliveries)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	if service.acknowledgements != nil {
		deleter, ok := service.acknowledgements.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("awards: acknowledgement repository %T does not support DeleteAllForCompany", service.acknowledgements)
		}
		if err := deleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}
	return nil
}
