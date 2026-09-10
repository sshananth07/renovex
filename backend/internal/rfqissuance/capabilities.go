package rfqissuance

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

// The capabilities rfqissuance CONSUMES (design spec §2, §2.1).
//
// Every method takes primitives and returns primitives or rfqissuance-owned
// types, so a provider satisfies these interfaces without either side importing
// the other (ADR 0002). Where Go's exact-return-type rule makes structural
// satisfaction impossible, the composition root supplies a thin adapter.

// ReadyRFQSnapshot is rfqissuance-owned: the allowlisted M7 projection this
// module snapshots into an immutable issued version.
//
// It carries no contractor internal notes, no estimated or committed cost, no
// margin, no indicative Supplier Offering price and no preferred-Supplier data.
// Those fields do not exist on this type, so the supplier-visible boundary is
// structural rather than a matter of remembering to omit them (design spec
// §3.2, §6.4).
type ReadyRFQSnapshot struct {
	RFQNumber           string
	ProjectID           string
	SourceM7RFQRevision int64
	Title               string
	DeliveryAddress     string
	RequiredByDate      *time.Time
	// ResponseDeadline is optional HERE because M7 permits a ready RFQ without
	// one. Issuance refuses such an RFQ rather than inventing a commercial date
	// (design spec §4.1A) — which is why the issued version's own field is a
	// concrete time.Time while this one is not.
	ResponseDeadline     *time.Time
	SupplierInstructions string
	Lines                []ReadyRFQLineSnapshot
}

// ReadyRFQLineSnapshot is one supplier-visible M7 line.
//
// Two provenance identifiers travel with it (design spec §2.1A, §3.2A):
//
//   - SourceRFQLineID is the M7 line identity, recorded on the issued line as
//     SourceM7RFQLineID. It is NEVER reused as the issued line's own ID.
//   - SourceMaterialRequirementID is the originating Material Requirement,
//     which is what copy-forward matches compatible lines on (approved
//     decision 11) — matching on Material ID alone would wrongly cross-match
//     two lines for the same Material under different requirements.
type ReadyRFQLineSnapshot struct {
	SourceRFQLineID             string
	SourceMaterialRequirementID string
	MaterialID                  string
	MaterialName                string
	Specification               string
	QuantityValue               string // canonical decimal string; never a float
	QuantityUnit                string
	RequiredByDate              *time.Time
	ProcurementNotes            string
	SortOrder                   int
}

// ReadyRFQSource is the ONLY way rfqissuance reaches an M7 RFQ.
//
// M8 never writes to rfqs, rfq_counters or material_requirements; this
// read-only capability is the entire M7 surface (design spec §2.1).
//
// A draft or absent RFQ reports found = false rather than an error, so issuance
// can refuse cleanly without learning why the chain is not ready. An
// infrastructure failure propagates unchanged, because treating it as "not
// ready" would hide an unknown source state behind a normal-looking refusal.
type ReadyRFQSource interface {
	GetReadyRFQSnapshot(ctx context.Context, companyID, rfqChainID string) (
		snapshot ReadyRFQSnapshot, found bool, err error)
}

// SupplierLookup is the ONLY way rfqissuance reaches the Supplier Directory
// (design spec §5.1).
//
// It answers ONE boolean rather than returning a Supplier. Three distinct
// refusals — absent, another company's, archived — collapse into `false` on
// purpose: a caller probing supplier IDs must not be able to tell which
// applies, and rfqissuance has no legitimate use for any other Supplier field.
// Returning a struct would also force this module to name a suppliers type,
// which ADR 0002 forbids.
type SupplierLookup interface {
	SupplierIsInvitable(ctx context.Context, companyID, supplierID string) (bool, error)
}

// RecipientReplacementPreparation asks supplieroffers to take the durable
// replacement barrier. It carries primitives only: rfqissuance never names a
// supplieroffers draft, chain or commercial type.
type RecipientReplacementPreparation struct {
	CompanyID                  string
	InvitationID               string
	IssuedRFQVersionID         string
	PreviousRecipientIdentity  string
	CandidateRecipientIdentity string
	ReplacementOperationID     string
}

// RecipientReplacementCompletion archives the held claim after the
// authoritative invitation replacement is confirmed.
type RecipientReplacementCompletion struct {
	CompanyID                    string
	InvitationID                 string
	IssuedRFQVersionID           string
	PreviousRecipientIdentity    string
	ReplacementRecipientIdentity string
	ReplacementOperationID       string
}

// RecipientReplacementAbort releases a claim that provably never became an
// authoritative replacement.
type RecipientReplacementAbort struct {
	CompanyID                 string
	InvitationID              string
	IssuedRFQVersionID        string
	PreviousRecipientIdentity string
	ReplacementOperationID    string
}

