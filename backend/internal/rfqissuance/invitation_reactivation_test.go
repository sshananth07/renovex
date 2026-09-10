package rfqissuance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Reactivation starts a NEW access generation; it never removes the reason an
// old link stopped working. These service tests prove orchestration over fakes.
// The one-winner conditional write is proved separately against real MongoDB.

func TestInvitationExpiryMutationsRejectBeyond180Days(t *testing.T) {
	late := time.Now().Add(180*24*time.Hour + time.Hour)
	t.Run("expiry update", func(t *testing.T) {
		svc, rig, invitation := advancementRig(t)
		current := *rig.invitations.stored[invitation.ID]
		if _, err := svc.UpdateInvitationExpiry(context.Background(), "company-1", "user-1",
			invitation.ID, current.Revision, late); !errors.Is(err, rfqissuance.ErrInvalidBusinessDate) {
			t.Errorf("expiry update error = %v, want ErrInvalidBusinessDate", err)
		}
	})
	t.Run("reactivation", func(t *testing.T) {
		svc, rig, invitation := advancementRig(t)
		current := *rig.invitations.stored[invitation.ID]
		revoked, err := svc.RevokeInvitation(context.Background(), "company-1", "user-1",
			invitation.ID, current.Revision)
		if err != nil {
			t.Fatalf("revoke setup: %v", err)
		}
		_, err = svc.ReactivateInvitation(context.Background(), "company-1", "user-1",
			rfqissuance.ReactivateInvitationInput{
				InvitationID: invitation.ID, ExpectedRevision: revoked.Revision,
				ExpiresAt: late, OperationID: "reactivate-too-late",
			})
		if !errors.Is(err, rfqissuance.ErrInvalidBusinessDate) {
			t.Errorf("reactivation error = %v, want ErrInvalidBusinessDate", err)
		}
	})
}

func TestReactivateRevokedInvitationMintsNewAccessAndCatchesUpToLatestVersion(
	t *testing.T,
) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()

	oldToken, err := rig.keyring.DeriveInvitationSecret(invitation.SecretKeyVersion,
		"company-1", invitation.ID, invitation.AccessGeneration)
	if err != nil {
		t.Fatalf("derive old token: %v", err)
	}

	current := *rig.invitations.stored[invitation.ID]
	revoked, err := svc.RevokeInvitation(ctx, "company-1", "user-1", invitation.ID,
		current.Revision)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// Revoked invitations deliberately do not advance during issuance.
	latest := issueAmendment(t, svc, "op-amend-after-revoke")
	if got := rig.invitations.stored[invitation.ID].CurrentIssuedRFQVersionID; got == latest.ID {
		t.Fatal("setup failed: the revoked invitation unexpectedly advanced")
	}

	// Configure a newer active key after the old link was minted. Reactivation
	// must adopt it rather than restoring the historical key version.
	newKeyring, err := secrets.NewInvitationKeyring(2, map[int]string{
		1: encodedTestKey(1),
		2: encodedTestKey(2),
	})
	if err != nil {
		t.Fatalf("build rotated keyring: %v", err)
	}
	rig.keyring = newKeyring
	svc = rig.rebuild(t)

	beforeAttempts := len(rig.deliveries.stored)
	beforeMail := len(rig.mailer.sent)
	newExpiry := time.Now().Add(45 * 24 * time.Hour)

	reactivated, err := svc.ReactivateInvitation(ctx, "company-1", "user-1",
		rfqissuance.ReactivateInvitationInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: revoked.Revision,
			ExpiresAt:        newExpiry,
			OperationID:      "op-reactivate-1",
		})
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}

	if reactivated.Status != rfqissuance.InvitationStatusActive {
		t.Errorf("Status = %q, want active", reactivated.Status)
	}
	if reactivated.RevokedAt != nil {
		t.Error("RevokedAt was not cleared even though the invitation is active again")
	}
	if !reactivated.ExpiresAt.Equal(newExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", reactivated.ExpiresAt, newExpiry)
	}
	if reactivated.AccessGeneration != invitation.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want exactly one increment from %d",
			reactivated.AccessGeneration, invitation.AccessGeneration)
	}
	if reactivated.SecretKeyVersion != newKeyring.ActiveVersion() {
		t.Errorf("SecretKeyVersion = %d, want active version %d",
			reactivated.SecretKeyVersion, newKeyring.ActiveVersion())
	}
	if reactivated.CurrentIssuedRFQVersionID != latest.ID {
		t.Errorf("CurrentIssuedRFQVersionID = %q, want latest issued version %q",
			reactivated.CurrentIssuedRFQVersionID, latest.ID)
	}
	if reactivated.Revision != revoked.Revision+1 {
		t.Errorf("Revision = %d, want %d", reactivated.Revision, revoked.Revision+1)
	}
	if !reactivated.PermitsAccess(time.Now()) {
		t.Error("the newly active, unexpired invitation does not permit access")
	}

	newToken, err := newKeyring.DeriveInvitationSecret(reactivated.SecretKeyVersion,
		"company-1", invitation.ID, reactivated.AccessGeneration)
	if err != nil {
		t.Fatalf("derive new token: %v", err)
	}
	if secrets.VerifyInvitationSecret(oldToken, reactivated.AccessSecretHash) {
		t.Error("the old token verifies after reactivation; old links must stay invalid forever")
	}
	if !secrets.VerifyInvitationSecret(newToken, reactivated.AccessSecretHash) {
		t.Error("the new-generation token does not verify against the persisted hash")
	}

	// Reactivation changes access only. Delivery remains an explicit action.
	if len(rig.mailer.sent) != beforeMail {
		t.Error("reactivation automatically sent email")
	}
	if len(rig.deliveries.stored) != beforeAttempts {
		t.Error("reactivation automatically created a delivery attempt")
	}
}

