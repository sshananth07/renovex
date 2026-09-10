package audit

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrEventNotRecorded wraps an underlying persistence failure. Audit writes
// are best-effort at every call site — a failure here never fails the
// business operation that triggered it (phase1.md §50: audit exists for
// accountability and debugging, not transactional correctness).
var ErrEventNotRecorded = errors.New("audit: failed to record event")

// EventRepository persists audit events. audit owns the audit_events
// collection exclusively.
type EventRepository interface {
	Create(ctx context.Context, e Event) error
}

// Service implements AuditRecorder. Each method constructs its own fixed,
// allowlisted Event internally from exactly its primitive arguments.
type Service struct {
	repo EventRepository
}

// NewService constructs a Service backed by repo.
func NewService(repo EventRepository) *Service {
	return &Service{repo: repo}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public EventRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Event owned by companyID.
// Development-tool use only (demoseed reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("audit: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// contractorOrSystemActor resolves the actor pair for a contractor-initiated
// method. An empty actorUserID means the operation was triggered structurally
// rather than by a directly-acting human — e.g. the revoke that accompanies a
// supersession, whose real human actor is already recorded on the companion
// Access Created / Quotation Sent event (design spec §12.1).
func contractorOrSystemActor(actorUserID string) (actorType, actorID string) {
	if actorUserID == "" {
		return ActorTypeSystem, ""
	}
	return ActorTypeContractor, actorUserID
}

func (s *Service) RecordQuotationSent(ctx context.Context, companyID, projectID, actorUserID, quotationID, quotationNumber string, version int) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeQuotationSent,
		SubjectType: SubjectTypeQuotation, SubjectID: quotationID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{
			"quotationId": quotationID, "quotationNumber": quotationNumber, "version": version,
		},
	})
}

func (s *Service) RecordAccessCreated(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber string, version int) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeAccessCreated,
		SubjectType: SubjectTypeAccessGrant, SubjectID: grantID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{
			"grantId": grantID, "quotationId": quotationID,
			"quotationNumber": quotationNumber, "version": version,
		},
	})
}

func (s *Service) RecordAccessRevoked(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber, revokedReason string, version int) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeAccessRevoked,
		SubjectType: SubjectTypeAccessGrant, SubjectID: grantID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{
			"grantId": grantID, "quotationId": quotationID,
			"quotationNumber": quotationNumber, "version": version,
			"revokedReason": revokedReason,
		},
	})
}

func (s *Service) RecordClientViewedQuotation(ctx context.Context, companyID, projectID, grantID, quotationID, quotationNumber string, version int) error {
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeClientViewedQuotation,
		SubjectType: SubjectTypeQuotation, SubjectID: quotationID,
		// No authenticated Client identity exists in M6 — ActorID stays empty.
		ActorType: ActorTypeClient, ActorID: "",
		Metadata: map[string]any{
			"grantId": grantID, "quotationId": quotationID,
			"quotationNumber": quotationNumber, "version": version,
		},
	})
}

func (s *Service) RecordClientDecision(ctx context.Context, companyID, projectID, grantID, approvalID, quotationID, quotationNumber string, version int, decisionStatus, clientName, clientEmail string) error {
	eventType := EventTypeClientRequestedChanges
	switch decisionStatus {
	case "accepted":
		eventType = EventTypeClientAcceptedQuotation
	case "rejected":
		eventType = EventTypeClientRejectedQuotation
	}

	// Note: the Client's free-text comment is deliberately absent — this
	// method has no parameter for it, so it can never reach an audit record.
	metadata := map[string]any{
		"grantId": grantID, "approvalId": approvalID, "quotationId": quotationID,
		"quotationNumber": quotationNumber, "version": version,
	}
	if clientName != "" {
		metadata["clientName"] = clientName
	}
	if clientEmail != "" {
		metadata["clientEmail"] = clientEmail
	}

	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   eventType,
		SubjectType: SubjectTypeQuotation, SubjectID: quotationID,
		ActorType: ActorTypeClient, ActorID: "",
		Metadata: metadata,
	})
}

// --- Milestone 7 ---
//
// The 24 methods below satisfy three SEPARATE consumer-owned AuditRecorder
// interfaces structurally: materialrequirements (8), rfqs (8), suppliers (8).
// There is deliberately no shared M7 audit interface, so no module depends on a
// contract wider than the events it actually emits (M7 design spec §1.5).
//
// Every parameter is a primitive. No method has a parameter capable of carrying
// a money.Money, a quantity.Quantity, a decimal, or a whole domain struct, so an
// internal cost figure or an entire aggregate cannot reach an audit record even
// by mistake. The signature IS the allowlist.

