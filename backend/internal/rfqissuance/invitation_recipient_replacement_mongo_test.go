package rfqissuance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

func seedReplaceableInvitation(
	t *testing.T,
	repo *rfqissuance.MongoInvitationRepository,
) rfqissuance.SupplierInvitation {
	t.Helper()
	created, err := repo.CreateInvitation(context.Background(),
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	return created
}

// Recipient replacement must be ONE atomic write.
//
// Performing the recipient snapshot and the secret rotation as two sequential
// CAS operations leaves a crash window in which the recipient has already
// changed while the previous recipient's link still resolves — the exact stale
// -link exposure replacement exists to close (§5.3A).
func TestReplaceRecipientAtomicallyRotatesAndSnapshots(t *testing.T) {
	db := setupDB(t)
	repository := newInvitationRepo(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	invitation := seedReplaceableInvitation(t, repository)

	replaced, err := repository.ReplaceRecipientAtomically(ctx,
		rfqissuance.ReplaceRecipientAtomicInput{
			CompanyID:                 invitation.CompanyID,
			InvitationID:              invitation.ID,
			ExpectedRevision:          invitation.Revision,
			PreviousRecipientIdentity: invitation.RecipientEmailNormalized,
			RecipientName:             "Next Person",
			RecipientEmail:            "next@example.test",
			RecipientEmailNormalized:  "next@example.test",
			NewSecretHash:             "hash-generation-2",
			SecretKeyVersion:          3,
			ReplacementOperationID:    "op-replace-1",
			ReplacedAt:                now,
		})
	if err != nil {
		t.Fatalf("ReplaceRecipientAtomically: %v", err)
	}

	if replaced.AccessGeneration != invitation.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want exactly one increment from %d",
			replaced.AccessGeneration, invitation.AccessGeneration)
	}
	if replaced.AccessSecretHash != "hash-generation-2" {
		t.Errorf("AccessSecretHash = %q, want the newly derived hash",
			replaced.AccessSecretHash)
	}
	if replaced.SecretKeyVersion != 3 {
		t.Errorf("SecretKeyVersion = %d, want the active key version 3",
			replaced.SecretKeyVersion)
	}
	if replaced.RecipientEmailNormalized != "next@example.test" ||
		replaced.RecipientName != "Next Person" {
		t.Errorf("recipient snapshot = %q/%q, want the replacement recipient",
			replaced.RecipientName, replaced.RecipientEmailNormalized)
	}
	if replaced.Revision != invitation.Revision+1 {
		t.Errorf("Revision = %d, want exactly one increment: the snapshot and "+
			"rotation are a single write, not two", replaced.Revision)
	}
}

// The write requires the EXACT previous recipient. Without it, an operation
// working from a stale read could replace a recipient that another replacement
// already changed, silently discarding that replacement.
func TestReplaceRecipientAtomicallyRequiresTheExactPreviousRecipient(t *testing.T) {
	db := setupDB(t)
	repository := newInvitationRepo(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	invitation := seedReplaceableInvitation(t, repository)

	_, err := repository.ReplaceRecipientAtomically(ctx,
		rfqissuance.ReplaceRecipientAtomicInput{
			CompanyID:                 invitation.CompanyID,
			InvitationID:              invitation.ID,
			ExpectedRevision:          invitation.Revision,
			PreviousRecipientIdentity: "stale@example.test",
			RecipientName:             "Next Person",
			RecipientEmail:            "next@example.test",
			RecipientEmailNormalized:  "next@example.test",
			NewSecretHash:             "hash-generation-2",
			SecretKeyVersion:          3,
			ReplacementOperationID:    "op-replace-1",
			ReplacedAt:                now,
		})
	if !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Fatalf("replacing from a stale previous recipient error = %v, "+
			"want a mismatch rather than a silent overwrite", err)
	}
}

// A failed replacement must leave NOTHING changed. A partial write is exactly
// the two-write bug: recipient changed while the old link still resolves.
func TestReplaceRecipientAtomicallyLeavesNoPartialState(t *testing.T) {
	db := setupDB(t)
	repository := newInvitationRepo(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	invitation := seedReplaceableInvitation(t, repository)

	if _, err := repository.ReplaceRecipientAtomically(ctx,
		rfqissuance.ReplaceRecipientAtomicInput{
			CompanyID:                 invitation.CompanyID,
			InvitationID:              invitation.ID,
			ExpectedRevision:          invitation.Revision + 99,
			PreviousRecipientIdentity: invitation.RecipientEmailNormalized,
			RecipientName:             "Next Person",
			RecipientEmail:            "next@example.test",
			RecipientEmailNormalized:  "next@example.test",
			NewSecretHash:             "hash-generation-2",
			SecretKeyVersion:          3,
			ReplacementOperationID:    "op-replace-1",
			ReplacedAt:                now,
		}); err == nil {
		t.Fatal("a stale expected revision must not replace the recipient")
	}

	unchanged, err := repository.FindInvitation(ctx, invitation.CompanyID, invitation.ID)
	if err != nil {
		t.Fatalf("reloading the invitation: %v", err)
	}
	if unchanged.RecipientEmailNormalized != invitation.RecipientEmailNormalized {
		t.Errorf("recipient = %q, want the original %q: a rejected replacement "+
			"must not change the recipient",
			unchanged.RecipientEmailNormalized, invitation.RecipientEmailNormalized)
	}
	if unchanged.AccessGeneration != invitation.AccessGeneration {
		t.Errorf("AccessGeneration = %d, want the original %d: a rejected "+
			"replacement must not invalidate the current link",
			unchanged.AccessGeneration, invitation.AccessGeneration)
	}
	if unchanged.AccessSecretHash != invitation.AccessSecretHash {
		t.Error("a rejected replacement must not rotate the secret hash")
	}
}

// Same-operation retry must converge without minting a second generation.
// Recovery re-runs this write after an infrastructure failure, and a second
// increment would invalidate the replacement recipient's freshly issued link.
func TestReplaceRecipientAtomicallyIsIdempotentForTheSameOperation(t *testing.T) {
	db := setupDB(t)
	repository := newInvitationRepo(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	invitation := seedReplaceableInvitation(t, repository)
	input := rfqissuance.ReplaceRecipientAtomicInput{
		CompanyID:                 invitation.CompanyID,
		InvitationID:              invitation.ID,
		ExpectedRevision:          invitation.Revision,
		PreviousRecipientIdentity: invitation.RecipientEmailNormalized,
		RecipientName:             "Next Person",
		RecipientEmail:            "next@example.test",
		RecipientEmailNormalized:  "next@example.test",
		NewSecretHash:             "hash-generation-2",
		SecretKeyVersion:          3,
		ReplacementOperationID:    "op-replace-1",
		ReplacedAt:                now,
	}

	first, err := repository.ReplaceRecipientAtomically(ctx, input)
	if err != nil {
		t.Fatalf("first replacement: %v", err)
	}

	retry, err := repository.ReplaceRecipientAtomically(ctx, input)
	if err != nil {
		t.Fatalf("same-operation retry must converge, got %v", err)
	}
	if retry.AccessGeneration != first.AccessGeneration {
		t.Errorf("retry AccessGeneration = %d, want the completed %d: a retry "+
			"must not invalidate the new recipient's link",
			retry.AccessGeneration, first.AccessGeneration)
	}
	if retry.Revision != first.Revision {
		t.Errorf("retry Revision = %d, want the completed %d",
			retry.Revision, first.Revision)
	}
}
