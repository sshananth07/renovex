package composition

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// Phase F composition adapters (M8 §8A.1).
//
// awards declares what it needs; these adapters satisfy those capabilities from
// rfqissuance and supplieroffers. Neither producer knows awards exists, and no
// domain module imports another — being adapters is exactly what permits these
// files to name two domain modules (ADR 0002).
//
// Adapters translate SHAPES, never policy. Phase F still decides which version
// is latest-eligible, how versions are labelled, who participates in outcomes,
// and how withdrawn, expired and superseded versions are treated.

// --- rfqissuance -> awards.IssuedRFQSource ---

// IssuedRFQAwardSourceAdapter resolves the issued version an award decides
// over.
type IssuedRFQAwardSourceAdapter struct {
	versions issuedVersionSource
}

func NewIssuedRFQAwardSourceAdapter(
	source issuedVersionSource,
) *IssuedRFQAwardSourceAdapter {
	return &IssuedRFQAwardSourceAdapter{versions: source}
}

// GetIssuedRFQForAward resolves the authoritative lines an award is made
// against.
//
// A missing or foreign version reports found = false rather than an error, so
// the caller returns a clean non-disclosing not-found; an infrastructure
// failure propagates unchanged rather than masquerading as an absent RFQ.
func (a *IssuedRFQAwardSourceAdapter) GetIssuedRFQForAward(
	ctx context.Context,
	companyID string,
	issuedRFQVersionID string,
) (awards.IssuedRFQSnapshot, bool, error) {

	version, err := a.versions.GetIssuedVersion(ctx, companyID, issuedRFQVersionID)
	if err != nil {
		if errors.Is(err, rfqissuance.ErrIssuedVersionNotFound) {
			return awards.IssuedRFQSnapshot{}, false, nil
		}
		return awards.IssuedRFQSnapshot{}, false, err
	}

	lines := make([]awards.IssuedRFQLineSnapshot, 0, len(version.Lines))
	for _, line := range version.Lines {
		lines = append(lines, awards.IssuedRFQLineSnapshot{
			ID: line.ID,
			// Load-bearing: F4's cross-version claim is keyed on this lineage,
			// so dropping it would break duplicate-award prevention (D4).
			LineageID:      line.LineageID,
			MaterialID:     line.MaterialID,
			MaterialName:   line.MaterialName,
			Specification:  line.Specification,
			Quantity:       line.Quantity,
			RequiredByDate: line.RequiredByDate,
			SortOrder:      line.SortOrder,
		})
	}

	return awards.IssuedRFQSnapshot{
		ID:               version.ID,
		CompanyID:        version.CompanyID,
		RFQChainID:       version.RFQChainID,
		RFQNumber:        version.RFQNumber,
		VersionNumber:    version.VersionNumber,
		Currency:         version.Currency,
		Title:            version.Title,
		ResponseDeadline: version.ResponseDeadline,
		Lines:            lines,
	}, true, nil
}

// --- supplieroffers + rfqissuance -> awards.OfferVersionSource ---

// offerVersionSource is the narrow slice of supplieroffers' version repository
// this adapter needs. It is a READ surface only.
type offerVersionSource interface {
	FindVersion(ctx context.Context, companyID, versionID string) (
		supplieroffers.SupplierOfferVersion, bool, error)
	ListVersionsForIssuedRFQVersion(
		ctx context.Context, companyID, issuedRFQVersionID string) (
		[]supplieroffers.SupplierOfferVersion, error)
}

// offerChainSource resolves which version is current for its invitation.
type offerChainSource interface {
	FindChain(ctx context.Context, companyID, chainID string) (
		supplieroffers.SupplierOfferChain, bool, error)
}

// offerEligibilitySource reads the Phase E gate without mutating it.
type offerEligibilitySource interface {
	FindEligibility(ctx context.Context, companyID, offerVersionID string) (
		supplieroffers.SupplierOfferEligibility, bool, error)
}

// invitationSupplierSource resolves the Supplier behind an invitation.
//
// The immutable Offer Version does not name its Supplier — only its Invitation
// — but D3 scopes outcomes to Supplier + Invitation, so the adapter joins the
// two here rather than widening either module's model.
//
// It yields the Supplier IDENTIFIER only. The Supplier's display name belongs
// to M7's suppliers module — resolved via supplierNameSource below, joined
// here rather than pulled through rfqissuance, which does not own that data.
type invitationSupplierSource interface {
	FindInvitation(ctx context.Context, companyID, invitationID string) (
		rfqissuance.SupplierInvitation, error)
}

// supplierNameSource resolves a Supplier's display name for presentation.
// Satisfied structurally by suppliers.Service.GetSupplier — this adapter is
// exactly the sanctioned place for that cross-module join (ADR 0002): awards
// itself never imports suppliers, and rfqissuance's invitation record only
// carries a Supplier ID, not a name.
type supplierNameSource interface {
	GetSupplier(ctx context.Context, companyID, supplierID string) (
		suppliers.Supplier, error)
}

