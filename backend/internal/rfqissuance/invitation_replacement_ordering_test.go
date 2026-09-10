package rfqissuance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// fakeOfferWorkspaceCoordinator records the ORDER of the offer-side calls so
// the tests can prove the claim happens before the authoritative invitation
// write, which is the entire point of the barrier (§5.3A).
type fakeOfferWorkspaceCoordinator struct {
	calls []string

	prepareErr  error
	completeErr error

	preparedPrevious  string
	preparedCandidate string
	completedFor      string

	// recipientAtPrepare captures what the invitation looked like when the
	// claim was taken, proving the claim precedes the invitation write.
	recipientAtPrepare string
	observeRecipient   func() string
}

func (f *fakeOfferWorkspaceCoordinator) PrepareRecipientReplacement(
	_ context.Context, input rfqissuance.RecipientReplacementPreparation) error {
	f.calls = append(f.calls, "prepare")
	f.preparedPrevious = input.PreviousRecipientIdentity
	f.preparedCandidate = input.CandidateRecipientIdentity
	if f.observeRecipient != nil {
		f.recipientAtPrepare = f.observeRecipient()
	}
	return f.prepareErr
}

func (f *fakeOfferWorkspaceCoordinator) CompleteRecipientReplacement(
	_ context.Context, input rfqissuance.RecipientReplacementCompletion) error {
	f.calls = append(f.calls, "complete")
	f.completedFor = input.ReplacementOperationID
	return f.completeErr
}

func (f *fakeOfferWorkspaceCoordinator) AbortRecipientReplacement(
	_ context.Context, _ rfqissuance.RecipientReplacementAbort) error {
	f.calls = append(f.calls, "abort")
	return nil
}

// The approved sequence is claim -> authoritative invitation replacement ->
// archival. Archiving first would release the uniqueness slot before the
// invitation write, reopening the race the barrier closes.
func TestReplaceRecipientClaimsTheOfferWorkspaceBeforeReplacing(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	coordinator := &fakeOfferWorkspaceCoordinator{
		observeRecipient: func() string {
			current, findErr := svc.GetInvitation(ctx, "company-1", invitation.ID)
			if findErr != nil {
				return ""
			}
			return current.RecipientEmailNormalized
		},
	}
	svc.SetOfferWorkspaceCoordinator(coordinator)

	if _, err := svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: invitation.Revision,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
			OperationID:      "op-replace-1",
		}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(coordinator.calls) != 2 ||
		coordinator.calls[0] != "prepare" || coordinator.calls[1] != "complete" {
		t.Fatalf("offer-side calls = %v, want [prepare complete]", coordinator.calls)
	}
	if coordinator.recipientAtPrepare != invitation.RecipientEmailNormalized {
		t.Errorf("recipient at claim time = %q, want the PREVIOUS recipient %q: "+
			"the claim must precede the authoritative invitation write",
			coordinator.recipientAtPrepare, invitation.RecipientEmailNormalized)
	}
	if coordinator.preparedPrevious != invitation.RecipientEmailNormalized ||
		coordinator.preparedCandidate != "procurement@supplier.com" {
		t.Errorf("claim identities = %q -> %q, want the exact pair",
			coordinator.preparedPrevious, coordinator.preparedCandidate)
	}
}

// If the offer-side claim cannot be taken, the invitation must NOT be replaced.
// The claim is a required precondition, not a best-effort cleanup.
func TestReplaceRecipientIsBlockedWhenTheOfferWorkspaceCannotBeClaimed(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	coordinator := &fakeOfferWorkspaceCoordinator{
		prepareErr: rfqissuance.ErrOfferWorkspaceConflict,
	}
	svc.SetOfferWorkspaceCoordinator(coordinator)

	if _, err := svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: invitation.Revision,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
			OperationID:      "op-replace-1",
		}); !errors.Is(err, rfqissuance.ErrOfferWorkspaceConflict) {
		t.Fatalf("error = %v, want the offer-workspace conflict", err)
	}

	current, err := svc.GetInvitation(ctx, "company-1", invitation.ID)
	if err != nil {
		t.Fatalf("reloading the invitation: %v", err)
	}
	if current.RecipientEmailNormalized != invitation.RecipientEmailNormalized {
		t.Error("the recipient was replaced even though the offer-side claim " +
			"failed: a submitting draft or competing operation was overridden")
	}
	if current.AccessGeneration != invitation.AccessGeneration {
		t.Error("the link was invalidated even though the claim failed")
	}
}

// Archival failing AFTER the authoritative write must not roll anything back.
// The invitation replacement is already authoritative; the claim simply stays
// held for reconciliation to finish.
func TestReplaceRecipientKeepsTheClaimWhenArchivalFails(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	coordinator := &fakeOfferWorkspaceCoordinator{
		completeErr: errors.New("archival unavailable"),
	}
	svc.SetOfferWorkspaceCoordinator(coordinator)

	if _, err := svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: invitation.Revision,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
			OperationID:      "op-replace-1",
		}); err == nil {
		t.Fatal("a failed archival must surface so the caller retries")
	}

	// The authoritative write stands. Rolling it back would restore access for
	// a recipient the contractor already replaced.
	current, err := svc.GetInvitation(ctx, "company-1", invitation.ID)
	if err != nil {
		t.Fatalf("reloading the invitation: %v", err)
	}
	if current.RecipientEmailNormalized != "procurement@supplier.com" {
		t.Errorf("recipient = %q, want the replacement to remain authoritative",
			current.RecipientEmailNormalized)
	}
	if coordinator.calls[len(coordinator.calls)-1] == "abort" {
		t.Error("archival failure must never abort a completed replacement: " +
			"the previous recipient would regain the workspace")
	}
}

// A same-operation retry must converge without a second generation.
func TestReplaceRecipientRetryConvergesForTheSameOperation(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	svc.SetOfferWorkspaceCoordinator(&fakeOfferWorkspaceCoordinator{})

	input := rfqissuance.ReplaceRecipientInput{
		InvitationID:     invitation.ID,
		ExpectedRevision: invitation.Revision,
		RecipientName:    "Bala Krishnan",
		RecipientEmail:   "procurement@supplier.com",
		OperationID:      "op-replace-1",
	}

	first, err := svc.ReplaceRecipient(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("first replacement: %v", err)
	}
	retry, err := svc.ReplaceRecipient(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("same-operation retry must converge, got %v", err)
	}
	if retry.AccessGeneration != first.AccessGeneration {
		t.Errorf("retry generation = %d, want the completed %d: a retry must "+
			"not invalidate the new recipient's link",
			retry.AccessGeneration, first.AccessGeneration)
	}
}
