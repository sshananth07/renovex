package supplieroffers

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// SupplierOfferReadAuthorization carries browser credentials but no claimed
// tenant or Supplier identity. Phase D derives those from the bound session.
type SupplierOfferReadAuthorization struct {
	SessionToken string
	InvitationID string
	AccessedAt   time.Time
}

// SupplierOfferMutationAuthorization adds the reusable Phase D CSRF proof.
type SupplierOfferMutationAuthorization struct {
	SupplierOfferReadAuthorization
	CSRFCookie string
	CSRFHeader string
}

type SupplierSessionCookieRenewal struct {
	Token     string
	ExpiresAt time.Time
}

// AuthorizedSupplierOfferAccess is the only session/invitation identity this
// module trusts. Raw CSRF values have no representation in the result.
type AuthorizedSupplierOfferAccess struct {
	SessionID                 string
	CompanyID                 string
	SupplierID                string
	RecipientIdentity         string
	InvitationID              string
	AccessGeneration          int64
	CurrentIssuedRFQVersionID string
	SessionCookieRenewal      SupplierSessionCookieRenewal
}

// SupplierOfferAccessAuthorizer is implemented by a composition adapter over
// supplieraccess.Service. supplieroffers never reads session or binding data.
type SupplierOfferAccessAuthorizer interface {
	AuthorizeSupplierOfferRead(
		context.Context,
		SupplierOfferReadAuthorization,
	) (AuthorizedSupplierOfferAccess, error)
	AuthorizeSupplierOfferMutation(
		context.Context,
		SupplierOfferMutationAuthorization,
	) (AuthorizedSupplierOfferAccess, error)
}

// IssuedRFQLineSnapshot is the exact authoritative commercial line input that
// drafting and copy-forward require. Contractor costs and internal notes cannot
// cross this structural allowlist.
type IssuedRFQLineSnapshot struct {
	ID                          string
	LineageID                   string
	SourceMaterialRequirementID *string
	MaterialID                  string
	MaterialName                string
	Specification               string
	Quantity                    quantity.Quantity
	RequiredByDate              *time.Time
	ProcurementNotes            string
	SortOrder                   int
}

type IssuedRFQSnapshot struct {
	ID                   string
	CompanyID            string
	RFQChainID           string
	RFQNumber            string
	VersionNumber        int
	Currency             string
	Title                string
	DeliveryAddress      string
	RequiredByDate       *time.Time
	ResponseDeadline     time.Time
	SupplierInstructions string
	Lines                []IssuedRFQLineSnapshot
}

// IssuedRFQSource is implemented by a composition adapter over
// rfqissuance.Service. The authenticated Company is always part of the lookup.
type IssuedRFQSource interface {
	GetIssuedRFQForOffer(
		ctx context.Context,
		companyID string,
		issuedRFQVersionID string,
	) (IssuedRFQSnapshot, bool, error)
}

// SupplierOfferAuditRecorder is primitive-only. Commercial text, prices,
// credentials, CSRF values and full domain aggregates cannot cross it.
type SupplierOfferAuditRecorder interface {
	RecordOfferDraftCreated(
		ctx context.Context,
		companyID, supplierID, invitationID, offerChainID, draftID string,
		revision int64,
		occurredAt time.Time,
	) error
	RecordOfferDraftCopied(
		ctx context.Context,
		companyID, supplierID, invitationID, offerChainID, draftID,
		sourceOfferVersionID string,
		revision int64,
		occurredAt time.Time,
	) error
	RecordOfferDraftUpdated(
		ctx context.Context,
		companyID, supplierID, invitationID, offerChainID, draftID string,
		revision int64,
		occurredAt time.Time,
	) error
	RecordOfferDraftArchived(
		ctx context.Context,
		companyID, supplierID, invitationID, offerChainID, draftID string,
		revision int64,
		occurredAt time.Time,
	) error
	RecordOfferSubmitted(
		ctx context.Context,
		companyID, supplierID, invitationID, offerChainID, draftID,
		offerVersionID string,
		versionNumber int,
		occurredAt time.Time,
	) error
	RecordOfferSubmissionReconciled(
		ctx context.Context,
		companyID, supplierID, invitationID, offerChainID, offerVersionID string,
		versionNumber int,
		occurredAt time.Time,
	) error
	RecordOfferWithdrawn(
		ctx context.Context,
		companyID, supplierID, invitationID, offerChainID, offerVersionID,
		withdrawalID string,
		occurredAt time.Time,
	) error
}
