package composition

import (
	"context"
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// Phase E composition adapters.
//
// supplieroffers declares what it needs; these adapters satisfy those
// capabilities from supplieraccess and rfqissuance. Neither producer module
// knows supplieroffers exists, and no domain module imports another.

// --- supplieraccess -> supplieroffers.SupplierOfferAccessAuthorizer ---

// supplierProtectedAccessSource is the narrow slice of supplieraccess.Service
// this adapter needs.
type supplierProtectedAccessSource interface {
	AuthorizeInvitationAccess(
		context.Context, supplieraccess.AuthorizeInvitationAccessInput) (
		supplieraccess.AuthorizedInvitationAccess, error)
	AuthorizeInvitationMutation(
		context.Context, supplieraccess.AuthorizeInvitationMutationInput) (
		supplieraccess.AuthorizedInvitationAccess, error)
}

// SupplierOfferAccessAdapter satisfies supplieroffers' authorization
// capability from Phase D's protected-access service.
//
// Read and mutation stay SEPARATE all the way through: adding a read route can
// therefore never accidentally weaken the CSRF requirement that protects
// state-changing routes.
type SupplierOfferAccessAdapter struct {
	access supplierProtectedAccessSource
}

// SupplierRFQAccessAdapter satisfies rfqissuance's read-only authorization
// capability from the same Phase D protected-access service. The consumer owns
// the projection and rechecks the mutable invitation after this call.
type SupplierRFQAccessAdapter struct {
	access supplierProtectedAccessSource
}

// NewSupplierRFQAccessAdapter wraps a supplieraccess.Service without exposing
// that concrete module across the rfqissuance boundary.
func NewSupplierRFQAccessAdapter(source supplierProtectedAccessSource) *SupplierRFQAccessAdapter {
	return &SupplierRFQAccessAdapter{access: source}
}

// AuthorizeSupplierRFQRead carries only the authoritative tenant, Supplier,
// invitation, and generation facts needed for the owner module's second check.
func (a *SupplierRFQAccessAdapter) AuthorizeSupplierRFQRead(
	ctx context.Context,
	input rfqissuance.SupplierRFQReadAuthorization,
) (rfqissuance.AuthorizedSupplierRFQAccess, error) {
	authorized, err := a.access.AuthorizeInvitationAccess(ctx,
		supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: input.SessionToken, InvitationID: input.InvitationID,
			AccessedAt: input.AccessedAt,
		})
	if err != nil {
		switch {
		case errors.Is(err, supplieraccess.ErrInvalidSupplierCredential),
			errors.Is(err, supplieraccess.ErrInvitationAccessInvalid),
			errors.Is(err, supplieraccess.ErrSupplierSessionNotFound):
			return rfqissuance.AuthorizedSupplierRFQAccess{},
				rfqissuance.ErrSupplierRFQAccessInvalid
		default:
			return rfqissuance.AuthorizedSupplierRFQAccess{}, err
		}
	}
	return rfqissuance.AuthorizedSupplierRFQAccess{
		CompanyID: authorized.CompanyID, SupplierID: authorized.SupplierID,
		InvitationID:     authorized.InvitationID,
		AccessGeneration: authorized.AccessGeneration,
	}, nil
}

// NewSupplierOfferAccessAdapter wraps a supplieraccess.Service.
func NewSupplierOfferAccessAdapter(
	source supplierProtectedAccessSource,
) *SupplierOfferAccessAdapter {
	return &SupplierOfferAccessAdapter{access: source}
}

// AuthorizeSupplierOfferRead validates session, binding and invitation.
func (a *SupplierOfferAccessAdapter) AuthorizeSupplierOfferRead(
	ctx context.Context,
	input supplieroffers.SupplierOfferReadAuthorization,
) (supplieroffers.AuthorizedSupplierOfferAccess, error) {

	authorized, err := a.access.AuthorizeInvitationAccess(ctx,
		supplieraccess.AuthorizeInvitationAccessInput{
			SessionToken: input.SessionToken,
			InvitationID: input.InvitationID,
			AccessedAt:   input.AccessedAt,
		})
	if err != nil {
		return supplieroffers.AuthorizedSupplierOfferAccess{},
			mapSupplierOfferAccessError(err)
	}
	return toAuthorizedOfferAccess(authorized), nil
}

// AuthorizeSupplierOfferMutation adds the CSRF layer.
func (a *SupplierOfferAccessAdapter) AuthorizeSupplierOfferMutation(
	ctx context.Context,
	input supplieroffers.SupplierOfferMutationAuthorization,
) (supplieroffers.AuthorizedSupplierOfferAccess, error) {

	authorized, err := a.access.AuthorizeInvitationMutation(ctx,
		supplieraccess.AuthorizeInvitationMutationInput{
			AuthorizeInvitationAccessInput: supplieraccess.AuthorizeInvitationAccessInput{
				SessionToken: input.SessionToken,
				InvitationID: input.InvitationID,
				AccessedAt:   input.AccessedAt,
			},
			CSRFCookie: input.CSRFCookie,
			CSRFHeader: input.CSRFHeader,
		})
	if err != nil {
		return supplieroffers.AuthorizedSupplierOfferAccess{},
			mapSupplierOfferAccessError(err)
	}
	return toAuthorizedOfferAccess(authorized), nil
}

