package audit

import (
	"context"
	"strconv"
	"time"
)

func awardEvent(companyID, actorUserID, eventType, subjectType, subjectID,
	dedupeIdentity string, occurredAt time.Time, metadata map[string]any) Event {
	actorType, actorID := contractorOrSystemActor(actorUserID)
	return Event{CompanyID: companyID, EventType: eventType,
		SubjectType: subjectType, SubjectID: subjectID,
		ActorType: actorType, ActorID: actorID, Metadata: metadata,
		DedupeIdentity: dedupeIdentity, CreatedAt: occurredAt}
}

func (s *Service) RecordAwardDraftCreated(ctx context.Context,
	companyID, actorUserID, awardChainID, awardDraftID string,
	revision int64, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID, EventTypeAwardDraftCreated,
		SubjectTypeAwardDraft, awardDraftID, strconv.FormatInt(revision, 10),
		occurredAt, map[string]any{"awardChainId": awardChainID, "revision": revision}))
}

func (s *Service) RecordAwardDraftUpdated(ctx context.Context,
	companyID, actorUserID, awardChainID, awardDraftID string,
	revision int64, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID, EventTypeAwardDraftUpdated,
		SubjectTypeAwardDraft, awardDraftID, strconv.FormatInt(revision, 10),
		occurredAt, map[string]any{"awardChainId": awardChainID, "revision": revision}))
}

func (s *Service) RecordAwardDraftDiscarded(ctx context.Context,
	companyID, actorUserID, awardChainID, awardDraftID string,
	revision int64, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID, EventTypeAwardDraftDiscarded,
		SubjectTypeAwardDraft, awardDraftID, strconv.FormatInt(revision, 10),
		occurredAt, map[string]any{"awardChainId": awardChainID, "revision": revision}))
}

func (s *Service) RecordAwardFinalised(ctx context.Context,
	companyID, actorUserID, awardChainID, awardRevisionID,
	finalisationOperationID string, revisionNumber int, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID, EventTypeAwardFinalised,
		SubjectTypeAwardRevision, awardRevisionID, finalisationOperationID,
		occurredAt, map[string]any{
			"awardChainId": awardChainID, "revisionNumber": revisionNumber,
		}))
}

func (s *Service) RecordAwardCorrected(ctx context.Context,
	companyID, actorUserID, awardChainID, awardRevisionID,
	supersededRevisionID, finalisationOperationID string,
	revisionNumber int, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID, EventTypeAwardCorrected,
		SubjectTypeAwardRevision, awardRevisionID, finalisationOperationID,
		occurredAt, map[string]any{
			"awardChainId": awardChainID, "supersededRevisionId": supersededRevisionID,
			"revisionNumber": revisionNumber,
		}))
}

func (s *Service) RecordAwardOutcomeGenerated(ctx context.Context,
	companyID, actorUserID, awardChainID, awardRevisionID, supplierID,
	invitationID, outcomeID, result string, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID,
		EventTypeAwardOutcomeGenerated, SubjectTypeAwardOutcome, outcomeID,
		awardRevisionID, occurredAt, map[string]any{
			"awardChainId": awardChainID, "awardRevisionId": awardRevisionID,
			"supplierId": supplierID, "invitationId": invitationID, "result": result,
		}))
}

func (s *Service) recordAwardDelivery(ctx context.Context, companyID, actorUserID,
	eventType, awardRevisionID, outcomeID, supplierID, deliveryID,
	dedupeIdentity string, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID, eventType,
		SubjectTypeAwardDelivery, deliveryID, dedupeIdentity, occurredAt,
		map[string]any{"awardRevisionId": awardRevisionID, "outcomeId": outcomeID,
			"supplierId": supplierID}))
}

func (s *Service) RecordAwardOutcomeNotified(ctx context.Context,
	companyID, actorUserID, awardRevisionID, outcomeID, supplierID,
	deliveryID, deliveryOperationID string, occurredAt time.Time) error {
	return s.recordAwardDelivery(ctx, companyID, actorUserID,
		EventTypeAwardOutcomeNotified, awardRevisionID, outcomeID, supplierID,
		deliveryID, deliveryOperationID, occurredAt)
}

func (s *Service) RecordAwardOutcomeNotificationRetried(ctx context.Context,
	companyID, actorUserID, awardRevisionID, outcomeID, supplierID,
	deliveryID, deliveryOperationID string, occurredAt time.Time) error {
	return s.recordAwardDelivery(ctx, companyID, actorUserID,
		EventTypeAwardOutcomeNotificationRetried, awardRevisionID, outcomeID, supplierID,
		deliveryID, deliveryOperationID, occurredAt)
}

func (s *Service) RecordAwardOutcomeNotificationObsoleted(ctx context.Context,
	companyID, actorUserID, awardRevisionID, outcomeID, supplierID,
	deliveryID string, occurredAt time.Time) error {
	return s.recordAwardDelivery(ctx, companyID, actorUserID,
		EventTypeAwardOutcomeNotificationObsoleted, awardRevisionID, outcomeID,
		supplierID, deliveryID, "obsoleted", occurredAt)
}

func (s *Service) RecordAwardOutcomeAcknowledged(ctx context.Context,
	companyID, outcomeID, supplierID, invitationID, sessionID string,
	occurredAt time.Time) error {
	return s.record(ctx, Event{CompanyID: companyID,
		EventType:   EventTypeAwardOutcomeAcknowledged,
		SubjectType: SubjectTypeAwardOutcome, SubjectID: outcomeID,
		ActorType: ActorTypeSupplier, ActorID: supplierID,
		DedupeIdentity: "acknowledged", CreatedAt: occurredAt,
		Metadata: map[string]any{"supplierId": supplierID,
			"invitationId": invitationID, "sessionId": sessionID},
	})
}

func (s *Service) RecordAwardReconciled(ctx context.Context,
	companyID, actorUserID, awardChainID, awardRevisionID string,
	revisionNumber int, occurredAt time.Time) error {
	return s.record(ctx, awardEvent(companyID, actorUserID, EventTypeAwardReconciled,
		SubjectTypeAwardRevision, awardRevisionID, strconv.Itoa(revisionNumber),
		occurredAt, map[string]any{
			"awardChainId": awardChainID, "revisionNumber": revisionNumber,
		}))
}