// OfferWorkspaceCoordinator is the narrow consumer-owned capability through
// which rfqissuance serializes recipient replacement against Supplier draft
// activity (§5.3A). supplieroffers implements it via a composition adapter;
// there is no cross-module repository access in either direction.
//
// Ordering is the contract: Prepare must succeed BEFORE the authoritative
// invitation write, and Complete runs only after that write is confirmed. The
// claim Prepare takes keeps holding the unfinished-draft uniqueness slot, so a
// crash between the two leaves the previous recipient locked out rather than
// silently authoritative again.
type OfferWorkspaceCoordinator interface {
	PrepareRecipientReplacement(context.Context, RecipientReplacementPreparation) error
	CompleteRecipientReplacement(context.Context, RecipientReplacementCompletion) error
	AbortRecipientReplacement(context.Context, RecipientReplacementAbort) error
}

// Mailer is the narrow send surface, satisfied directly by
// platform/mail.EmailSender.
//
// mail.Message is a platform INFRASTRUCTURE type carrying no domain meaning, so
// naming it here does not create the cross-domain dependency ADR 0002 forbids —
// the same reasoning that lets companies.Service take a mailer.
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// AuditRecorder is the primitive-only audit surface rfqissuance consumes.
// Domain structs, raw RFQ content, quantities, prices, and secrets cannot cross
// this interface because no method accepts them.
type AuditRecorder interface {
	RecordRFQVersionIssued(ctx context.Context,
		companyID, projectID, actorUserID, rfqChainID, rfqNumber, versionID string,
		versionNumber int) error
	RecordRFQAmendmentDraftCreated(ctx context.Context,
		companyID, projectID, actorUserID, rfqChainID, rfqNumber, draftID string,
		baseVersionNumber int) error
	RecordRFQAmendmentDraftUpdated(ctx context.Context,
		companyID, actorUserID, rfqChainID, draftID string,
		revision int64) error
	RecordRFQAmendmentDraftDiscarded(ctx context.Context,
		companyID, actorUserID, rfqChainID, draftID string,
		revision int64) error
	RecordRFQAmendmentIssued(ctx context.Context,
		companyID, projectID, actorUserID, rfqChainID, rfqNumber, draftID, versionID string,
		versionNumber int) error
	RecordRFQIssuanceChainReconciled(ctx context.Context,
		companyID, projectID, actorUserID, rfqChainID, rfqNumber, versionID string,
		versionNumber int) error

	// Invitations (§15). Every parameter is a primitive, and there is NO
	// parameter capable of carrying a raw invitation link, token or secret —
	// the signature IS the allowlist, so a secret cannot reach an audit record
	// even by mistake.
	RecordSupplierInvitationCreated(ctx context.Context,
		companyID, projectID, actorUserID, rfqChainID, rfqNumber, invitationID,
		supplierID string) error
	RecordSupplierInvitationSent(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID, attemptID, channel string) error
	RecordSupplierInvitationDeliveryFailed(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID, attemptID, failureCode string) error
	RecordSupplierInvitationLinkCopied(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID string,
		accessGeneration int64) error
	RecordSupplierInvitationRecipientReplaced(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID string,
		accessGeneration int64) error
	RecordSupplierInvitationSecretRotated(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID string,
		accessGeneration int64) error
	RecordSupplierInvitationRevoked(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID string) error
	RecordSupplierInvitationReactivated(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID, issuedVersionID string,
		accessGeneration int64) error
	RecordSupplierInvitationExpiryChanged(ctx context.Context,
		companyID, actorUserID, rfqChainID, invitationID string) error
	RecordSupplierInvitationAdvanced(ctx context.Context,
		companyID, actorUserID, rfqChainID, versionID string, invitationCount int64) error
}

type noOpAuditRecorder struct{}

func (noOpAuditRecorder) RecordRFQVersionIssued(
	context.Context, string, string, string, string, string, string, int,
) error {
	return nil
}

func (noOpAuditRecorder) RecordRFQAmendmentDraftCreated(
	context.Context, string, string, string, string, string, string, int,
) error {
	return nil
}

func (noOpAuditRecorder) RecordRFQAmendmentDraftUpdated(
	context.Context, string, string, string, string, int64,
) error {
	return nil
}

func (noOpAuditRecorder) RecordRFQAmendmentDraftDiscarded(
	context.Context, string, string, string, string, int64,
) error {
	return nil
}

func (noOpAuditRecorder) RecordRFQAmendmentIssued(
	context.Context, string, string, string, string, string, string, string, int,
) error {
	return nil
}

func (noOpAuditRecorder) RecordRFQIssuanceChainReconciled(
	context.Context, string, string, string, string, string, string, int,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationCreated(
	context.Context, string, string, string, string, string, string, string,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationSent(
	context.Context, string, string, string, string, string, string,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationDeliveryFailed(
	context.Context, string, string, string, string, string, string,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationLinkCopied(
	context.Context, string, string, string, string, int64,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationRecipientReplaced(
	context.Context, string, string, string, string, int64,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationSecretRotated(
	context.Context, string, string, string, string, int64,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationRevoked(
	context.Context, string, string, string, string,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationReactivated(
	context.Context, string, string, string, string, string, int64,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationExpiryChanged(
	context.Context, string, string, string, string,
) error {
	return nil
}

func (noOpAuditRecorder) RecordSupplierInvitationAdvanced(
	context.Context, string, string, string, string, int64,
) error {
	return nil
}
