// Package audit owns Audit Events: an append-only record of critical
// business events (Quotation Sent, Client Viewed Quotation, etc.) layered on
// top of current-state domain records, not full event sourcing.
// See phase1.md §50.
//
// The AuditRecorder interface below is deliberately one TYPED METHOD PER
// EVENT taking only primitive arguments — never a generic
// metadata map[string]any. Each method's signature IS the allowlist: there
// is no parameter on any method capable of carrying a raw access token, a
// whole domain struct, an internal cost/margin figure, or a Client's
// free-text comment, so none of those can reach an audit record even by
// mistake (M6 design spec §12).
package audit

import (
	"context"
	"time"
)

// Event types recorded by M6. The names mirror phase1.md §50's own list.
const (
	EventTypeQuotationSent           = "quotation_sent"
	EventTypeAccessCreated           = "access_created"
	EventTypeAccessRevoked           = "access_revoked"
	EventTypeClientViewedQuotation   = "client_viewed_quotation"
	EventTypeClientAcceptedQuotation = "client_accepted_quotation"
	EventTypeClientRejectedQuotation = "client_rejected_quotation"
	EventTypeClientRequestedChanges  = "client_requested_changes"
)

// Event types recorded by M7 (M7 design spec §1.5). Three separate
// consumer-owned AuditRecorder interfaces are satisfied by Service structurally
// — materialrequirements (8 methods), rfqs (8), suppliers (8) — deliberately
// with no shared cross-module audit interface, so no module depends on a
// contract wider than it uses.
const (
	// Material Requirements.
	EventTypeMaterialRequirementsGenerated          = "material_requirements_generated"
	EventTypeMaterialRequirementCreated             = "material_requirement_created"
	EventTypeMaterialRequirementUpdated             = "material_requirement_updated"
	EventTypeMaterialRequirementReviewed            = "material_requirement_reviewed"
	EventTypeMaterialRequirementUnitAcknowledged    = "material_requirement_unit_acknowledged"
	EventTypeMaterialRequirementDiscrepancyResolved = "material_requirement_discrepancy_resolved"
	EventTypeMaterialRequirementSplit               = "material_requirement_split"
	EventTypeMaterialRequirementArchived            = "material_requirement_archived"
	EventTypeMaterialRequirementDeleted             = "material_requirement_deleted"

	// RFQs.
	EventTypeRFQCreated         = "rfq_created"
	EventTypeRFQUpdated         = "rfq_updated"
	EventTypeRFQLineAdded       = "rfq_line_added"
	EventTypeRFQLineRemoved     = "rfq_line_removed"
	EventTypeRFQMarkedReady     = "rfq_marked_ready"
	EventTypeRFQReopened        = "rfq_reopened"
	EventTypeRFQDeleted         = "rfq_deleted"
	EventTypeRFQClaimReconciled = "rfq_claim_reconciled"

	// Suppliers, offerings and the advisory material-supplier preference.
	EventTypeSupplierCreated                    = "supplier_created"
	EventTypeSupplierUpdated                    = "supplier_updated"
	EventTypeSupplierActiveStateChanged         = "supplier_active_state_changed"
	EventTypeSupplierOfferingCreated            = "supplier_offering_created"
	EventTypeSupplierOfferingUpdated            = "supplier_offering_updated"
	EventTypeSupplierOfferingActiveStateChanged = "supplier_offering_active_state_changed"
	EventTypePreferredSupplierChanged           = "preferred_supplier_changed"
	EventTypePreferredSupplierCleared           = "preferred_supplier_cleared"
)

// RFQ issuance event types recorded by M8. These are distinct from M7's
// editable-RFQ events: an issued version is an immutable external record.
const (
	EventTypeRFQVersionIssued           = "rfq_version_issued"
	EventTypeRFQAmendmentDraftCreated   = "rfq_amendment_draft_created"
	EventTypeRFQAmendmentDraftUpdated   = "rfq_amendment_draft_updated"
	EventTypeRFQAmendmentDraftDiscarded = "rfq_amendment_draft_discarded"
	EventTypeRFQAmendmentIssued         = "rfq_amendment_issued"
	EventTypeRFQIssuanceChainReconciled = "rfq_issuance_chain_reconciled"
)

// Supplier Invitation event types recorded by M8 (design spec §15).
//
// None of these methods accepts a parameter capable of carrying a raw
// invitation link, token or secret. The signature IS the allowlist: a secret
// has nowhere to land, so it cannot reach an audit record even by mistake.
const (
	EventTypeSupplierInvitationCreated           = "supplier_invitation_created"
	EventTypeSupplierInvitationSent              = "supplier_invitation_sent"
	EventTypeSupplierInvitationDeliveryFailed    = "supplier_invitation_delivery_failed"
	EventTypeSupplierInvitationLinkCopied        = "supplier_invitation_link_copied"
	EventTypeSupplierInvitationRecipientReplaced = "supplier_invitation_recipient_replaced"
	EventTypeSupplierInvitationSecretRotated     = "supplier_invitation_secret_rotated"
	EventTypeSupplierInvitationRevoked           = "supplier_invitation_revoked"
	EventTypeSupplierInvitationReactivated       = "supplier_invitation_reactivated"
	EventTypeSupplierInvitationExpiryChanged     = "supplier_invitation_expiry_changed"
	EventTypeSupplierInvitationAdvanced          = "supplier_invitation_advanced"
)

