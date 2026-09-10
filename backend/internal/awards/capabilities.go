package awards

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// The five consumer-owned capabilities of §8A.1. Every cross-module need of
// this package is expressed here as an interface this package owns and another
// module satisfies through a composition adapter. Nothing in awards imports
// rfqissuance, supplieraccess or supplieroffers (ADR 0002), so these shapes are
// deliberately narrow: they carry the exact facts Phase F needs and no more.

// IssuedRFQLineSnapshot is the authoritative line Phase F awards against.
//
// LineageID is load-bearing rather than informational: F4's cross-version line
// claim is keyed on CompanyID + RFQChainID + StableLineageID, which is what
// makes "one Supplier per line, across every issued version" true (§8E, D4).
type IssuedRFQLineSnapshot struct {
	ID             string
	LineageID      string
	MaterialID     string
	MaterialName   string
	Specification  string
	Quantity       quantity.Quantity
	RequiredByDate *time.Time
	SortOrder      int
}

// IssuedRFQSnapshot is the immutable issued version an award decides over.
// RFQChainID is required because line claims span every issued version of the
// chain, not just this one.
type IssuedRFQSnapshot struct {
	ID               string
	CompanyID        string
	RFQChainID       string
	RFQNumber        string
	VersionNumber    int
	Currency         string
	Title            string
	ResponseDeadline time.Time
	Lines            []IssuedRFQLineSnapshot
}

// IssuedRFQSource is satisfied by a composition adapter over rfqissuance. The
// authenticated Company is always part of the lookup, so naming a foreign
// version ID cannot reach another tenant's RFQ.
type IssuedRFQSource interface {
	GetIssuedRFQForAward(
		ctx context.Context,
		companyID string,
		issuedRFQVersionID string,
	) (IssuedRFQSnapshot, bool, error)
}

// OfferLineResponse mirrors the Phase E response discriminator as a primitive.
// Only quoted lines are ever selectable; no_bid and unavailable are explicit
// declines shown in comparison but never awardable (§8B).
type OfferLineResponse string

const (
	OfferLineQuoted      OfferLineResponse = "quoted"
	OfferLineNoBid       OfferLineResponse = "no_bid"
	OfferLineUnavailable OfferLineResponse = "unavailable"
	OfferLineUnanswered  OfferLineResponse = "unanswered"
)

// The discriminated tax shapes come from the shared kernel so an awarded tax
// figure is produced by the same code that produced the quoted one.
// offer_level is the mode D1 constrains to all-or-nothing selection.
type (
	OfferTaxMode = procurementcalc.TaxMode
	OfferTaxRule = procurementcalc.SupplierOfferTax
	OfferLineTax = procurementcalc.QuotedLineTax
	DeliveryRule = procurementcalc.DeliveryCharge
)

const (
	OfferTaxNotApplicable = procurementcalc.TaxModeNotApplicable
	OfferTaxLineLevel     = procurementcalc.TaxModeLineLevel
	OfferTaxOfferLevel    = procurementcalc.TaxModeOfferLevel
)

// OfferLineSnapshot is one frozen line of a submitted Offer Version. Amounts
// are the Supplier's own calculated figures; F3 recalculates over the selected
// subset rather than trusting a submission-time total for a different line set.
type OfferLineSnapshot struct {
	ID                       string
	RFQLineID                string
	ResponseStatus           OfferLineResponse
	QuotedQuantity           *quantity.Quantity
	UnitPriceExcludingTax    *money.Money
	LineSubtotalExcludingTax *money.Money
	LineTaxAmount            money.Money
	// LineTax is the Supplier's immutable per-line tax RULE. F3 recalculates
	// tax over the awarded subset rather than reusing LineTaxAmount, which was
	// calculated for the whole submitted offer.
	LineTax *OfferLineTax

	Brand                string
	SKU                  string
	ProductDescription   string
	LeadTime             string
	SupplierLineNotes    string
	CommercialExceptions string
}

// OfferChargeGroupSnapshot carries the Supplier's complete immutable charge
// RULE, not its submission-time calculated amount.
//
// §8D requires F3 to re-evaluate each group against the SELECTED line set: a
// group's trigger and its percentage base both depend on which lines are
// actually awarded, so carrying forward the submitted total would attribute a
// charge to a line set that was never awarded. Re-evaluation needs the rule.
//
// It is the procurementcalc rule type itself rather than a parallel shape, so
// the awarded charge is produced by exactly the same arithmetic that produced
// the Supplier's quoted charge.
type OfferChargeGroupSnapshot = procurementcalc.ConditionalChargeGroup