// toAuthorizedOfferAccess renames one field across the boundary: Phase D calls
// it NormalizedRecipientEmail, Phase E calls it RecipientIdentity. Everything
// supplieroffers trusts about Company, Supplier and recipient comes from here
// and never from request content.
func toAuthorizedOfferAccess(
	authorized supplieraccess.AuthorizedInvitationAccess,
) supplieroffers.AuthorizedSupplierOfferAccess {
	return supplieroffers.AuthorizedSupplierOfferAccess{
		SessionID:                 authorized.SessionID,
		CompanyID:                 authorized.CompanyID,
		SupplierID:                authorized.SupplierID,
		RecipientIdentity:         authorized.NormalizedRecipientEmail,
		InvitationID:              authorized.InvitationID,
		AccessGeneration:          authorized.AccessGeneration,
		CurrentIssuedRFQVersionID: authorized.CurrentIssuedRFQVersionID,
		SessionCookieRenewal: supplieroffers.SupplierSessionCookieRenewal{
			Token:     authorized.SessionCookieRenewal.Token,
			ExpiresAt: authorized.SessionCookieRenewal.ExpiresAt,
		},
	}
}

// mapSupplierOfferAccessError collapses every credential-rejection reason into
// the consumer's single invalid-access error. An infrastructure failure
// propagates unchanged so a real outage is never reported as a bad credential.
func mapSupplierOfferAccessError(err error) error {
	switch {
	// Phase D already collapses expiry, revocation and every other credential
	// rejection into these few sentinels; that non-disclosure is deliberate and
	// this adapter preserves it rather than re-deriving reasons.
	case errors.Is(err, supplieraccess.ErrSupplierCSRFRejected):
		return supplieroffers.ErrSupplierOfferCSRFRejected
	case errors.Is(err, supplieraccess.ErrInvalidSupplierCredential),
		errors.Is(err, supplieraccess.ErrInvitationAccessInvalid),
		errors.Is(err, supplieraccess.ErrSupplierSessionNotFound):
		return supplieroffers.ErrSupplierOfferAccessInvalid
	default:
		return err
	}
}

// --- rfqissuance -> supplieroffers.IssuedRFQSource ---

// issuedVersionSource is the narrow slice of rfqissuance.Service this adapter
// needs.
type issuedVersionSource interface {
	GetIssuedVersion(ctx context.Context, companyID, versionID string) (
		rfqissuance.IssuedRFQVersion, error)
}

// IssuedRFQOfferSourceAdapter projects the authoritative issued RFQ into the
// narrow Supplier-visible snapshot supplieroffers consumes.
//
// The projection is a structural allowlist: contractor costs, internal notes
// and any future contractor-facing field cannot reach a Supplier because no
// field here carries them.
type IssuedRFQOfferSourceAdapter struct {
	versions issuedVersionSource
}

// NewIssuedRFQOfferSourceAdapter wraps an rfqissuance.Service.
func NewIssuedRFQOfferSourceAdapter(
	source issuedVersionSource,
) *IssuedRFQOfferSourceAdapter {
	return &IssuedRFQOfferSourceAdapter{versions: source}
}

// GetIssuedRFQForOffer resolves the version a Supplier is quoting against.
//
// A missing or foreign version reports found = false rather than an error, so
// the Supplier sees a clean not-found; an infrastructure failure propagates
// unchanged rather than masquerading as an absent RFQ.
func (a *IssuedRFQOfferSourceAdapter) GetIssuedRFQForOffer(
	ctx context.Context,
	companyID string,
	issuedRFQVersionID string,
) (supplieroffers.IssuedRFQSnapshot, bool, error) {

	version, err := a.versions.GetIssuedVersion(ctx, companyID, issuedRFQVersionID)
	if err != nil {
		if errors.Is(err, rfqissuance.ErrIssuedVersionNotFound) {
			return supplieroffers.IssuedRFQSnapshot{}, false, nil
		}
		return supplieroffers.IssuedRFQSnapshot{}, false, err
	}

	lines := make([]supplieroffers.IssuedRFQLineSnapshot, 0, len(version.Lines))
	for _, line := range version.Lines {
		lines = append(lines, supplieroffers.IssuedRFQLineSnapshot{
			ID:                          line.ID,
			LineageID:                   line.LineageID,
			SourceMaterialRequirementID: line.SourceMaterialRequirementID,
			MaterialID:                  line.MaterialID,
			MaterialName:                line.MaterialName,
			Specification:               line.Specification,
			Quantity:                    line.Quantity,
			RequiredByDate:              line.RequiredByDate,
			ProcurementNotes:            line.ProcurementNotes,
			SortOrder:                   line.SortOrder,
		})
	}

	return supplieroffers.IssuedRFQSnapshot{
		ID:                   version.ID,
		CompanyID:            version.CompanyID,
		RFQChainID:           version.RFQChainID,
		RFQNumber:            version.RFQNumber,
		VersionNumber:        version.VersionNumber,
		Currency:             version.Currency,
		Title:                version.Title,
		DeliveryAddress:      version.DeliveryAddress,
		RequiredByDate:       version.RequiredByDate,
		ResponseDeadline:     version.ResponseDeadline,
		SupplierInstructions: version.SupplierInstructions,
		Lines:                lines,
	}, true, nil
}