// RecordMaterialRequirementsGenerated records one generation run as counts
// only. Requirement bodies are never included: the event answers "what
// happened", not "what was created".
func (s *Service) RecordMaterialRequirementsGenerated(ctx context.Context, companyID, projectID, actorUserID string,
	createdCount, unchangedCount, discrepancyCount, sourceRemovedCount, skippedCount int) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementsGenerated,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: "",
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{
			"createdCount": createdCount, "unchangedCount": unchangedCount,
			"discrepancyCount": discrepancyCount, "sourceRemovedCount": sourceRemovedCount,
			"skippedCount": skippedCount,
		},
	})
}

func (s *Service) RecordMaterialRequirementCreated(ctx context.Context, companyID, projectID, actorUserID,
	requirementID, materialID, sourceType string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementCreated,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: requirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"materialId": materialID, "sourceType": sourceType},
	})
}

// RecordMaterialRequirementUpdated records an edit. reviewReset states whether
// the edit moved reviewed back to draft, which distinguishes a
// procurement-relevant change from an InternalNotes-only one (M7 design spec
// §5.4). The field VALUES are never recorded — only that they changed.
func (s *Service) RecordMaterialRequirementUpdated(ctx context.Context, companyID, projectID, actorUserID,
	requirementID string, reviewReset bool) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementUpdated,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: requirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"reviewReset": reviewReset},
	})
}

func (s *Service) RecordMaterialRequirementReviewed(ctx context.Context, companyID, projectID, actorUserID,
	requirementID, materialID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementReviewed,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: requirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"materialId": materialID},
	})
}

// RecordMaterialRequirementUnitAcknowledged records that the contractor
// accepted a procurement unit differing from the catalog unit. This relaxes an
// RFQ eligibility gate, so it is audited explicitly (M7 design spec §3.5, §8.5).
func (s *Service) RecordMaterialRequirementUnitAcknowledged(ctx context.Context, companyID, projectID, actorUserID,
	requirementID, procurementUnit, catalogUnit string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementUnitAcknowledged,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: requirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"procurementUnit": procurementUnit, "catalogUnit": catalogUnit},
	})
}

// RecordMaterialRequirementDiscrepancyResolved records which resolution the
// contractor chose, the sync state it resolved from, and the anchor's status —
// the last because update/merge are unavailable on a terminal anchor (M7 design
// spec §5.6). Quantities are deliberately absent.
func (s *Service) RecordMaterialRequirementDiscrepancyResolved(ctx context.Context, companyID, projectID, actorUserID,
	requirementID, action, syncStateBefore, anchorStatus string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementDiscrepancyResolved,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: requirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{
			"action": action, "syncStateBefore": syncStateBefore, "anchorStatus": anchorStatus,
		},
	})
}

func (s *Service) RecordMaterialRequirementSplit(ctx context.Context, companyID, projectID, actorUserID,
	sourceRequirementID, splitGroupID string, childCount int) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementSplit,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: sourceRequirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"splitGroupId": splitGroupID, "childCount": childCount},
	})
}

func (s *Service) RecordMaterialRequirementArchived(ctx context.Context, companyID, projectID, actorUserID,
	requirementID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementArchived,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: requirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{},
	})
}

func (s *Service) RecordMaterialRequirementDeleted(ctx context.Context, companyID, projectID, actorUserID,
	requirementID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeMaterialRequirementDeleted,
		SubjectType: SubjectTypeMaterialRequirement, SubjectID: requirementID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{},
	})
}

// rfqEvent builds an RFQ event. The SubjectID is always the stable RFQ chain
// id; rfqNumber travels in metadata as display context only, because M8's
// issued versions reference the chain id and never the formatted number (M7
// design spec §6.1).
func (s *Service) rfqEvent(companyID, projectID, actorUserID, eventType, rfqChainID, rfqNumber string,
	extra map[string]any) Event {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	metadata := map[string]any{"rfqChainId": rfqChainID}
	if rfqNumber != "" {
		metadata["rfqNumber"] = rfqNumber
	}
	for k, v := range extra {
		metadata[k] = v
	}
	return Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   eventType,
		SubjectType: SubjectTypeRFQ, SubjectID: rfqChainID,
		ActorType: actorType, ActorID: actorID,
		Metadata: metadata,
	}
}

