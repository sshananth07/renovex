package supplieroffers

import (
	"context"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

type SupplierOfferReconciliationKind string

const (
	ReconciliationCompletedSubmission SupplierOfferReconciliationKind = "submission_completed"
	ReconciliationCompletedWithdrawal SupplierOfferReconciliationKind = "withdrawal_completed"
)

type ReconcileSupplierOfferInput struct {
	CompanyID          string
	ActorUserID        string
	InvitationID       string
	IssuedRFQVersionID string
	OperationID        string
}

type SupplierOfferReconciliationResult struct {
	Kind           SupplierOfferReconciliationKind
	OfferVersionID string
	WithdrawalID   string
}

type reconciliationChainRepository interface {
	FindChainByScope(context.Context, string, string, string) (SupplierOfferChain, bool, error)
}

type reconciliationDraftRepository interface {
	SubmissionRepository
	FindSubmissionByOperation(context.Context, string, string, string) (SupplierOfferDraft, bool, error)
	FindLiveSubmissionForChain(context.Context, string, string) (SupplierOfferDraft, bool, error)
}

type reconciliationEligibilityRepository interface {
	EligibilityClaimRepository
	SubmissionEligibilityRepository
	FindEligibilityByOperation(context.Context, string, string, string) (SupplierOfferEligibility, bool, error)
	FindLiveClaimForChain(context.Context, string, string) (SupplierOfferEligibility, bool, error)
}

// ReconcileSupplierOffer resolves the externally discoverable tenant tuple,
// then resumes only the exact durable operation recorded inside that chain.
// It never accepts a client-supplied chain or Company identity.
func (service *Service) ReconcileSupplierOffer(ctx context.Context,
	input ReconcileSupplierOfferInput) (SupplierOfferReconciliationResult, error) {
	if strings.TrimSpace(input.CompanyID) == "" ||
		procurementlimits.ValidateID(input.InvitationID) != nil ||
		procurementlimits.ValidateID(input.IssuedRFQVersionID) != nil ||
		procurementlimits.ValidateID(input.OperationID) != nil {
		return SupplierOfferReconciliationResult{}, ErrInputLimitExceeded
	}
	chains, chainOK := service.chains.(reconciliationChainRepository)
	drafts, draftOK := service.drafts.(reconciliationDraftRepository)
	eligibility, eligibilityOK := service.eligibility.(reconciliationEligibilityRepository)
	versions, versionOK := service.versions.(SubmissionVersionRepository)
	if !chainOK || !draftOK || !eligibilityOK || !versionOK ||
		service.issuedRFQ == nil {
		return SupplierOfferReconciliationResult{}, ErrSupplierOffersNotConfigured
	}
	chain, found, err := chains.FindChainByScope(ctx, input.CompanyID,
		input.InvitationID, input.IssuedRFQVersionID)
	if err != nil {
		return SupplierOfferReconciliationResult{}, err
	}
	if !found {
		return SupplierOfferReconciliationResult{}, ErrOfferChainNotFound
	}

	if draft, found, err := drafts.FindSubmissionByOperation(ctx, input.CompanyID,
		chain.ID, input.OperationID); err != nil {
		return SupplierOfferReconciliationResult{}, err
	} else if found {
		rfq, rfqFound, loadErr := service.issuedRFQ.GetIssuedRFQForOffer(
			ctx, input.CompanyID, chain.IssuedRFQVersionID)
		if loadErr != nil {
			return SupplierOfferReconciliationResult{}, loadErr
		}
		if !rfqFound {
			return SupplierOfferReconciliationResult{}, ErrIssuedRFQNotFound
		}
		authorized := AuthorizedSupplierOfferContext{RFQ: rfq,
			Access: AuthorizedSupplierOfferAccess{CompanyID: input.CompanyID,
				InvitationID:      chain.InvitationID,
				RecipientIdentity: draft.SubmissionRecipientIdentity,
				AccessGeneration:  draft.SubmissionAccessGeneration}}
		version, completeErr := service.completeSubmission(ctx, draft, authorized,
			drafts, service.chains.(SubmissionChainRepository), versions, eligibility)
		if completeErr != nil {
			return SupplierOfferReconciliationResult{}, completeErr
		}
		if service.audit != nil {
			_ = service.audit.RecordOfferSubmissionReconciled(ctx, version.CompanyID,
				"", version.InvitationID, version.OfferChainID, version.ID,
				version.VersionNumber, time.Now().UTC())
		}
		return SupplierOfferReconciliationResult{Kind: ReconciliationCompletedSubmission,
			OfferVersionID: version.ID}, nil
	}

	if gate, found, err := eligibility.FindEligibilityByOperation(ctx, input.CompanyID,
		chain.ID, input.OperationID); err != nil {
		return SupplierOfferReconciliationResult{}, err
	} else if found {
		if service.withdrawals == nil {
			return SupplierOfferReconciliationResult{}, ErrSupplierOffersNotConfigured
		}
		if gate.ClaimType != EligibilityClaimWithdrawal {
			return SupplierOfferReconciliationResult{}, ErrOfferEligibilityConflict
		}
		version, versionFound, findErr := versions.FindVersion(ctx, input.CompanyID,
			gate.OfferVersionID)
		if findErr != nil {
			return SupplierOfferReconciliationResult{}, findErr
		}
		if !versionFound || version.OfferChainID != chain.ID {
			return SupplierOfferReconciliationResult{}, ErrOfferVersionNotFound
		}
		authorized := AuthorizedSupplierOfferContext{Access: AuthorizedSupplierOfferAccess{
			CompanyID: input.CompanyID, InvitationID: chain.InvitationID,
			RecipientIdentity: version.RecipientIdentity,
		}}
		if gate.State == EligibilityWithdrawn {
			if existing, exists, findErr := service.withdrawals.FindWithdrawal(ctx,
				input.CompanyID, version.ID); findErr != nil {
				return SupplierOfferReconciliationResult{}, findErr
			} else if exists {
				service.ensureWithdrawalAudit(ctx, authorized, existing)
				return SupplierOfferReconciliationResult{Kind: ReconciliationCompletedWithdrawal,
					OfferVersionID: version.ID, WithdrawalID: existing.ID}, nil
			}
		}
		withdrawal, completeErr := service.completeWithdrawalFromClaim(ctx,
			authorized, version, gate, eligibility)
		if completeErr != nil {
			return SupplierOfferReconciliationResult{}, completeErr
		}
		return SupplierOfferReconciliationResult{Kind: ReconciliationCompletedWithdrawal,
			OfferVersionID: version.ID, WithdrawalID: withdrawal.ID}, nil
	}

	if _, live, err := drafts.FindLiveSubmissionForChain(ctx, input.CompanyID,
		chain.ID); err != nil {
		return SupplierOfferReconciliationResult{}, err
	} else if live {
		return SupplierOfferReconciliationResult{}, ErrOfferDraftConflict
	}
	if _, live, err := eligibility.FindLiveClaimForChain(ctx, input.CompanyID,
		chain.ID); err != nil {
		return SupplierOfferReconciliationResult{}, err
	} else if live {
		return SupplierOfferReconciliationResult{}, ErrOfferEligibilityConflict
	}
	return SupplierOfferReconciliationResult{}, ErrOfferVersionNotFound
}
