package rfqissuance_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Reactivation concurrency is a persistence invariant, so these tests use real
// MongoDB. An in-memory mutex would only prove the fake's implementation.

func revokedInvitationFixture(t *testing.T) (
	*rfqissuance.MongoInvitationRepository,
	rfqissuance.SupplierInvitation,
) {
	t.Helper()
	repo := newInvitationRepo(t, setupDB(t))
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	revokedAt := time.Now().UTC().Truncate(time.Millisecond)
	edited := created
	edited.Status = rfqissuance.InvitationStatusRevoked
	edited.RevokedAt = &revokedAt

	revoked, err := repo.UpdateInvitation(ctx, "company-1", created.ID,
		created.Revision, edited)
	if err != nil {
		t.Fatalf("revoke invitation: %v", err)
	}
	return repo, revoked
}

func reactivationCommand(invitation rfqissuance.SupplierInvitation,
	operationID string) rfqissuance.ReactivateInvitationCommand {

	return rfqissuance.ReactivateInvitationCommand{
		ExpectedRevision:          invitation.Revision,
		ExpiresAt:                 time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Millisecond),
		OperationID:               operationID,
		CurrentIssuedRFQVersionID: "version-2",
		NewAccessSecretHash:       "hash-generation-2",
		SecretKeyVersion:          2,
		Now:                       time.Now().UTC().Truncate(time.Millisecond),
	}
}

func TestMongoReactivateInvitationCommitsTheWholeNewGenerationAtomically(t *testing.T) {
	repo, revoked := revokedInvitationFixture(t)
	ctx := context.Background()
	command := reactivationCommand(revoked, "op-reactivate-1")

	reactivated, applied, err := repo.ReactivateInvitation(ctx, "company-1",
		revoked.ID, command)
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if !applied {
		t.Fatal("first operation did not report the authoritative write")
	}

	if reactivated.Status != rfqissuance.InvitationStatusActive ||
		reactivated.RevokedAt != nil {
		t.Errorf("status/revokedAt = %q/%v, want active/nil",
			reactivated.Status, reactivated.RevokedAt)
	}
	if reactivated.AccessGeneration != revoked.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want exactly %d",
			reactivated.AccessGeneration, revoked.AccessGeneration+1)
	}
	if reactivated.AccessSecretHash != command.NewAccessSecretHash ||
		reactivated.SecretKeyVersion != command.SecretKeyVersion {
		t.Errorf("hash/key version = %q/%d, want %q/%d",
			reactivated.AccessSecretHash, reactivated.SecretKeyVersion,
			command.NewAccessSecretHash, command.SecretKeyVersion)
	}
	if reactivated.CurrentIssuedRFQVersionID != command.CurrentIssuedRFQVersionID {
		t.Errorf("version pointer = %q, want %q",
			reactivated.CurrentIssuedRFQVersionID, command.CurrentIssuedRFQVersionID)
	}
	if !reactivated.ExpiresAt.Equal(command.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", reactivated.ExpiresAt, command.ExpiresAt)
	}
	if reactivated.Revision != revoked.Revision+1 {
		t.Errorf("Revision = %d, want %d", reactivated.Revision, revoked.Revision+1)
	}

	// The same operation resolves the result already written. In particular it
	// must not move generation or revision a second time.
	retried, applied, err := repo.ReactivateInvitation(ctx, "company-1",
		revoked.ID, command)
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if applied {
		t.Error("idempotent retry claimed a second authoritative write")
	}
	if retried.AccessGeneration != reactivated.AccessGeneration ||
		retried.Revision != reactivated.Revision ||
		retried.AccessSecretHash != reactivated.AccessSecretHash {
		t.Errorf("retry changed the result: first=%+v retry=%+v", reactivated, retried)
	}
}

func TestMongoConcurrentDifferentReactivationsHaveExactlyOneWinner(t *testing.T) {
	repo, revoked := revokedInvitationFixture(t)
	ctx := context.Background()

	commands := []rfqissuance.ReactivateInvitationCommand{
		reactivationCommand(revoked, "op-reactivate-a"),
		reactivationCommand(revoked, "op-reactivate-b"),
	}
	commands[1].NewAccessSecretHash = "hash-winner-b"

	type outcome struct {
		applied bool
		err     error
	}
	outcomes := make([]outcome, len(commands))

	var wg sync.WaitGroup
	wg.Add(len(commands))
	for i := range commands {
		go func(i int) {
			defer wg.Done()
			_, outcomes[i].applied, outcomes[i].err = repo.ReactivateInvitation(
				ctx, "company-1", revoked.ID, commands[i])
		}(i)
	}
	wg.Wait()

	winners := 0
	conflicts := 0
	for i, outcome := range outcomes {
		switch {
		case outcome.err == nil && outcome.applied:
			winners++
		case errors.Is(outcome.err, rfqissuance.ErrRevisionMismatch):
			conflicts++
		default:
			t.Errorf("operation %d result = applied %v, error %v",
				i, outcome.applied, outcome.err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Errorf("winners/conflicts = %d/%d, want exactly 1/1", winners, conflicts)
	}

	final, err := repo.FindInvitation(ctx, "company-1", revoked.ID)
	if err != nil {
		t.Fatalf("read final invitation: %v", err)
	}
	if final.AccessGeneration != revoked.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want one increment to %d",
			final.AccessGeneration, revoked.AccessGeneration+1)
	}
	if final.Revision != revoked.Revision+1 {
		t.Errorf("Revision = %d, want one increment to %d",
			final.Revision, revoked.Revision+1)
	}
}

func TestMongoReactivateInvitationRefusesActiveUnexpiredStateWithoutMutation(t *testing.T) {
	db := setupDB(t)
	repo := newInvitationRepo(t, db)
	ctx := context.Background()

	created, err := repo.CreateInvitation(ctx,
		invitationFixture(t, "company-1", "chain-1", "supplier-1"))
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	edited := created
	edited.Status = rfqissuance.InvitationStatusActive
	active, err := repo.UpdateInvitation(ctx, "company-1", created.ID,
		created.Revision, edited)
	if err != nil {
		t.Fatalf("activate invitation: %v", err)
	}

	_, _, err = repo.ReactivateInvitation(ctx, "company-1", active.ID,
		reactivationCommand(active, "op-must-not-rotate"))
	if !errors.Is(err, rfqissuance.ErrInvitationAlreadyActive) {
		t.Errorf("error = %v, want ErrInvitationAlreadyActive", err)
	}

	after, err := repo.FindInvitation(ctx, "company-1", active.ID)
	if err != nil {
		t.Fatalf("read after refusal: %v", err)
	}
	if after.AccessGeneration != active.AccessGeneration ||
		after.Revision != active.Revision ||
		after.AccessSecretHash != active.AccessSecretHash {
		t.Errorf("active invitation mutated: before=%+v after=%+v", active, after)
	}
}