type OfferVersionAwardSourceAdapter struct {
	versions      offerVersionSource
	chains        offerChainSource
	eligibility   offerEligibilitySource
	invitations   invitationSupplierSource
	supplierNames supplierNameSource
}

func NewOfferVersionAwardSourceAdapter(
	versions offerVersionSource,
	chains offerChainSource,
	eligibility offerEligibilitySource,
	invitations invitationSupplierSource,
	supplierNames supplierNameSource,
) *OfferVersionAwardSourceAdapter {
	return &OfferVersionAwardSourceAdapter{
		versions:      versions,
		chains:        chains,
		eligibility:   eligibility,
		invitations:   invitations,
		supplierNames: supplierNames,
	}
}

func (a *OfferVersionAwardSourceAdapter) GetOfferVersionForAward(
	ctx context.Context,
	companyID string,
	offerVersionID string,
) (awards.OfferVersionSnapshot, bool, error) {

	version, found, err := a.versions.FindVersion(ctx, companyID, offerVersionID)
	if err != nil || !found {
		return awards.OfferVersionSnapshot{}, false, err
	}
	snapshot, err := a.toSnapshot(ctx, companyID, version)
	if err != nil {
		return awards.OfferVersionSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (a *OfferVersionAwardSourceAdapter) ListOfferVersionsForIssuedRFQVersion(
	ctx context.Context,
	companyID string,
	issuedRFQVersionID string,
) ([]awards.OfferVersionSnapshot, error) {

	versions, err := a.versions.ListVersionsForIssuedRFQVersion(
		ctx, companyID, issuedRFQVersionID)
	if err != nil {
		return nil, err
	}

	snapshots := make([]awards.OfferVersionSnapshot, 0, len(versions))
	for _, version := range versions {
		snapshot, err := a.toSnapshot(ctx, companyID, version)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// toSnapshot joins the immutable version with the two mutable facts Phase F
// validates against: whether it is still the current submitted version for its
// invitation, and its eligibility gate.
//
// It reports those facts and stops. It does not decide whether the version is
// awardable — that is F3's job, and a correction deliberately ignores both
// facts for its locked baseline (§8G).
func (a *OfferVersionAwardSourceAdapter) toSnapshot(
	ctx context.Context,
	companyID string,
	version supplieroffers.SupplierOfferVersion,
) (awards.OfferVersionSnapshot, error) {

	// Supplier identity lives on the invitation, not the offer version.
	supplierID := ""
	if version.InvitationID != "" {
		invitation, err := a.invitations.FindInvitation(
			ctx, companyID, version.InvitationID)
		if err != nil {
			// A missing invitation leaves the Supplier unresolved rather than
			// failing the whole comparison: the offer is still a real quote,
			// and Phase F treats an unidentifiable Supplier as unselectable.
			if !errors.Is(err, rfqissuance.ErrInvitationNotFound) {
				return awards.OfferVersionSnapshot{}, err
			}
		} else {
			supplierID = invitation.SupplierID
		}
	}

	// The display name is presentation data, not part of the awardability
	// decision above — a missing directory entry leaves it blank rather than
	// failing the whole comparison, the same tolerance already applied to a
	// missing invitation.
	supplierName := ""
	if supplierID != "" {
		supplier, err := a.supplierNames.GetSupplier(ctx, companyID, supplierID)
		if err != nil {
			if !errors.Is(err, suppliers.ErrSupplierNotFound) {
				return awards.OfferVersionSnapshot{}, err
			}
		} else {
			supplierName = supplier.Name
		}
	}

	isLatest := false
	if chain, found, err := a.chains.FindChain(
		ctx, companyID, version.OfferChainID); err != nil {
		return awards.OfferVersionSnapshot{}, err
	} else if found && chain.LatestSubmittedID != nil {
		isLatest = *chain.LatestSubmittedID == version.ID
	}

	// An absent gate is treated as NOT eligible: a version whose serialization
	// state cannot be read must never be awarded on the assumption it is free.
	gateState := awards.OfferEligibilityState("")
	var gateRevision int64
	if gate, found, err := a.eligibility.FindEligibility(
		ctx, companyID, version.ID); err != nil {
		return awards.OfferVersionSnapshot{}, err
	} else if found {
		gateState = awards.OfferEligibilityState(gate.State)
		gateRevision = gate.Revision
	}

	lines := make([]awards.OfferLineSnapshot, 0, len(version.Lines))
	for _, line := range version.Lines {
		lines = append(lines, awards.OfferLineSnapshot{
			ID:                       line.ID,
			RFQLineID:                line.RFQLineID,
			ResponseStatus:           awards.OfferLineResponse(line.ResponseStatus),
			QuotedQuantity:           line.QuotedQuantity,
			UnitPriceExcludingTax:    line.UnitPriceExcludingTax,
			LineSubtotalExcludingTax: line.LineSubtotalExcludingTax,
			LineTaxAmount:            line.LineTaxAmount,
			// The immutable per-line tax RULE, so F3 recalculates tax over the
			// awarded subset through the shared kernel rather than reusing a
			// figure computed for the whole submitted offer.
			LineTax:              line.LineTax,
			Brand:                line.Brand,
			SKU:                  line.SKU,
			ProductDescription:   line.ProductDescription,
			LeadTime:             line.LeadTime,
			SupplierLineNotes:    line.SupplierLineNotes,
			CommercialExceptions: line.CommercialExceptions,
		})
	}

	return awards.OfferVersionSnapshot{
		ID:                 version.ID,
		CompanyID:          version.CompanyID,
		OfferChainID:       version.OfferChainID,
		SupplierID:         supplierID,
		SupplierName:       supplierName,
		InvitationID:       version.InvitationID,
		IssuedRFQVersionID: version.IssuedRFQVersionID,
		VersionNumber:      version.VersionNumber,
		Currency:           version.Currency,
		Lines:              lines,
		// Rules, not amounts: F3 re-evaluates all three against the awarded
		// subset (§8D).
		Tax:                 version.Tax,
		DeliveryCharge:      version.DeliveryCharge,
		ChargeGroups:        version.ChargeGroups,
		OfferLevelTaxAmount: offerLevelTaxAmount(version),
		DeliveryChargeTotal: version.DeliveryChargeTotal,
		QuotedLineSubtotal:  version.QuotedLineSubtotal,
		QuotedTaxTotal:      version.QuotedTaxTotal,
		GrandTotal:          version.GrandTotal,
		OfferValidUntil:     version.OfferValidUntil,
		SupplierNotes:       version.SupplierNotes,
		SubmittedAt:         version.SubmittedAt,
		IsLatestSubmitted:   isLatest,
		EligibilityState:    gateState,
		EligibilityRevision: gateRevision,
	}, nil
}

// offerLevelTaxAmount reads the whole-offer tax figure when the Supplier quoted
// one. It is carried for display and for the correction baseline, never as the
// authoritative award input.
func offerLevelTaxAmount(
	version supplieroffers.SupplierOfferVersion,
) money.Money {
	if version.Tax.Mode == supplieroffers.TaxModeOfferLevel &&
		version.Tax.OfferLevel != nil {
		return version.Tax.OfferLevel.TaxAmount
	}
	return money.New(0, version.Currency)
}

// --- supplieroffers -> awards.OfferEligibilityClaimant ---

// offerEligibilityClaimSource is the narrow claim surface. Claim state,
// operation ID and claim ID all stay enforced inside supplieroffers; this
// adapter only translates shapes.
type offerEligibilityClaimSource interface {
	offerEligibilitySource
	ClaimEligibility(ctx context.Context, input supplieroffers.EligibilityClaimInput) (
		supplieroffers.SupplierOfferEligibility, error)
	CompleteEligibilityClaim(
		ctx context.Context, input supplieroffers.EligibilityCompletionInput) (
		supplieroffers.SupplierOfferEligibility, error)
	ReleaseAwardClaim(
		ctx context.Context, input supplieroffers.EligibilityReleaseInput) (
		supplieroffers.SupplierOfferEligibility, error)
}

// OfferEligibilityClaimantAdapter satisfies awards' claim capability.
//
// The no-Award-Revision-exists check does NOT live here and must not: this
// adapter has no award knowledge, and neither does supplieroffers. That
// verification belongs to the awards finalisation service, immediately before
// it calls release (§8E).
type OfferEligibilityClaimantAdapter struct {
	eligibility offerEligibilityClaimSource
}

func NewOfferEligibilityClaimantAdapter(
	source offerEligibilityClaimSource,
) *OfferEligibilityClaimantAdapter {
	return &OfferEligibilityClaimantAdapter{eligibility: source}
}

func (a *OfferEligibilityClaimantAdapter) ClaimOfferForAward(
	ctx context.Context,
	request awards.OfferEligibilityClaimRequest,
) (awards.OfferEligibilitySnapshot, error) {

	claimed, err := a.eligibility.ClaimEligibility(ctx,
		supplieroffers.EligibilityClaimInput{
			CompanyID:      request.CompanyID,
			OfferVersionID: request.OfferVersionID,
			// Always an AWARD claim: a withdrawal claim is the Supplier's
			// decision and can never be made on their behalf from here.
			ClaimType:        supplieroffers.EligibilityClaimAward,
			OperationID:      request.OperationID,
			ClaimID:          request.ClaimID,
			ExpectedRevision: request.ExpectedRevision,
			ClaimedAt:        request.ClaimedAt,
		})
	if err != nil {
		return awards.OfferEligibilitySnapshot{}, mapEligibilityError(err)
	}
	return toEligibilitySnapshot(claimed), nil
}

func (a *OfferEligibilityClaimantAdapter) CompleteOfferAward(
	ctx context.Context,
	request awards.OfferEligibilityCompletionRequest,
) (awards.OfferEligibilitySnapshot, error) {

	completed, err := a.eligibility.CompleteEligibilityClaim(ctx,
		supplieroffers.EligibilityCompletionInput{
			CompanyID:        request.CompanyID,
			OfferVersionID:   request.OfferVersionID,
			ClaimType:        supplieroffers.EligibilityClaimAward,
			OperationID:      request.OperationID,
			ExpectedRevision: request.ExpectedRevision,
			CompletedAt:      request.CompletedAt,
		})
	if err != nil {
		return awards.OfferEligibilitySnapshot{}, mapEligibilityError(err)
	}
	return toEligibilitySnapshot(completed), nil
}

func (a *OfferEligibilityClaimantAdapter) ReleaseOfferAwardClaim(
	ctx context.Context,
	request awards.OfferEligibilityReleaseRequest,
) (awards.OfferEligibilitySnapshot, error) {

	released, err := a.eligibility.ReleaseAwardClaim(ctx,
		supplieroffers.EligibilityReleaseInput{
			CompanyID:        request.CompanyID,
			OfferVersionID:   request.OfferVersionID,
			OperationID:      request.OperationID,
			ClaimID:          request.ClaimID,
			ExpectedRevision: request.ExpectedRevision,
		})
	if err != nil {
		return awards.OfferEligibilitySnapshot{}, mapEligibilityError(err)
	}
	return toEligibilitySnapshot(released), nil
}

func (a *OfferEligibilityClaimantAdapter) GetOfferEligibility(
	ctx context.Context,
	companyID string,
	offerVersionID string,
) (awards.OfferEligibilitySnapshot, bool, error) {

	gate, found, err := a.eligibility.FindEligibility(
		ctx, companyID, offerVersionID)
	if err != nil || !found {
		return awards.OfferEligibilitySnapshot{}, false, err
	}
	return toEligibilitySnapshot(gate), true, nil
}

func toEligibilitySnapshot(
	gate supplieroffers.SupplierOfferEligibility,
) awards.OfferEligibilitySnapshot {
	return awards.OfferEligibilitySnapshot{
		OfferVersionID: gate.OfferVersionID,
		OfferChainID:   gate.OfferChainID,
		State:          awards.OfferEligibilityState(gate.State),
		OperationID:    gate.OperationID,
		ClaimID:        gate.ClaimID,
		Revision:       gate.Revision,
	}
}

// mapEligibilityError translates a Phase E refusal into the bounded Phase F
// sentinel, so the award handler maps it to the status §8D fixes rather than
// falling through to a generic 503.
func mapEligibilityError(err error) error {
	switch {
	case errors.Is(err, supplieroffers.ErrOfferEligibilityConflict):
		return awards.ErrOfferVersionNotEligible
	case errors.Is(err, supplieroffers.ErrInvalidOfferEligibility):
		return awards.ErrOfferVersionNotEligible
	default:
		return err
	}
}

// --- rfqissuance -> awards.InvitationLinkSource ---

// invitationLinkDeriver is the narrow rfqissuance surface this adapter needs.
type invitationLinkDeriver interface {
	DeriveInvitationLink(ctx context.Context, companyID, invitationID string) (
		rfqissuance.InvitationLink, error)
}

// InvitationLinkSourceAdapter lets awards ask for an outcome notification's
// Supplier Access URL without importing rfqissuance by type (ADR 0002).
//
// It is a thin passthrough to rfqissuance.Service.DeriveInvitationLink, which
// re-derives the invitation's EXISTING access secret with no rotation, no new
// invitation, no delivery-attempt record and no mutation — see that method's
// own doc comment for the full guarantee.
type InvitationLinkSourceAdapter struct {
	invitations invitationLinkDeriver
}

func NewInvitationLinkSourceAdapter(
	invitations invitationLinkDeriver,
) *InvitationLinkSourceAdapter {
	return &InvitationLinkSourceAdapter{invitations: invitations}
}

func (a *InvitationLinkSourceAdapter) DeriveInvitationLink(
	ctx context.Context,
	companyID string,
	invitationID string,
) (awards.InvitationLink, error) {
	link, err := a.invitations.DeriveInvitationLink(ctx, companyID, invitationID)
	if err != nil {
		return awards.InvitationLink{}, err
	}
	return awards.InvitationLink{Token: link.Token, URL: link.URL}, nil
}
