package supplieroffers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func replacementService(
	t *testing.T,
) (*supplieroffers.Service, *supplieroffers.MongoOfferDraftRepository,
	*supplieroffers.MongoOfferChainRepository) {
	t.Helper()
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	chains := newOfferChainRepository(t, db)

	service := supplieroffers.NewService(
		supplieroffers.WithSupplierOfferDraftRepository(repository),
		supplieroffers.WithSupplierOfferChainRepository(chains),
	)
	return service, repository, chains
}

func preparation() supplieroffers.RecipientReplacementPreparation {
	return supplieroffers.RecipientReplacementPreparation{
		CompanyID:                  "company-1",
		InvitationID:               "invitation-1",
		IssuedRFQVersionID:         "issued-rfq-version-1",
		PreviousRecipientIdentity:  "previous@example.test",
		CandidateRecipientIdentity: "next@example.test",
		ReplacementOperationID:     "op-replace-1",
	}
}

func completion() supplieroffers.RecipientReplacementCompletion {
	return supplieroffers.RecipientReplacementCompletion{
		CompanyID:                    "company-1",
		InvitationID:                 "invitation-1",
		IssuedRFQVersionID:           "issued-rfq-version-1",
		PreviousRecipientIdentity:    "previous@example.test",
		ReplacementRecipientIdentity: "next@example.test",
		ReplacementOperationID:       "op-replace-1",
	}
}

// Preparing with no existing draft must still take a durable barrier, so the
// previous recipient cannot open a draft during the replacement window.
func TestPrepareRecipientReplacementTakesABarrierWithNoDraft(t *testing.T) {
	service, repository, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}

	held, found, err := repository.FindUnfinishedDraftForInvitation(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil || !found {
		t.Fatalf("expected a held claim, found = %v, err = %v", found, err)
	}
	if held.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Errorf("status = %q, want the slot to be held", held.Status)
	}
}

// Completion archives the claim with its full provenance, and the replacement
// recipient must NOT inherit the previous recipient's draft.
func TestCompleteRecipientReplacementArchivesWithProvenance(t *testing.T) {
	service, repository, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}
	if err := service.CompleteRecipientReplacement(ctx, completion()); err != nil {
		t.Fatalf("CompleteRecipientReplacement: %v", err)
	}

	_, found, err := repository.FindUnfinishedDraftForInvitation(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil {
		t.Fatalf("looking up the unfinished slot: %v", err)
	}
	if found {
		t.Fatal("the slot is still held after completion; the replacement " +
			"recipient can never open their own draft")
	}
}

// The archived draft carries why it was archived and by whom, so replacement
// history is auditable rather than an unexplained disappearance.
func TestCompleteRecipientReplacementPreservesArchivalReason(t *testing.T) {
	service, repository, chains := replacementService(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	// The draft must live on the SAME Offer Chain the service resolves, since
	// that chain identity is what the uniqueness slot is keyed on.
	chain, err := chains.EnsureOfferChain(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil {
		t.Fatalf("ensuring the offer chain: %v", err)
	}
	existing := activeCommercialDraft(now)
	existing.OfferChainID = chain.ID
	if err := repository.InsertDraft(ctx, existing); err != nil {
		t.Fatalf("inserting active draft: %v", err)
	}

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}
	if err := service.CompleteRecipientReplacement(ctx, completion()); err != nil {
		t.Fatalf("CompleteRecipientReplacement: %v", err)
	}

	archived, found, err := repository.FindDraft(ctx, "company-1", existing.ID)
	if err != nil || !found {
		t.Fatalf("FindDraft found = %v, err = %v", found, err)
	}
	if archived.Status != supplieroffers.DraftArchived {
		t.Errorf("status = %q, want archived", archived.Status)
	}
	if archived.ArchivedReason != supplieroffers.DraftArchivedReasonRecipientReplacement {
		t.Errorf("ArchivedReason = %q, want recipient_replacement",
			archived.ArchivedReason)
	}
	if archived.ArchivedByOperationID != "op-replace-1" {
		t.Errorf("ArchivedByOperationID = %q, want op-replace-1",
			archived.ArchivedByOperationID)
	}
	if archived.PreviousRecipientIdentity != "previous@example.test" ||
		archived.ReplacementRecipientIdentity != "next@example.test" {
		t.Errorf("archival recorded %q -> %q, want the exact pair",
			archived.PreviousRecipientIdentity, archived.ReplacementRecipientIdentity)
	}
	if archived.ArchivedAt == nil {
		t.Error("ArchivedAt must be stamped")
	}
}

// Completion is idempotent: recovery re-drives it after a lost response.
func TestCompleteRecipientReplacementIsIdempotent(t *testing.T) {
	service, _, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}
	if err := service.CompleteRecipientReplacement(ctx, completion()); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if err := service.CompleteRecipientReplacement(ctx, completion()); err != nil {
		t.Fatalf("repeat completion must converge, got %v", err)
	}
}

