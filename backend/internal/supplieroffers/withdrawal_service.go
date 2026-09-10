package supplieroffers

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

// Withdrawal (spec §7).
//
// Withdrawal never mutates the immutable Offer Version. It records a separate
// immutable withdrawal and moves the separate mutable eligibility gate, so the
// contractor's frozen view of what was offered stays exactly as submitted while
// the offer's availability changes.

// WithdrawOfferCommand withdraws one submitted Offer Version.
type WithdrawOfferCommand struct {
	Context        SupplierOfferMutationContextInput
	OfferVersionID string
	Reason         string
	// OperationID makes the whole withdrawal idempotent across retries.
	OperationID string
}

// WithdrawalRepository is the narrow immutable-withdrawal surface.
type WithdrawalRepository interface {
	InsertWithdrawal(ctx context.Context, withdrawal SupplierOfferWithdrawal) error
	FindWithdrawal(ctx context.Context, companyID, offerVersionID string) (
		SupplierOfferWithdrawal, bool, error)
}

// EligibilityClaimRepository is the narrow G5 claim surface withdrawal drives.
// Fresh acquisition is one atomic chain-plus-eligibility operation; terminal
// completion remains the existing recoverable single-document transition.
type EligibilityClaimRepository interface {
	ClaimLatestEligibilityForWithdrawal(ctx context.Context,
		input LatestWithdrawalClaimInput) (
		SupplierOfferEligibility, error)
	CompleteEligibilityClaim(ctx context.Context, input EligibilityCompletionInput) (
		SupplierOfferEligibility, error)
	FindEligibility(ctx context.Context, companyID, offerVersionID string) (
		SupplierOfferEligibility, bool, error)
}

// WithdrawOffer claims the eligibility gate, records the immutable withdrawal,
// then completes the claim.
//
// Claiming first is what serializes withdrawal against award finalisation: the
// gate CAS decides the winner before any withdrawal record exists, so an
// awarded offer can never acquire a withdrawal.
func (service *Service) WithdrawOffer(
	ctx context.Context,
	command WithdrawOfferCommand,
) (SupplierOfferWithdrawal, error) {
	if service.versions == nil || service.withdrawals == nil ||
		service.eligibility == nil {
		return SupplierOfferWithdrawal{}, ErrSupplierOffersNotConfigured
	}
	claims, ok := service.eligibility.(EligibilityClaimRepository)
	if !ok {
		return SupplierOfferWithdrawal{}, ErrSupplierOffersNotConfigured
	}

	reason := strings.TrimSpace(command.Reason)
	if reason == "" || len(reason) > MaxWithdrawalReasonLength ||
		procurementlimits.ValidateID(command.OfferVersionID) != nil ||
		procurementlimits.ValidateID(command.OperationID) != nil {
		return SupplierOfferWithdrawal{}, ErrInvalidOfferEligibility
	}

	authorized, err := service.ResolveMutationContext(ctx, command.Context)
	if err != nil {
		return SupplierOfferWithdrawal{}, err
	}

	// Company scoping makes a missing and a foreign version indistinguishable.
	version, found, err := service.versions.FindVersion(
		ctx, authorized.Access.CompanyID, command.OfferVersionID)
	if err != nil {
		return SupplierOfferWithdrawal{}, err
	}
	if !found {
		return SupplierOfferWithdrawal{}, ErrOfferVersionNotFound
	}
	// Only the recipient who submitted an offer may withdraw it.
	if version.RecipientIdentity != authorized.Access.RecipientIdentity ||
		version.InvitationID != authorized.Access.InvitationID {
		return SupplierOfferWithdrawal{}, ErrOfferVersionNotFound
	}

	gate, found, err := claims.FindEligibility(
		ctx, authorized.Access.CompanyID, version.ID)
	if err != nil {
		return SupplierOfferWithdrawal{}, err
	}
	if !found {
		return SupplierOfferWithdrawal{}, ErrInvalidOfferEligibility
	}

	// Same-operation recovery runs BEFORE a new latest-version transaction. A
	// durable V1 claim may legitimately be followed by V2, so rechecking latest
	// here would reject the operation that already won while V1 was current.
	if gate.OperationID != "" {
		if gate.OperationID != command.OperationID ||
			gate.ClaimType != EligibilityClaimWithdrawal ||
			gate.WithdrawalReason != reason {
			return SupplierOfferWithdrawal{}, ErrOfferEligibilityConflict
		}
		if gate.State == EligibilityWithdrawn {
			existing, exists, findErr := service.withdrawals.FindWithdrawal(
				ctx, authorized.Access.CompanyID, version.ID)
			if findErr != nil {
				return SupplierOfferWithdrawal{}, findErr
			}
			if exists {
				service.ensureWithdrawalAudit(ctx, authorized, existing)
				return existing, nil
			}
		}
		return service.completeWithdrawalFromClaim(ctx, authorized, version, gate, claims)
	}

	// Every callback-retry input is generated once here. MongoDB may execute the
	// transaction function repeatedly after transient errors, but every attempt
	// represents this exact same intended claim.
	claimed, err := claims.ClaimLatestEligibilityForWithdrawal(ctx,
		LatestWithdrawalClaimInput{
			CompanyID:        authorized.Access.CompanyID,
			OfferChainID:     version.OfferChainID,
			OfferVersionID:   version.ID,
			OperationID:      command.OperationID,
			ClaimID:          bson.NewObjectID().Hex(),
			WithdrawalReason: reason,
			ClaimedAt:        command.Context.AccessedAt,
		})
	if err != nil {
		return SupplierOfferWithdrawal{}, err
	}
	return service.completeWithdrawalFromClaim(ctx, authorized, version, claimed, claims)
}