// OfferVersionSnapshot is an immutable submitted Offer Version.
//
// SupplierID and InvitationID are carried because the immutable version alone
// does not name its Supplier, and D3 scopes outcomes to Supplier + Invitation.
// IsLatestSubmitted and EligibilityState are the mutable facts F3 validates
// against; a correction's LOCKED BASELINE deliberately ignores both (§8G).
type OfferVersionSnapshot struct {
	ID                 string
	CompanyID          string
	OfferChainID       string
	SupplierID         string
	SupplierName       string
	InvitationID       string
	IssuedRFQVersionID string
	VersionNumber      int
	Currency           string

	Lines []OfferLineSnapshot

	// Tax and DeliveryCharge are the Supplier's immutable RULES, which F3
	// re-runs through the shared kernel against the awarded subset. The
	// *Amount/*Total fields below are the Supplier's own submitted figures,
	// carried for display and for the correction baseline — never as the
	// authoritative award input.
	Tax            OfferTaxRule
	DeliveryCharge *DeliveryRule
	ChargeGroups   []OfferChargeGroupSnapshot

	OfferLevelTaxAmount money.Money
	DeliveryChargeTotal money.Money

	QuotedLineSubtotal money.Money
	QuotedTaxTotal     money.Money
	GrandTotal         money.Money

	OfferValidUntil     time.Time
	SupplierNotes       string
	SubmittedAt         time.Time
	IsLatestSubmitted   bool
	EligibilityState    OfferEligibilityState
	EligibilityRevision int64
}

// OfferVersionSource is satisfied by a composition adapter over supplieroffers.
// The list form is what F1's comparison projection reads; awards never queries
// another module's collection.
type OfferVersionSource interface {
	GetOfferVersionForAward(
		ctx context.Context,
		companyID string,
		offerVersionID string,
	) (OfferVersionSnapshot, bool, error)
	ListOfferVersionsForIssuedRFQVersion(
		ctx context.Context,
		companyID string,
		issuedRFQVersionID string,
	) ([]OfferVersionSnapshot, error)
}

// OfferEligibilityState mirrors the Phase E gate as a primitive. This is the
// per-Offer-Version serialization point, distinct from the per-lineage award
// line claim awards owns itself (§8A.2) — conflating them is a design error.
type OfferEligibilityState string

const (
	OfferEligibilityEligible          OfferEligibilityState = "eligible"
	OfferEligibilityWithdrawalClaimed OfferEligibilityState = "withdrawal_claimed"
	OfferEligibilityWithdrawn         OfferEligibilityState = "withdrawn"
	OfferEligibilityAwardClaimed      OfferEligibilityState = "award_claimed"
	OfferEligibilityAwarded           OfferEligibilityState = "awarded"
)

type OfferEligibilitySnapshot struct {
	OfferVersionID string
	OfferChainID   string
	State          OfferEligibilityState
	OperationID    string
	ClaimID        string
	Revision       int64
}

type OfferEligibilityClaimRequest struct {
	CompanyID        string
	OfferVersionID   string
	OperationID      string
	ClaimID          string
	ExpectedRevision int64
	ClaimedAt        time.Time
}

type OfferEligibilityCompletionRequest struct {
	CompanyID        string
	OfferVersionID   string
	OperationID      string
	ExpectedRevision int64
	CompletedAt      time.Time
}

type OfferEligibilityReleaseRequest struct {
	CompanyID        string
	OfferVersionID   string
	OperationID      string
	ClaimID          string
	ExpectedRevision int64
}

// OfferEligibilityClaimant is satisfied by a composition adapter over
// supplieroffers' eligibility repository.
//
// ReleaseOfferAwardClaim is the only compensating transition, and it is unsafe
// on its own: supplieroffers has no award knowledge and cannot check whether an
// Award Revision exists (ADR 0002). That verification belongs to this module's
// finalisation service, immediately before it calls release — otherwise a
// release could undo an authoritative award, exactly what D2 forbids (§8E).
type OfferEligibilityClaimant interface {
	ClaimOfferForAward(
		ctx context.Context,
		request OfferEligibilityClaimRequest,
	) (OfferEligibilitySnapshot, error)
	CompleteOfferAward(
		ctx context.Context,
		request OfferEligibilityCompletionRequest,
	) (OfferEligibilitySnapshot, error)
	ReleaseOfferAwardClaim(
		ctx context.Context,
		request OfferEligibilityReleaseRequest,
	) (OfferEligibilitySnapshot, error)
	GetOfferEligibility(
		ctx context.Context,
		companyID string,
		offerVersionID string,
	) (OfferEligibilitySnapshot, bool, error)
}

