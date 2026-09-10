package rfqissuance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Invitation advancement on amendment issuance (design spec §4.2 step 6-7,
// §10.7).
//
// The guarantee: an invitation ALWAYS points at a complete issued version.
// If advancement is interrupted, an invitation lagging on version N keeps
// exposing the whole of version N — never a partially created N+1 — and
// reconciliation completes the move later.

// advancementRig builds a service with an issued V1 and one active invitation.
func advancementRig(t *testing.T) (*rfqissuance.Service, *invitationTestRig,
	rfqissuance.SupplierInvitation) {
	t.Helper()

	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("setup create invitation: %v", err)
	}
	if _, err := svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{
			InvitationID: invitation.ID, OperationID: "op-send-1",
		}); err != nil {
		t.Fatalf("setup send: %v", err)
	}
	return svc, rig, invitation
}

// issueAmendment drives a full amendment cycle and returns the new version.
func issueAmendment(t *testing.T, svc *rfqissuance.Service, operationID string) rfqissuance.IssuedRFQVersion {
	t.Helper()
	ctx := context.Background()

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("creating amendment draft: %v", err)
	}
	version, err := svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: draft.Revision,
			OperationID: operationID,
		})
	if err != nil {
		t.Fatalf("issuing amendment: %v", err)
	}
	return version
}

// Issuing a new version advances every non-revoked invitation to it (§4.2).
func TestIssuingAnAmendmentAdvancesActiveInvitations(t *testing.T) {
	svc, rig, invitation := advancementRig(t)

	second := issueAmendment(t, svc, "op-amend-1")

	advanced := rig.invitations.stored[invitation.ID]
	if advanced.CurrentIssuedRFQVersionID != second.ID {
		t.Errorf("the invitation points at %q, want the newly issued %q. An active "+
			"Supplier must be moved to the current version (§4.2 step 6)",
			advanced.CurrentIssuedRFQVersionID, second.ID)
	}
}

// A REVOKED invitation is left where it was: revocation preserves the record of
// what that Supplier was actually invited to (§5.4).
func TestIssuingAnAmendmentDoesNotAdvanceRevokedInvitations(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()

	current := rig.invitations.stored[invitation.ID]
	firstVersionID := current.CurrentIssuedRFQVersionID

	if _, err := svc.RevokeInvitation(ctx, "company-1", "user-1", invitation.ID,
		current.Revision); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	issueAmendment(t, svc, "op-amend-1")

	after := rig.invitations.stored[invitation.ID]
	if after.CurrentIssuedRFQVersionID != firstVersionID {
		t.Errorf("a revoked invitation advanced to %q; it must remain on %q (§5.4)",
			after.CurrentIssuedRFQVersionID, firstVersionID)
	}
}

// Advancement must never point an invitation at a version that does not exist.
// The version is created BEFORE any invitation moves, so an interruption leaves
// invitations on a complete earlier version rather than a phantom one.
func TestAdvancementNeverPointsAtANonExistentVersion(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()

	second := issueAmendment(t, svc, "op-amend-1")

	pointer := rig.invitations.stored[invitation.ID].CurrentIssuedRFQVersionID
	if _, err := svc.GetIssuedVersion(ctx, "company-1", pointer); err != nil {
		t.Errorf("the invitation points at %q, which cannot be read: %v", pointer, err)
	}
	if pointer != second.ID {
		t.Errorf("pointer = %q, want %q", pointer, second.ID)
	}
}

// A FAILED advancement must not fail the issuance. The immutable version
// already exists and is authoritative; a lagging invitation is a reconcilable
// gap (§4.2 step 7).
func TestFailedAdvancementDoesNotFailTheIssuance(t *testing.T) {
	svc, rig, invitation := advancementRig(t)

	rig.invitations.failAdvance = true

	// The issuance must still succeed.
	second := issueAmendment(t, svc, "op-amend-1")

	lagging := rig.invitations.stored[invitation.ID]
	if lagging.CurrentIssuedRFQVersionID == second.ID {
		t.Fatal("the advancement did not actually fail; the test proves nothing")
	}

	// And the lagging invitation still exposes a COMPLETE earlier version.
	if _, err := svc.GetIssuedVersion(context.Background(), "company-1",
		lagging.CurrentIssuedRFQVersionID); err != nil {
		t.Errorf("the lagging invitation points at an unreadable version: %v", err)
	}
}