// Supplier-access event types record only settled public-access transitions.
// Their method signatures accept no browser credential, verification code,
// email address, request content, or delivery-provider detail.
const (
	EventTypeSupplierChallengeRequested       = "supplier_challenge_requested"
	EventTypeSupplierChallengeDeliveryRetried = "supplier_challenge_delivery_retried"
	EventTypeSupplierVerificationFailed       = "supplier_verification_failed"
	EventTypeSupplierVerificationSucceeded    = "supplier_verification_succeeded"
	EventTypeSupplierSessionCreated           = "supplier_session_created"
	EventTypeSupplierSessionReverified        = "supplier_session_reverified"
	EventTypeSupplierSessionRenewed           = "supplier_session_renewed"
	EventTypeSupplierSessionRevoked           = "supplier_session_revoked"
)

// Supplier Offer and Award events are intentionally action-only records. The
// commercial aggregates retain prices, reasons and messages; audit stores only
// stable identities, versions and bounded enums needed for accountability.
const (
	EventTypeOfferDraftCreated                 = "offer_draft_created"
	EventTypeOfferDraftCopied                  = "offer_draft_copied"
	EventTypeOfferDraftUpdated                 = "offer_draft_updated"
	EventTypeOfferDraftArchived                = "offer_draft_archived"
	EventTypeOfferSubmitted                    = "offer_submitted"
	EventTypeOfferSubmissionReconciled         = "offer_submission_reconciled"
	EventTypeOfferWithdrawn                    = "offer_withdrawn"
	EventTypeAwardDraftCreated                 = "award_draft_created"
	EventTypeAwardDraftUpdated                 = "award_draft_updated"
	EventTypeAwardDraftDiscarded               = "award_draft_discarded"
	EventTypeAwardFinalised                    = "award_finalised"
	EventTypeAwardCorrected                    = "award_corrected"
	EventTypeAwardOutcomeGenerated             = "award_outcome_generated"
	EventTypeAwardOutcomeNotified              = "award_outcome_notified"
	EventTypeAwardOutcomeNotificationRetried   = "award_outcome_notification_retried"
	EventTypeAwardOutcomeNotificationObsoleted = "award_outcome_notification_obsoleted"
	EventTypeAwardOutcomeAcknowledged          = "award_outcome_acknowledged"
	EventTypeAwardReconciled                   = "award_reconciled"
)

// Subject types.
const (
	SubjectTypeQuotation   = "quotation"
	SubjectTypeAccessGrant = "access_grant"

	// M7 subject types.
	SubjectTypeMaterialRequirement = "material_requirement"
	// SubjectTypeRFQ's SubjectID is always the stable RFQ CHAIN id, never the
	// display RFQNumber (M7 design spec §6.1).
	SubjectTypeRFQ                        = "rfq"
	SubjectTypeSupplier                   = "supplier"
	SubjectTypeSupplierOffering           = "supplier_offering"
	SubjectTypeMaterialSupplierPreference = "material_supplier_preference"
	SubjectTypeSupplierInvitation         = "supplier_invitation"
	SubjectTypeSupplierSession            = "supplier_session"
	SubjectTypeOfferDraft                 = "offer_draft"
	SubjectTypeOfferVersion               = "offer_version"
	SubjectTypeOfferWithdrawal            = "offer_withdrawal"
	SubjectTypeAwardDraft                 = "award_draft"
	SubjectTypeAwardRevision              = "award_revision"
	SubjectTypeAwardOutcome               = "award_outcome"
	SubjectTypeAwardDelivery              = "award_delivery"
)

// Actor types.
const (
	ActorTypeContractor = "contractor"
	ActorTypeClient     = "client"
	ActorTypeSystem     = "system"
	ActorTypeSupplier   = "supplier"
)

// Event is one append-only audit record.
type Event struct {
	ID          string
	CompanyID   string
	ProjectID   string
	EventType   string
	SubjectType string
	SubjectID   string
	ActorType   string
	ActorID     string
	Metadata    map[string]any
	// DedupeIdentity is populated only for ensure-once events. Repository
	// uniqueness combines it with tenant, type and subject.
	DedupeIdentity string
	CreatedAt      time.Time
	SchemaVersion  int
}

// AuditRecorder is the capability other modules consume. Defined here (in
// audit) rather than in the consumer because every parameter and return
// value is already a primitive — audit.Service satisfies it directly with no
// adapter or conversion (design spec §14).
type AuditRecorder interface {
	RecordQuotationSent(ctx context.Context, companyID, projectID, actorUserID, quotationID, quotationNumber string, version int) error
	RecordAccessCreated(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber string, version int) error
	RecordAccessRevoked(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber, revokedReason string, version int) error
	RecordClientViewedQuotation(ctx context.Context, companyID, projectID, grantID, quotationID, quotationNumber string, version int) error
	RecordClientDecision(ctx context.Context, companyID, projectID, grantID, approvalID, quotationID, quotationNumber string, version int, decisionStatus, clientName, clientEmail string) error
}