// AwardOutcomeNotification carries only what the mail template renders. It
// deliberately holds no Supplier credential, session token or invitation
// secret: F8 must be unable to leak one into a delivery record or a log.
// InvitationID is a plain identifier, not a secret — it identifies which
// Supplier Access invitation the notification's link targets, so the
// notification can say what the acknowledgement/outcome routes require to
// find it again, without carrying the link itself.
type AwardOutcomeNotification struct {
	CompanyID         string
	CompanyName       string
	SupplierID        string
	SupplierName      string
	InvitationID      string
	RecipientIdentity string
	RFQNumber         string
	RFQTitle          string
	OutcomeID         string
	Result            string
	ContractorMessage string
	OutcomeURL        string
}

// AwardNotificationMailer is satisfied by a composition adapter over
// platform/mail.
//
// It receives the notification (recipient/lifecycle/identity) and the
// immutable OutcomeProjection (awarded lines, amounts, currency) as two
// separate arguments rather than one merged struct: the projection is
// already the single authoritative snapshot of what this Supplier was
// awarded, and duplicating its contents into AwardOutcomeNotification would
// create a second historical truth for the same fact.
type AwardNotificationMailer interface {
	SendAwardOutcomeNotification(
		ctx context.Context,
		notification AwardOutcomeNotification,
		projection OutcomeProjection,
	) error
}

// InvitationLinkSource is the narrow capability this module needs to build a
// Supplier Access URL for an outcome notification.
//
// Satisfied structurally by rfqissuance.Service.DeriveInvitationLink, which
// re-derives the invitation's EXISTING access secret — no rotation, no new
// invitation, no delivery-attempt record, no mutation. awards never touches
// the raw invitation secret's derivation itself; it only asks for the
// finished URL.
type InvitationLinkSource interface {
	DeriveInvitationLink(
		ctx context.Context,
		companyID string,
		invitationID string,
	) (InvitationLink, error)
}

// InvitationLink is the narrow shape this module needs from a derived
// Supplier Access link — a structural mirror of rfqissuance.InvitationLink,
// declared here rather than imported so awards never names rfqissuance by
// type (ADR 0002).
type InvitationLink struct {
	Token string
	URL   string
}

// AwardAuditRecorder is primitive-only. Money, commercial text, projections and
// full domain aggregates cannot cross it, so an audit sink can never become a
// second, drifting copy of the award record.
//
// Emission follows the ensure-once rule of §8F: the deterministic identity
// CompanyID + EventType + subject + operation ID means a crash between the
// authoritative write and its audit event is repaired by any completion path,
// while a replay records nothing.
type AwardAuditRecorder interface {
	RecordAwardDraftCreated(
		ctx context.Context,
		companyID, actorUserID, awardChainID, awardDraftID string,
		revision int64,
		occurredAt time.Time,
	) error
	RecordAwardDraftUpdated(
		ctx context.Context,
		companyID, actorUserID, awardChainID, awardDraftID string,
		revision int64,
		occurredAt time.Time,
	) error
	RecordAwardDraftDiscarded(
		ctx context.Context,
		companyID, actorUserID, awardChainID, awardDraftID string,
		revision int64,
		occurredAt time.Time,
	) error
	RecordAwardFinalised(
		ctx context.Context,
		companyID, actorUserID, awardChainID, awardRevisionID,
		finalisationOperationID string,
		revisionNumber int,
		occurredAt time.Time,
	) error
	RecordAwardCorrected(
		ctx context.Context,
		companyID, actorUserID, awardChainID, awardRevisionID,
		supersededRevisionID, finalisationOperationID string,
		revisionNumber int,
		occurredAt time.Time,
	) error
	RecordAwardOutcomeGenerated(
		ctx context.Context,
		companyID, actorUserID, awardChainID, awardRevisionID, supplierID,
		invitationID, outcomeID, result string,
		occurredAt time.Time,
	) error
	RecordAwardOutcomeNotified(
		ctx context.Context,
		companyID, actorUserID, awardRevisionID, outcomeID, supplierID,
		deliveryID, deliveryOperationID string,
		occurredAt time.Time,
	) error
	RecordAwardOutcomeNotificationRetried(
		ctx context.Context,
		companyID, actorUserID, awardRevisionID, outcomeID, supplierID,
		deliveryID, deliveryOperationID string,
		occurredAt time.Time,
	) error
	RecordAwardOutcomeNotificationObsoleted(
		ctx context.Context,
		companyID, actorUserID, awardRevisionID, outcomeID, supplierID,
		deliveryID string,
		occurredAt time.Time,
	) error
	RecordAwardOutcomeAcknowledged(
		ctx context.Context,
		companyID, outcomeID, supplierID, invitationID, sessionID string,
		occurredAt time.Time,
	) error
	RecordAwardReconciled(
		ctx context.Context,
		companyID, actorUserID, awardChainID, awardRevisionID string,
		revisionNumber int,
		occurredAt time.Time,
	) error
}