// Reconciliation completes an interrupted advancement (§10.7).
func TestReconcileInvitationsAdvancesLaggingInvitations(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()

	rig.invitations.failAdvance = true
	second := issueAmendment(t, svc, "op-amend-1")
	rig.invitations.failAdvance = false

	advanced, err := svc.ReconcileInvitationAdvancement(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if advanced != 1 {
		t.Errorf("reconciled %d invitations, want 1", advanced)
	}

	after := rig.invitations.stored[invitation.ID]
	if after.CurrentIssuedRFQVersionID != second.ID {
		t.Errorf("after reconciliation the invitation points at %q, want %q",
			after.CurrentIssuedRFQVersionID, second.ID)
	}
}

// Reconciliation is idempotent: running it on an aligned chain changes nothing.
func TestReconcileInvitationsIsIdempotent(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()

	second := issueAmendment(t, svc, "op-amend-1")

	if _, err := svc.ReconcileInvitationAdvancement(ctx, "company-1", "user-1",
		"chain-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after := rig.invitations.stored[invitation.ID]
	if after.CurrentIssuedRFQVersionID != second.ID {
		t.Errorf("pointer moved to %q on an aligned chain", after.CurrentIssuedRFQVersionID)
	}
}

// Reconciliation is tenant-scoped.
func TestReconcileInvitationsIsTenantScoped(t *testing.T) {
	svc, _, _ := advancementRig(t)

	_, err := svc.ReconcileInvitationAdvancement(context.Background(), "company-2",
		"user-1", "chain-1")

	if !errors.Is(err, rfqissuance.ErrIssuanceChainNotFound) {
		t.Errorf("error = %v, want ErrIssuanceChainNotFound for a foreign company", err)
	}
}

// --- delivery obsolescence (§3.5) ---

// A pending attempt for a SUPERSEDED access generation becomes obsolete: the
// link it was carrying no longer works, so reporting it as still pending would
// misdescribe what the Supplier can do.
func TestPendingAttemptsBecomeObsoleteAfterSecretRotation(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()

	// A genuinely PENDING attempt only arises from an interruption between the
	// intent insert and the status write — a failed send records `failed`, and
	// a successful one records `sent`. Model that crash directly.
	stuck := rfqissuance.NewDeliveryAttempt(rfqissuance.NewDeliveryAttemptInput{
		CompanyID: "company-1", InvitationID: invitation.ID,
		DeliveryOperationID: "op-send-stuck",
		AccessGeneration:    rig.invitations.stored[invitation.ID].AccessGeneration,
		Channel:             rfqissuance.DeliveryChannelEmail,
		RecipientEmail:      "sales@supplier.com",
		IssuedRFQVersionID:  "version-1",
		RequestedByUserID:   "user-1",
	})
	if _, err := rig.deliveries.CreateAttempt(ctx, stuck); err != nil {
		t.Fatalf("seeding a stuck attempt: %v", err)
	}

	current := rig.invitations.stored[invitation.ID]
	if _, err := svc.RotateInvitationSecret(ctx, "company-1", "user-1", invitation.ID,
		current.Revision); err != nil {
		t.Fatalf("rotating: %v", err)
	}

	obsoleted, err := svc.ObsoleteSupersededDeliveryAttempts(ctx, "company-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obsoleted == 0 {
		t.Fatal("no attempt was marked obsolete")
	}

	for _, attempt := range rig.deliveries.stored {
		if attempt.AccessGeneration < rig.invitations.stored[invitation.ID].AccessGeneration &&
			attempt.Status == rfqissuance.DeliveryStatusPending {
			t.Errorf("attempt %s is still pending on superseded generation %d; its link "+
				"no longer works (§3.5)", attempt.ID, attempt.AccessGeneration)
		}
	}
}

// A SENT attempt is historical fact and must not be rewritten.
func TestObsoletingDoesNotRewriteSentAttempts(t *testing.T) {
	svc, rig, invitation := advancementRig(t)
	ctx := context.Background()

	current := rig.invitations.stored[invitation.ID]
	if _, err := svc.RotateInvitationSecret(ctx, "company-1", "user-1", invitation.ID,
		current.Revision); err != nil {
		t.Fatalf("rotating: %v", err)
	}

	if _, err := svc.ObsoleteSupersededDeliveryAttempts(ctx, "company-1",
		invitation.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, attempt := range rig.deliveries.stored {
		if attempt.Status == rfqissuance.DeliveryStatusObsolete && attempt.SentAt != nil {
			t.Errorf("attempt %s was sent at %v but was marked obsolete; a completed "+
				"delivery is historical fact", attempt.ID, attempt.SentAt)
		}
	}
}

var _ = time.Now