// --- Milestone 8: immutable RFQ issuance ---
//
// These methods satisfy rfqissuance's consumer-owned AuditRecorder
// structurally. Every argument is a primitive identity or business version;
// no RFQ body, quantity, price, operation secret, or whole domain record can
// enter the append-only audit event.

func (s *Service) RecordRFQVersionIssued(ctx context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, versionID string,
	versionNumber int) error {

	return s.record(ctx, s.rfqEvent(
		companyID, projectID, actorUserID, EventTypeRFQVersionIssued,
		rfqChainID, rfqNumber,
		map[string]any{"versionId": versionID, "versionNumber": versionNumber},
	))
}

func (s *Service) RecordRFQAmendmentDraftCreated(ctx context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, draftID string,
	baseVersionNumber int) error {

	return s.record(ctx, s.rfqEvent(
		companyID, projectID, actorUserID, EventTypeRFQAmendmentDraftCreated,
		rfqChainID, rfqNumber,
		map[string]any{
			"draftId": draftID, "baseVersionNumber": baseVersionNumber,
		},
	))
}

func (s *Service) RecordRFQAmendmentDraftUpdated(ctx context.Context,
	companyID, actorUserID, rfqChainID, draftID string, revision int64) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeRFQAmendmentDraftUpdated,
		rfqChainID, "",
		map[string]any{"draftId": draftID, "revision": revision},
	))
}

func (s *Service) RecordRFQAmendmentDraftDiscarded(ctx context.Context,
	companyID, actorUserID, rfqChainID, draftID string, revision int64) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeRFQAmendmentDraftDiscarded,
		rfqChainID, "",
		map[string]any{"draftId": draftID, "revision": revision},
	))
}

func (s *Service) RecordRFQAmendmentIssued(ctx context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, draftID, versionID string,
	versionNumber int) error {

	return s.record(ctx, s.rfqEvent(
		companyID, projectID, actorUserID, EventTypeRFQAmendmentIssued,
		rfqChainID, rfqNumber,
		map[string]any{
			"draftId": draftID, "versionId": versionID,
			"versionNumber": versionNumber,
		},
	))
}

func (s *Service) RecordRFQIssuanceChainReconciled(ctx context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, versionID string,
	versionNumber int) error {

	return s.record(ctx, s.rfqEvent(
		companyID, projectID, actorUserID, EventTypeRFQIssuanceChainReconciled,
		rfqChainID, rfqNumber,
		map[string]any{"versionId": versionID, "versionNumber": versionNumber},
	))
}

func (s *Service) RecordRFQCreated(ctx context.Context, companyID, projectID, actorUserID, rfqChainID, rfqNumber string) error {
	return s.record(ctx, s.rfqEvent(companyID, projectID, actorUserID, EventTypeRFQCreated, rfqChainID, rfqNumber, nil))
}

func (s *Service) RecordRFQUpdated(ctx context.Context, companyID, projectID, actorUserID, rfqChainID, rfqNumber string) error {
	return s.record(ctx, s.rfqEvent(companyID, projectID, actorUserID, EventTypeRFQUpdated, rfqChainID, rfqNumber, nil))
}

func (s *Service) RecordRFQLineAdded(ctx context.Context, companyID, projectID, actorUserID,
	rfqChainID, rfqNumber, requirementID, lineID string) error {
	return s.record(ctx, s.rfqEvent(companyID, projectID, actorUserID, EventTypeRFQLineAdded, rfqChainID, rfqNumber,
		map[string]any{"materialRequirementId": requirementID, "lineId": lineID}))
}

func (s *Service) RecordRFQLineRemoved(ctx context.Context, companyID, projectID, actorUserID,
	rfqChainID, rfqNumber, requirementID, lineID string) error {
	return s.record(ctx, s.rfqEvent(companyID, projectID, actorUserID, EventTypeRFQLineRemoved, rfqChainID, rfqNumber,
		map[string]any{"materialRequirementId": requirementID, "lineId": lineID}))
}

func (s *Service) RecordRFQMarkedReady(ctx context.Context, companyID, projectID, actorUserID,
	rfqChainID, rfqNumber string, lineCount int) error {
	return s.record(ctx, s.rfqEvent(companyID, projectID, actorUserID, EventTypeRFQMarkedReady, rfqChainID, rfqNumber,
		map[string]any{"lineCount": lineCount}))
}

func (s *Service) RecordRFQReopened(ctx context.Context, companyID, projectID, actorUserID, rfqChainID, rfqNumber string) error {
	return s.record(ctx, s.rfqEvent(companyID, projectID, actorUserID, EventTypeRFQReopened, rfqChainID, rfqNumber, nil))
}