// completeWithdrawalFromClaim contains no transaction. It is deliberately
// idempotent so a crash after durable claim acquisition always completes
// forward using the fields frozen on that claim.
func (service *Service) completeWithdrawalFromClaim(
	ctx context.Context,
	authorized AuthorizedSupplierOfferContext,
	version SupplierOfferVersion,
	claimed SupplierOfferEligibility,
	claims EligibilityClaimRepository,
) (SupplierOfferWithdrawal, error) {
	if claimed.ClaimedAt == nil {
		return SupplierOfferWithdrawal{}, ErrInvalidOfferEligibility
	}
	claimedAt := *claimed.ClaimedAt

	withdrawal := SupplierOfferWithdrawal{
		ID:                     claimed.ClaimID,
		CompanyID:              authorized.Access.CompanyID,
		OfferChainID:           version.OfferChainID,
		SupplierOfferVersionID: version.ID,
		InvitationID:           version.InvitationID,
		RecipientIdentity:      version.RecipientIdentity,
		OperationID:            claimed.OperationID,
		Reason:                 claimed.WithdrawalReason,
		WithdrawnAt:            claimedAt,
		SchemaVersion:          SupplierOfferWithdrawalSchemaVersion,
	}
	if insertErr := service.withdrawals.InsertWithdrawal(ctx, withdrawal); insertErr != nil {
		// A duplicate means a concurrent retry already recorded it; adopt that
		// record rather than failing a withdrawal that actually happened.
		existing, found, findErr := service.withdrawals.FindWithdrawal(
			ctx, authorized.Access.CompanyID, version.ID)
		if findErr != nil || !found {
			return SupplierOfferWithdrawal{}, insertErr
		}
		withdrawal = existing
	}

	if _, completeErr := claims.CompleteEligibilityClaim(ctx,
		EligibilityCompletionInput{
			CompanyID:        authorized.Access.CompanyID,
			OfferVersionID:   version.ID,
			ExpectedRevision: claimed.Revision,
			ClaimType:        EligibilityClaimWithdrawal,
			OperationID:      claimed.OperationID,
			CompletedAt:      claimedAt,
		}); completeErr != nil && !errors.Is(completeErr, ErrOfferEligibilityConflict) {
		return SupplierOfferWithdrawal{}, completeErr
	}

	service.ensureWithdrawalAudit(ctx, authorized, withdrawal)

	return withdrawal, nil
}

func (service *Service) ensureWithdrawalAudit(ctx context.Context,
	authorized AuthorizedSupplierOfferContext, withdrawal SupplierOfferWithdrawal) {
	if service.audit == nil {
		return
	}
	_ = service.audit.RecordOfferWithdrawn(ctx,
		withdrawal.CompanyID, authorized.Access.SupplierID,
		withdrawal.InvitationID, withdrawal.OfferChainID,
		withdrawal.SupplierOfferVersionID, withdrawal.ID, withdrawal.WithdrawnAt)
}