func TestReactivateEffectivelyExpiredInvitationMintsOneNewGeneration(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1",
		createInvitationInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stored := rig.invitations.stored[invitation.ID]
	stored.Status = rfqissuance.InvitationStatusActive
	stored.ExpiresAt = time.Now().Add(-time.Minute)

	future := time.Now().Add(7 * 24 * time.Hour)
	reactivated, err := svc.ReactivateInvitation(ctx, "company-1", "user-1",
		rfqissuance.ReactivateInvitationInput{
			InvitationID: invitation.ID, ExpectedRevision: stored.Revision,
			ExpiresAt: future, OperationID: "op-reactivate-expired",
		})
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}

	if reactivated.AccessGeneration != invitation.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want %d",
			reactivated.AccessGeneration, invitation.AccessGeneration+1)
	}
	if !reactivated.ExpiresAt.Equal(future) || !reactivated.PermitsAccess(time.Now()) {
		t.Error("expired invitation did not receive a usable future access window")
	}
}

func TestReactivateInvitationIsIdempotentForTheSameOperationID(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()
	current := *rig.invitations.stored[invitation.ID]

	revoked, err := svc.RevokeInvitation(ctx, "company-1", "user-1", invitation.ID,
		current.Revision)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	input := rfqissuance.ReactivateInvitationInput{
		InvitationID: invitation.ID, ExpectedRevision: revoked.Revision,
		ExpiresAt: time.Now().Add(21 * 24 * time.Hour), OperationID: "op-reactivate-same",
	}
	first, err := svc.ReactivateInvitation(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("first reactivation: %v", err)
	}
	second, err := svc.ReactivateInvitation(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}

	if second.AccessGeneration != first.AccessGeneration ||
		second.Revision != first.Revision ||
		second.AccessSecretHash != first.AccessSecretHash {
		t.Errorf("retry result differs: first=%+v second=%+v", first, second)
	}
}

func TestReactivateInvitationRecordsOnePrimitiveAuditEventAfterTheWinningWrite(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()
	current := *rig.invitations.stored[invitation.ID]

	revoked, err := svc.RevokeInvitation(ctx, "company-1", "user-1", invitation.ID,
		current.Revision)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	audit := &recordingIssuanceAudit{}
	svc = rig.rebuild(t, rfqissuance.WithAuditRecorder(audit))
	input := rfqissuance.ReactivateInvitationInput{
		InvitationID: invitation.ID, ExpectedRevision: revoked.Revision,
		ExpiresAt:   time.Now().Add(21 * 24 * time.Hour),
		OperationID: "op-reactivate-audit",
	}

	reactivated, err := svc.ReactivateInvitation(ctx, "company-1", "user-2", input)
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if _, err := svc.ReactivateInvitation(ctx, "company-1", "user-2", input); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}

	if len(audit.invitationReactivated) != 1 {
		t.Fatalf("recorded %d reactivation events, want exactly the winning write's 1",
			len(audit.invitationReactivated))
	}
	call := audit.invitationReactivated[0]
	if call.companyID != "company-1" || call.actorUserID != "user-2" ||
		call.rfqChainID != invitation.RFQChainID ||
		call.invitationID != invitation.ID ||
		call.accessGeneration != reactivated.AccessGeneration {
		t.Errorf("audit primitives = %+v, want the winning transition's stable identity", call)
	}
}