// RecordRFQDeleted records deletion of an empty draft. It is audited because a
// number is consumed and a record disappears (M7 design spec §17).
func (s *Service) RecordRFQDeleted(ctx context.Context, companyID, projectID, actorUserID, rfqChainID, rfqNumber string) error {
	return s.record(ctx, s.rfqEvent(companyID, projectID, actorUserID, EventTypeRFQDeleted, rfqChainID, rfqNumber, nil))
}

// RecordRFQClaimReconciled records repair of an orphaned claim — either
// retry_line or release (M7 design spec §7.5).
func (s *Service) RecordRFQClaimReconciled(ctx context.Context, companyID, projectID, actorUserID,
	rfqChainID, requirementID, action string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID: companyID, ProjectID: projectID,
		EventType:   EventTypeRFQClaimReconciled,
		SubjectType: SubjectTypeRFQ, SubjectID: rfqChainID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{
			"rfqChainId": rfqChainID, "materialRequirementId": requirementID, "action": action,
		},
	})
}

// Supplier events carry NO ProjectID: the Supplier Directory is company-wide,
// never project-scoped (M7 design spec §4.1).

func (s *Service) RecordSupplierCreated(ctx context.Context, companyID, actorUserID, supplierID, supplierName string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypeSupplierCreated,
		SubjectType: SubjectTypeSupplier, SubjectID: supplierID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"supplierName": supplierName},
	})
}

func (s *Service) RecordSupplierUpdated(ctx context.Context, companyID, actorUserID, supplierID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypeSupplierUpdated,
		SubjectType: SubjectTypeSupplier, SubjectID: supplierID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{},
	})
}

// RecordSupplierActiveStateChanged records retirement or reactivation. There is
// no cascade: offerings and preferences are never modified by this transition
// (M7 design spec §4.1).
func (s *Service) RecordSupplierActiveStateChanged(ctx context.Context, companyID, actorUserID, supplierID string, active bool) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypeSupplierActiveStateChanged,
		SubjectType: SubjectTypeSupplier, SubjectID: supplierID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"active": active},
	})
}

// The offering methods deliberately record no price. An IndicativePrice is
// informational and must never appear in an audit trail as though it were a
// formal supplier quotation (M7 design spec §4.2).

func (s *Service) RecordSupplierOfferingCreated(ctx context.Context, companyID, actorUserID, supplierID, offeringID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypeSupplierOfferingCreated,
		SubjectType: SubjectTypeSupplierOffering, SubjectID: offeringID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"supplierId": supplierID},
	})
}

func (s *Service) RecordSupplierOfferingUpdated(ctx context.Context, companyID, actorUserID, supplierID, offeringID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypeSupplierOfferingUpdated,
		SubjectType: SubjectTypeSupplierOffering, SubjectID: offeringID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"supplierId": supplierID},
	})
}

func (s *Service) RecordSupplierOfferingActiveStateChanged(ctx context.Context, companyID, actorUserID,
	supplierID, offeringID string, active bool) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypeSupplierOfferingActiveStateChanged,
		SubjectType: SubjectTypeSupplierOffering, SubjectID: offeringID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"supplierId": supplierID, "active": active},
	})
}

// The preference is keyed by Material — at most one preferred Supplier each —
// so the Material is the subject (M7 design spec §4.3).

func (s *Service) RecordPreferredSupplierChanged(ctx context.Context, companyID, actorUserID, materialID, supplierID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypePreferredSupplierChanged,
		SubjectType: SubjectTypeMaterialSupplierPreference, SubjectID: materialID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"materialId": materialID, "supplierId": supplierID},
	})
}

// RecordPreferredSupplierCleared records the previous SupplierID, because the
// preference row may be physically deleted and the audit trail is then the only
// remaining history (M7 design spec §4.3, §17).
func (s *Service) RecordPreferredSupplierCleared(ctx context.Context, companyID, actorUserID, materialID, previousSupplierID string) error {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return s.record(ctx, Event{
		CompanyID:   companyID,
		EventType:   EventTypePreferredSupplierCleared,
		SubjectType: SubjectTypeMaterialSupplierPreference, SubjectID: materialID,
		ActorType: actorType, ActorID: actorID,
		Metadata: map[string]any{"materialId": materialID, "previousSupplierId": previousSupplierID},
	})
}

func (s *Service) record(ctx context.Context, e Event) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	e.SchemaVersion = 1
	return s.repo.Create(ctx, e)
}
