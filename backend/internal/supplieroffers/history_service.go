package supplieroffers

import (
	"context"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

type SupplierOfferHistoryInput struct {
	SupplierOfferReadContextInput
	PageSize int
	Cursor   int
}

type SupplierOfferVersionDetailInput struct {
	SupplierOfferReadContextInput
	OfferVersionID string
}

type SupplierOfferVersionSummary struct {
	ID                 string
	IssuedRFQVersionID string
	VersionNumber      int
	Currency           string
	SubmittedAt        time.Time
	OfferValidUntil    time.Time
	PublicStatus       OfferVersionStatus
	IsSuperseded       bool
	CanWithdraw        bool
	GrandTotal         money.Money
}

type SupplierOfferVersionProjection struct {
	Version      SupplierOfferVersion
	PublicStatus OfferVersionStatus
	IsSuperseded bool
	CanWithdraw  bool
	Withdrawal   *SupplierOfferWithdrawal
}

type SupplierOfferHistoryPage struct {
	Versions   []SupplierOfferVersionSummary
	NextCursor *int
}

type offerHistoryChainRepository interface {
	FindChainByScope(context.Context, string, string, string) (SupplierOfferChain, bool, error)
}

type offerHistoryVersionRepository interface {
	ListVersionsForChain(context.Context, string, string) ([]SupplierOfferVersion, error)
	FindVersion(context.Context, string, string) (SupplierOfferVersion, bool, error)
}

type offerHistoryEligibilityRepository interface {
	FindEligibility(context.Context, string, string) (SupplierOfferEligibility, bool, error)
}

func (service *Service) resolveOfferHistory(
	ctx context.Context,
	input SupplierOfferReadContextInput,
) (AuthorizedSupplierOfferContext, SupplierOfferChain, error) {
	authorized, err := service.ResolveReadContext(ctx, input)
	if err != nil {
		return AuthorizedSupplierOfferContext{}, SupplierOfferChain{}, err
	}
	chains, ok := service.chains.(offerHistoryChainRepository)
	if !ok {
		return AuthorizedSupplierOfferContext{}, SupplierOfferChain{}, ErrSupplierOffersNotConfigured
	}
	chain, found, err := chains.FindChainByScope(ctx, authorized.Access.CompanyID,
		authorized.Access.InvitationID, authorized.RFQ.ID)
	if err != nil {
		return AuthorizedSupplierOfferContext{}, SupplierOfferChain{}, err
	}
	if !found {
		return AuthorizedSupplierOfferContext{}, SupplierOfferChain{}, ErrOfferChainNotFound
	}
	return authorized, chain, nil
}

func (service *Service) projectOfferVersion(
	ctx context.Context,
	authorized AuthorizedSupplierOfferContext,
	chain SupplierOfferChain,
	version SupplierOfferVersion,
	evaluatedAt time.Time,
) (SupplierOfferVersionProjection, error) {
	eligibilities, ok := service.eligibility.(offerHistoryEligibilityRepository)
	if !ok {
		return SupplierOfferVersionProjection{}, ErrSupplierOffersNotConfigured
	}
	gate, found, err := eligibilities.FindEligibility(ctx, authorized.Access.CompanyID, version.ID)
	if err != nil {
		return SupplierOfferVersionProjection{}, err
	}
	if !found {
		return SupplierOfferVersionProjection{}, ErrOfferStatePending
	}
	latestID := ""
	if chain.LatestSubmittedID != nil {
		latestID = *chain.LatestSubmittedID
	}
	state := EvaluateOfferVersionState(OfferVersionStateFacts{
		OfferVersionID: version.ID, LatestSubmittedVersionID: latestID,
		EligibilityState: gate.State, EligibilityClaimType: gate.ClaimType,
		OfferValidUntil: version.OfferValidUntil, EvaluatedAt: evaluatedAt,
	})
	if state.Status == OfferVersionStatusPending {
		return SupplierOfferVersionProjection{}, ErrOfferStatePending
	}
	// Read access survives recipient replacement, but mutation authority does
	// not transfer to a recipient who did not submit the immutable version.
	canWithdraw := state.CanWithdraw &&
		version.RecipientIdentity == authorized.Access.RecipientIdentity
	projection := SupplierOfferVersionProjection{
		Version: version, PublicStatus: state.Status,
		IsSuperseded: state.IsSuperseded, CanWithdraw: canWithdraw,
	}
	if gate.State == EligibilityWithdrawn {
		withdrawal, found, err := service.withdrawals.FindWithdrawal(ctx,
			authorized.Access.CompanyID, version.ID)
		if err != nil {
			return SupplierOfferVersionProjection{}, err
		}
		if !found {
			return SupplierOfferVersionProjection{}, ErrOfferStatePending
		}
		projection.Withdrawal = &withdrawal
	}
	return projection, nil
}

func (service *Service) ListSupplierOfferVersions(
	ctx context.Context,
	input SupplierOfferHistoryInput,
) (SupplierOfferHistoryPage, error) {
	authorized, chain, err := service.resolveOfferHistory(ctx, input.SupplierOfferReadContextInput)
	if err != nil {
		return SupplierOfferHistoryPage{}, err
	}
	pageSize, err := procurementlimits.PageSize(input.PageSize)
	if err != nil || input.Cursor < 0 || input.Cursor > chain.LatestSubmittedVersion {
		return SupplierOfferHistoryPage{}, ErrInputLimitExceeded
	}
	versions, ok := service.versions.(offerHistoryVersionRepository)
	if !ok {
		return SupplierOfferHistoryPage{}, ErrSupplierOffersNotConfigured
	}
	listed, err := versions.ListVersionsForChain(ctx, authorized.Access.CompanyID, chain.ID)
	if err != nil {
		return SupplierOfferHistoryPage{}, err
	}
	page := SupplierOfferHistoryPage{Versions: make([]SupplierOfferVersionSummary, 0, pageSize)}
	eligible := make([]SupplierOfferVersion, 0, len(listed))
	for _, version := range listed {
		if input.Cursor > 0 && version.VersionNumber >= input.Cursor {
			continue
		}
		eligible = append(eligible, version)
	}
	for index, version := range eligible {
		if index == pageSize {
			cursor := page.Versions[len(page.Versions)-1].VersionNumber
			page.NextCursor = &cursor
			break
		}
		projection, err := service.projectOfferVersion(ctx, authorized, chain, version,
			input.AccessedAt)
		if err != nil {
			return SupplierOfferHistoryPage{}, err
		}
		page.Versions = append(page.Versions, SupplierOfferVersionSummary{
			ID: version.ID, IssuedRFQVersionID: version.IssuedRFQVersionID,
			VersionNumber: version.VersionNumber, Currency: version.Currency,
			SubmittedAt: version.SubmittedAt, OfferValidUntil: version.OfferValidUntil,
			PublicStatus: projection.PublicStatus, IsSuperseded: projection.IsSuperseded,
			CanWithdraw: projection.CanWithdraw, GrandTotal: version.GrandTotal,
		})
	}
	return page, nil
}

func (service *Service) GetSupplierOfferVersion(
	ctx context.Context,
	input SupplierOfferVersionDetailInput,
) (SupplierOfferVersionProjection, error) {
	if strings.TrimSpace(input.OfferVersionID) == "" {
		return SupplierOfferVersionProjection{}, ErrOfferVersionNotFound
	}
	authorized, chain, err := service.resolveOfferHistory(ctx, input.SupplierOfferReadContextInput)
	if err != nil {
		return SupplierOfferVersionProjection{}, err
	}
	versions, ok := service.versions.(offerHistoryVersionRepository)
	if !ok {
		return SupplierOfferVersionProjection{}, ErrSupplierOffersNotConfigured
	}
	version, found, err := versions.FindVersion(ctx, authorized.Access.CompanyID,
		input.OfferVersionID)
	if err != nil {
		return SupplierOfferVersionProjection{}, err
	}
	if !found || version.OfferChainID != chain.ID {
		return SupplierOfferVersionProjection{}, ErrOfferVersionNotFound
	}
	return service.projectOfferVersion(ctx, authorized, chain, version, input.AccessedAt)
}