func TestReactivateInvitationRejectsActiveUnexpiredStateWithoutMutation(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()
	before := *rig.invitations.stored[invitation.ID]

	_, err := svc.ReactivateInvitation(ctx, "company-1", "user-1",
		rfqissuance.ReactivateInvitationInput{
			InvitationID: invitation.ID, ExpectedRevision: before.Revision,
			ExpiresAt:   time.Now().Add(60 * 24 * time.Hour),
			OperationID: "op-must-not-rotate",
		})

	if !errors.Is(err, rfqissuance.ErrInvitationAlreadyActive) {
		t.Errorf("error = %v, want ErrInvitationAlreadyActive", err)
	}
	after := *rig.invitations.stored[invitation.ID]
	if after != before {
		t.Errorf("active invitation mutated: before=%+v after=%+v", before, after)
	}
}

func TestReactivateInvitationObsoletesOnlyOlderPendingAttempts(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()
	current := *rig.invitations.stored[invitation.ID]

	revoked, err := svc.RevokeInvitation(ctx, "company-1", "user-1", invitation.ID,
		current.Revision)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}

	seed := func(operationID string, status rfqissuance.DeliveryStatus) {
		t.Helper()
		attempt, createErr := rig.deliveries.CreateAttempt(ctx,
			rfqissuance.NewDeliveryAttempt(rfqissuance.NewDeliveryAttemptInput{
				CompanyID: "company-1", InvitationID: invitation.ID,
				DeliveryOperationID: operationID,
				AccessGeneration:    invitation.AccessGeneration,
				Channel:             rfqissuance.DeliveryChannelEmail,
				RecipientEmail:      invitation.RecipientEmail,
				IssuedRFQVersionID:  invitation.CurrentIssuedRFQVersionID,
				RequestedByUserID:   "user-1",
			}))
		if createErr != nil {
			t.Fatalf("seed %s: %v", operationID, createErr)
		}
		if status != rfqissuance.DeliveryStatusPending {
			if updateErr := rig.deliveries.UpdateAttemptStatus(ctx, "company-1",
				attempt.ID, status, "", nil); updateErr != nil {
				t.Fatalf("set %s status: %v", operationID, updateErr)
			}
		}
	}

	seed("pending-old", rfqissuance.DeliveryStatusPending)
	seed("sent-old", rfqissuance.DeliveryStatusSent)
	seed("failed-old", rfqissuance.DeliveryStatusFailed)
	seed("obsolete-old", rfqissuance.DeliveryStatusObsolete)

	_, err = svc.ReactivateInvitation(ctx, "company-1", "user-1",
		rfqissuance.ReactivateInvitationInput{
			InvitationID: invitation.ID, ExpectedRevision: revoked.Revision,
			ExpiresAt:   time.Now().Add(14 * 24 * time.Hour),
			OperationID: "op-reactivate-history",
		})
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}

	statuses := map[string]rfqissuance.DeliveryStatus{}
	for _, attempt := range rig.deliveries.stored {
		statuses[attempt.DeliveryOperationID] = attempt.Status
	}
	if statuses["pending-old"] != rfqissuance.DeliveryStatusObsolete {
		t.Errorf("pending old generation = %q, want obsolete", statuses["pending-old"])
	}
	for operationID, want := range map[string]rfqissuance.DeliveryStatus{
		"sent-old":     rfqissuance.DeliveryStatusSent,
		"failed-old":   rfqissuance.DeliveryStatusFailed,
		"obsolete-old": rfqissuance.DeliveryStatusObsolete,
	} {
		if statuses[operationID] != want {
			t.Errorf("%s status = %q, want preserved %q",
				operationID, statuses[operationID], want)
		}
	}
}
