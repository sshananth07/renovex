package audit

import "context"

// Supplier Invitation audit events (M8 design spec §15).
//
// Every method takes primitives only. Deliberately absent from every signature:
// any parameter that could carry a raw invitation link, token, secret,
// verification code or session token. A caller physically cannot pass one, so
// the §15 forbidden-values rule is enforced by the type system rather than by
// remembering it at each call site.
//
// The recipient EMAIL is likewise absent: it is Supplier contact data, already
// stored on the invitation, and an audit trail does not need a second copy.

func (s *Service) RecordSupplierInvitationCreated(ctx context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, invitationID,
	supplierID string) error {

	return s.record(ctx, s.rfqEvent(
		companyID, projectID, actorUserID, EventTypeSupplierInvitationCreated,
		rfqChainID, rfqNumber,
		map[string]any{"invitationId": invitationID, "supplierId": supplierID},
	))
}

func (s *Service) RecordSupplierInvitationSent(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID, attemptID, channel string) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationSent,
		rfqChainID, "",
		map[string]any{
			"invitationId": invitationID, "attemptId": attemptID, "channel": channel,
		},
	))
}

// RecordSupplierInvitationDeliveryFailed records a BOUNDED failure code only.
// Raw provider error text never reaches this method (§1A.2).
func (s *Service) RecordSupplierInvitationDeliveryFailed(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID, attemptID, failureCode string) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationDeliveryFailed,
		rfqChainID, "",
		map[string]any{
			"invitationId": invitationID, "attemptId": attemptID,
			"failureCode": failureCode,
		},
	))
}

// RecordSupplierInvitationLinkCopied records THAT a link was copied and for
// which access generation — never the link or token itself (§15).
func (s *Service) RecordSupplierInvitationLinkCopied(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
	accessGeneration int64) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationLinkCopied,
		rfqChainID, "",
		map[string]any{
			"invitationId": invitationID, "accessGeneration": accessGeneration,
		},
	))
}

// RecordSupplierInvitationRecipientReplaced records the replacement and the new
// access generation. The recipient's name and address are deliberately absent.
func (s *Service) RecordSupplierInvitationRecipientReplaced(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
	accessGeneration int64) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationRecipientReplaced,
		rfqChainID, "",
		map[string]any{
			"invitationId": invitationID, "accessGeneration": accessGeneration,
		},
	))
}

func (s *Service) RecordSupplierInvitationSecretRotated(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
	accessGeneration int64) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationSecretRotated,
		rfqChainID, "",
		map[string]any{
			"invitationId": invitationID, "accessGeneration": accessGeneration,
		},
	))
}

func (s *Service) RecordSupplierInvitationRevoked(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID string) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationRevoked,
		rfqChainID, "",
		map[string]any{"invitationId": invitationID},
	))
}

// RecordSupplierInvitationReactivated records only stable identifiers and the
// new generation. The derived token and hash have no parameter through which
// they could enter the append-only audit stream.
func (s *Service) RecordSupplierInvitationReactivated(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID, issuedVersionID string,
	accessGeneration int64) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationReactivated,
		rfqChainID, "",
		map[string]any{
			"invitationId": invitationID, "versionId": issuedVersionID,
			"accessGeneration": accessGeneration,
		},
	))
}

func (s *Service) RecordSupplierInvitationExpiryChanged(ctx context.Context,
	companyID, actorUserID, rfqChainID, invitationID string) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationExpiryChanged,
		rfqChainID, "",
		map[string]any{"invitationId": invitationID},
	))
}

// RecordSupplierInvitationAdvanced records the bulk advance performed when a
// new version is issued (§4.2 step 6). The COUNT is recorded rather than the
// individual invitations: the per-invitation state is already queryable, and a
// list would grow unbounded on a large chain.
func (s *Service) RecordSupplierInvitationAdvanced(ctx context.Context,
	companyID, actorUserID, rfqChainID, versionID string, invitationCount int64) error {

	return s.record(ctx, s.rfqEvent(
		companyID, "", actorUserID, EventTypeSupplierInvitationAdvanced,
		rfqChainID, "",
		map[string]any{
			"versionId": versionID, "invitationCount": invitationCount,
		},
	))
}
