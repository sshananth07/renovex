package audit

import (
	"context"
	"time"
)

func supplierAccessEvent(companyID, supplierID, eventType, subjectType,
	subjectID string, metadata map[string]any) Event {

	return Event{
		CompanyID: companyID, EventType: eventType,
		SubjectType: subjectType, SubjectID: subjectID,
		ActorType: ActorTypeSupplier, ActorID: supplierID,
		Metadata: metadata,
	}
}

func (s *Service) RecordChallengeRequested(ctx context.Context,
	companyID, supplierID, invitationID, challengeID string,
	accessGeneration int64, requestedAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierChallengeRequested,
		SubjectTypeSupplierInvitation, invitationID, map[string]any{
			"challengeId": challengeID, "accessGeneration": accessGeneration,
			"requestedAt": requestedAt,
		}))
}

func (s *Service) RecordChallengeDeliveryRetried(ctx context.Context,
	companyID, supplierID, invitationID, challengeID, deliveryAttemptID string,
	accessGeneration int64, retriedAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierChallengeDeliveryRetried,
		SubjectTypeSupplierInvitation, invitationID, map[string]any{
			"challengeId": challengeID, "deliveryAttemptId": deliveryAttemptID,
			"accessGeneration": accessGeneration, "retriedAt": retriedAt,
		}))
}

func (s *Service) RecordVerificationFailed(ctx context.Context,
	companyID, supplierID, invitationID, challengeID string,
	accessGeneration int64, reason string, failedAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierVerificationFailed,
		SubjectTypeSupplierInvitation, invitationID, map[string]any{
			"challengeId": challengeID, "accessGeneration": accessGeneration,
			"reason": reason, "failedAt": failedAt,
		}))
}

func (s *Service) RecordVerificationSucceeded(ctx context.Context,
	companyID, supplierID, invitationID, challengeID, sessionID string,
	accessGeneration int64, verifiedAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierVerificationSucceeded,
		SubjectTypeSupplierInvitation, invitationID, map[string]any{
			"challengeId": challengeID, "sessionId": sessionID,
			"accessGeneration": accessGeneration, "verifiedAt": verifiedAt,
		}))
}

func (s *Service) RecordSessionCreated(ctx context.Context,
	companyID, supplierID, invitationID, challengeID, sessionID string,
	accessGeneration, tokenGeneration int64, createdAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierSessionCreated,
		SubjectTypeSupplierSession, sessionID, map[string]any{
			"invitationId": invitationID, "challengeId": challengeID,
			"accessGeneration": accessGeneration,
			"tokenGeneration":  tokenGeneration, "createdAt": createdAt,
		}))
}

func (s *Service) RecordSessionReverified(ctx context.Context,
	companyID, supplierID, invitationID, challengeID, sessionID string,
	accessGeneration, tokenGeneration int64, reverifiedAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierSessionReverified,
		SubjectTypeSupplierSession, sessionID, map[string]any{
			"invitationId": invitationID, "challengeId": challengeID,
			"accessGeneration": accessGeneration,
			"tokenGeneration":  tokenGeneration, "reverifiedAt": reverifiedAt,
		}))
}

func (s *Service) RecordSessionRenewed(ctx context.Context,
	companyID, supplierID, sessionID string, tokenGeneration int64,
	slidingExpiresAt, renewedAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierSessionRenewed,
		SubjectTypeSupplierSession, sessionID, map[string]any{
			"tokenGeneration":  tokenGeneration,
			"slidingExpiresAt": slidingExpiresAt, "renewedAt": renewedAt,
		}))
}

func (s *Service) RecordSessionRevoked(ctx context.Context,
	companyID, supplierID, sessionID string, tokenGeneration int64,
	revokedAt time.Time) error {

	return s.record(ctx, supplierAccessEvent(
		companyID, supplierID, EventTypeSupplierSessionRevoked,
		SubjectTypeSupplierSession, sessionID, map[string]any{
			"tokenGeneration": tokenGeneration, "revokedAt": revokedAt,
		}))
}
