package audit

import (
	"context"
	"strconv"
	"time"
)

func supplierOfferEvent(companyID, supplierID, eventType, subjectType,
	subjectID, dedupeIdentity string, occurredAt time.Time,
	metadata map[string]any) Event {
	actorType, actorID := ActorTypeSupplier, supplierID
	if supplierID == "" {
		// Privileged reconciliation can reproduce a durable claim without the
		// original Supplier directory identity. Record that recovery as system
		// work rather than inventing or re-resolving a Supplier actor.
		actorType, actorID = ActorTypeSystem, ""
	}
	return Event{CompanyID: companyID, EventType: eventType,
		SubjectType: subjectType, SubjectID: subjectID,
		ActorType: actorType, ActorID: actorID,
		Metadata: metadata, DedupeIdentity: dedupeIdentity, CreatedAt: occurredAt}
}

func (s *Service) RecordOfferDraftCreated(ctx context.Context,
	companyID, supplierID, invitationID, offerChainID, draftID string,
	revision int64, occurredAt time.Time) error {
	return s.record(ctx, supplierOfferEvent(companyID, supplierID,
		EventTypeOfferDraftCreated, SubjectTypeOfferDraft, draftID,
		strconv.FormatInt(revision, 10), occurredAt, map[string]any{
			"invitationId": invitationID, "offerChainId": offerChainID,
			"revision": revision,
		}))
}

func (s *Service) RecordOfferDraftCopied(ctx context.Context,
	companyID, supplierID, invitationID, offerChainID, draftID,
	sourceOfferVersionID string, revision int64, occurredAt time.Time) error {
	return s.record(ctx, supplierOfferEvent(companyID, supplierID,
		EventTypeOfferDraftCopied, SubjectTypeOfferDraft, draftID,
		strconv.FormatInt(revision, 10), occurredAt, map[string]any{
			"invitationId": invitationID, "offerChainId": offerChainID,
			"sourceOfferVersionId": sourceOfferVersionID, "revision": revision,
		}))
}

func (s *Service) RecordOfferDraftUpdated(ctx context.Context,
	companyID, supplierID, invitationID, offerChainID, draftID string,
	revision int64, occurredAt time.Time) error {
	return s.record(ctx, supplierOfferEvent(companyID, supplierID,
		EventTypeOfferDraftUpdated, SubjectTypeOfferDraft, draftID,
		strconv.FormatInt(revision, 10), occurredAt, map[string]any{
			"invitationId": invitationID, "offerChainId": offerChainID,
			"revision": revision,
		}))
}

func (s *Service) RecordOfferDraftArchived(ctx context.Context,
	companyID, supplierID, invitationID, offerChainID, draftID string,
	revision int64, occurredAt time.Time) error {
	return s.record(ctx, supplierOfferEvent(companyID, supplierID,
		EventTypeOfferDraftArchived, SubjectTypeOfferDraft, draftID,
		strconv.FormatInt(revision, 10), occurredAt, map[string]any{
			"invitationId": invitationID, "offerChainId": offerChainID,
			"revision": revision,
		}))
}

func (s *Service) RecordOfferSubmitted(ctx context.Context,
	companyID, supplierID, invitationID, offerChainID, draftID,
	offerVersionID string, versionNumber int, occurredAt time.Time) error {
	return s.record(ctx, supplierOfferEvent(companyID, supplierID,
		EventTypeOfferSubmitted, SubjectTypeOfferVersion, offerVersionID,
		draftID, occurredAt, map[string]any{
			"invitationId": invitationID, "offerChainId": offerChainID,
			"draftId": draftID, "versionNumber": versionNumber,
		}))
}

func (s *Service) RecordOfferSubmissionReconciled(ctx context.Context,
	companyID, supplierID, invitationID, offerChainID, offerVersionID string,
	versionNumber int, occurredAt time.Time) error {
	return s.record(ctx, supplierOfferEvent(companyID, supplierID,
		EventTypeOfferSubmissionReconciled, SubjectTypeOfferVersion, offerVersionID,
		strconv.Itoa(versionNumber), occurredAt, map[string]any{
			"invitationId": invitationID, "offerChainId": offerChainID,
			"versionNumber": versionNumber,
		}))
}

func (s *Service) RecordOfferWithdrawn(ctx context.Context,
	companyID, supplierID, invitationID, offerChainID, offerVersionID,
	withdrawalID string, occurredAt time.Time) error {
	return s.record(ctx, supplierOfferEvent(companyID, supplierID,
		EventTypeOfferWithdrawn, SubjectTypeOfferWithdrawal, withdrawalID,
		offerVersionID, occurredAt, map[string]any{
			"invitationId": invitationID, "offerChainId": offerChainID,
			"offerVersionId": offerVersionID,
		}))
}