// A different operation must not be able to complete — or steal — a claim it
// does not own.
func TestCompleteRecipientReplacementRejectsAForeignOperation(t *testing.T) {
	service, _, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}

	foreign := completion()
	foreign.ReplacementOperationID = "op-replace-2"
	if err := service.CompleteRecipientReplacement(ctx, foreign); !errors.Is(
		err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("a foreign completion error = %v, want conflict", err)
	}
}

// Abort releases a claim only for its owning operation.
func TestAbortRecipientReplacementReleasesOnlyItsOwnClaim(t *testing.T) {
	service, repository, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}

	foreign := supplieroffers.RecipientReplacementAbort{
		CompanyID:                 "company-1",
		InvitationID:              "invitation-1",
		IssuedRFQVersionID:        "issued-rfq-version-1",
		PreviousRecipientIdentity: "previous@example.test",
		ReplacementOperationID:    "op-replace-2",
	}
	if err := service.AbortRecipientReplacement(ctx, foreign); !errors.Is(
		err, supplieroffers.ErrOfferDraftConflict) {
		t.Fatalf("a foreign abort error = %v, want conflict: one operation "+
			"must never release another's barrier", err)
	}

	// The claim must survive the foreign abort attempt.
	held, found, err := repository.FindUnfinishedDraftForInvitation(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil || !found {
		t.Fatalf("the claim must survive, found = %v, err = %v", found, err)
	}
	if held.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Errorf("status = %q, want the claim still held", held.Status)
	}

	owning := foreign
	owning.ReplacementOperationID = "op-replace-1"
	if err := service.AbortRecipientReplacement(ctx, owning); err != nil {
		t.Fatalf("the owning operation must be able to abort, got %v", err)
	}
}

// Aborting a barrier-only claim frees the slot so the recipient can work again.
func TestAbortRecipientReplacementFreesTheSlot(t *testing.T) {
	service, repository, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}
	if err := service.AbortRecipientReplacement(ctx,
		supplieroffers.RecipientReplacementAbort{
			CompanyID:                 "company-1",
			InvitationID:              "invitation-1",
			IssuedRFQVersionID:        "issued-rfq-version-1",
			PreviousRecipientIdentity: "previous@example.test",
			ReplacementOperationID:    "op-replace-1",
		}); err != nil {
		t.Fatalf("AbortRecipientReplacement: %v", err)
	}

	_, found, err := repository.FindUnfinishedDraftForInvitation(
		ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil {
		t.Fatalf("looking up the unfinished slot: %v", err)
	}
	if found {
		t.Error("the slot is still held after abort")
	}
}

// Same-operation prepare converges rather than failing or double-claiming.
func TestPrepareRecipientReplacementIsIdempotent(t *testing.T) {
	service, _, _ := replacementService(t)
	ctx := context.Background()

	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	if err := service.PrepareRecipientReplacement(ctx, preparation()); err != nil {
		t.Fatalf("same-operation prepare must converge, got %v", err)
	}
}
