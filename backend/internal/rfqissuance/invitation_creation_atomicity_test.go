package rfqissuance_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Invitation creation must be a SINGLE atomic insert (design spec §5.1, §6.1A).
//
// Creation and rotation are different business operations and must not share a
// mechanism. A create-then-rotate sequence leaves a real failure window:
//
//	invitation inserted
//	→ process crashes, or the rotate write fails
//	→ an invitation exists with no reproducible link, and looks healthy
//
// The correction is for the service to mint the invitation ID itself, derive
// the secret for the INITIAL generation, and insert the hash, generation and
// key version together in one write.

// TestCreateInvitationRequiresNoSecondRepositoryMutation is the guard against
// that window. A store that refuses every post-insert mutation must still yield
// a fully usable invitation.
func TestCreateInvitationRequiresNoSecondRepositoryMutation(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	// From here on, ANY further write to the invitation fails — simulating a
	// crash or an infrastructure failure immediately after the insert.
	rig.invitations.failAfterCreate = true

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("creation must complete in ONE write, got %v", err)
	}

	if invitation.AccessSecretHash == "" {
		t.Fatal("the invitation was created without its secret hash: a crash here would " +
			"leave a record with no reproducible link (§5.1)")
	}
	if invitation.AccessGeneration < 1 {
		t.Errorf("AccessGeneration = %d, want the initial generation to be set on insert",
			invitation.AccessGeneration)
	}
	if invitation.SecretKeyVersion != rig.keyring.ActiveVersion() {
		t.Errorf("SecretKeyVersion = %d, want the active version %d",
			invitation.SecretKeyVersion, rig.keyring.ActiveVersion())
	}

	// The persisted record must be complete and self-consistent.
	stored := rig.invitations.stored[invitation.ID]
	if stored == nil {
		t.Fatal("the invitation was not stored")
	}
	if stored.AccessSecretHash == "" {
		t.Fatal("the STORED invitation carries no secret hash")
	}

	// And the link must be derivable from what was persisted, with no repair.
	raw, err := rig.keyring.DeriveInvitationSecret(stored.SecretKeyVersion,
		"company-1", stored.ID, stored.AccessGeneration)
	if err != nil {
		t.Fatalf("deriving from the persisted state: %v", err)
	}
	if !secrets.VerifyInvitationSecret(raw, stored.AccessSecretHash) {
		t.Error("the token derived from the PERSISTED key version and generation does not " +
			"verify against the PERSISTED hash; the invitation is unusable and only a " +
			"second write could repair it")
	}
}

// The service must mint the ID, because the ID is part of the derivation input.
// A repository-assigned ID would make the secret underivable until after the
// insert — which is precisely what forces a second write.
func TestCreateInvitationUsesAServiceGeneratedID(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invitation.ID == "" {
		t.Fatal("the invitation must carry an ID")
	}
	if rig.invitations.assignedID {
		t.Error("the repository assigned the ID. The service must mint it, because the ID " +
			"is part of the HMAC derivation input and the secret must exist before the " +
			"insert (§6.1A)")
	}
}

// Copy-link must work immediately after creation, with no intervening write.
func TestCopyLinkWorksImmediatelyAfterCreationWithNoFurtherWrites(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rig.invitations.failAfterCreate = true

	link, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("copy-link must work from the created state alone, got %v", err)
	}
	if link.URL == "" || link.Token == "" {
		t.Error("copy-link returned an empty link")
	}
}

// The initial generation is 1. Rotation is what advances it, so a freshly
// created invitation sitting at a higher generation would misreport how many
// times its link has been invalidated.
func TestCreateInvitationStartsAtGenerationOne(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invitation.AccessGeneration != 1 {
		t.Errorf("AccessGeneration = %d, want 1. Creation is not a rotation: a new "+
			"invitation has never had its link invalidated", invitation.AccessGeneration)
	}
}

// Rotation remains a distinct operation on an EXISTING invitation, and still
// advances the generation from whatever it currently is.
func TestRotateSecretAdvancesFromTheCurrentGeneration(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	before, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := svc.RotateInvitationSecret(ctx, "company-1", "user-1", invitation.ID,
		invitation.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rotated.AccessGeneration != invitation.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want %d", rotated.AccessGeneration,
			invitation.AccessGeneration+1)
	}

	after, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if after.URL == before.URL {
		t.Error("rotation did not change the link")
	}
	// The pre-rotation token must no longer verify.
	if secrets.VerifyInvitationSecret(before.Token,
		rig.invitations.stored[invitation.ID].AccessSecretHash) {
		t.Error("the pre-rotation token still verifies against the current hash")
	}
}

var _ = rfqissuance.InvitationStatusDraft
