package rfqissuance_test

import "context"

// The invitation half of recordingIssuanceAudit (design spec §15).
//
// Kept in its own file so the Phase B recorder in issue_test.go stays readable.
// These record the CALLS rather than asserting inside the fake, so a test can
// state what it expects rather than the fake deciding for it.

type invitationAuditCall struct {
	companyID        string
	actorUserID      string
	rfqChainID       string
	invitationID     string
	supplierID       string
	attemptID        string
	channel          string
	failureCode      string
	accessGeneration int64
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationCreated(
	_ context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, invitationID,
	supplierID string,
) error {
	a.invitationCreated = append(a.invitationCreated, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID, supplierID: supplierID,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationSent(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID, attemptID, channel string,
) error {
	a.invitationSent = append(a.invitationSent, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID, attemptID: attemptID, channel: channel,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationDeliveryFailed(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID, attemptID, failureCode string,
) error {
	a.invitationDeliveryFailed = append(a.invitationDeliveryFailed, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID, attemptID: attemptID, failureCode: failureCode,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationLinkCopied(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
	accessGeneration int64,
) error {
	a.invitationLinkCopied = append(a.invitationLinkCopied, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID, accessGeneration: accessGeneration,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationRecipientReplaced(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
	accessGeneration int64,
) error {
	a.invitationRecipientReplaced = append(a.invitationRecipientReplaced, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID, accessGeneration: accessGeneration,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationSecretRotated(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
	accessGeneration int64,
) error {
	a.invitationSecretRotated = append(a.invitationSecretRotated, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID, accessGeneration: accessGeneration,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationRevoked(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
) error {
	a.invitationRevoked = append(a.invitationRevoked, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationReactivated(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID, issuedVersionID string,
	accessGeneration int64,
) error {
	a.invitationReactivated = append(a.invitationReactivated, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID, attemptID: issuedVersionID,
		accessGeneration: accessGeneration,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationExpiryChanged(
	_ context.Context,
	companyID, actorUserID, rfqChainID, invitationID string,
) error {
	a.invitationExpiryChanged = append(a.invitationExpiryChanged, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: invitationID,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordSupplierInvitationAdvanced(
	_ context.Context,
	companyID, actorUserID, rfqChainID, versionID string, invitationCount int64,
) error {
	a.invitationAdvanced = append(a.invitationAdvanced, invitationAuditCall{
		companyID: companyID, actorUserID: actorUserID, rfqChainID: rfqChainID,
		invitationID: versionID, accessGeneration: invitationCount,
	})
	return nil
}
