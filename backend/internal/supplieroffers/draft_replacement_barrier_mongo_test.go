package supplieroffers_test

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func barrierClaim(claimedAt time.Time) supplieroffers.SupplierOfferDraft {
	return supplieroffers.SupplierOfferDraft{
		ID:                         "draft-barrier",
		CompanyID:                  "company-1",
		OfferChainID:               "chain-1",
		InvitationID:               "invitation-1",
		IssuedRFQVersionID:         "issued-rfq-version-1",
		RecipientIdentity:          "previous@example.test",
		Status:                     supplieroffers.DraftRecipientReplacementClaimed,
		DraftPurpose:               supplieroffers.DraftPurposeRecipientReplacementBarrier,
		ReplacementOperationID:     "op-replace-1",
		PreviousRecipientIdentity:  "previous@example.test",
		CandidateRecipientIdentity: "next@example.test",
		ClaimedAt:                  &claimedAt,
		Revision:                   1,
		CreatedAt:                  claimedAt,
		UpdatedAt:                  claimedAt,
		SchemaVersion:              supplieroffers.SupplierOfferDraftSchemaVersion,
	}
}

// The barrier must contend on the SAME named unique index as ordinary draft
// creation. This is the whole no-draft race guarantee: MongoDB picks one
// winner atomically instead of a count/existence check followed by an insert.
func TestReplacementBarrierOccupiesTheUnfinishedDraftSlot(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	if err := repository.InsertDraft(ctx, barrierClaim(now)); err != nil {
		t.Fatalf("inserting the replacement barrier: %v", err)
	}

	// The previous recipient must not be able to open a fresh draft while the
	// replacement is in flight.
	competing := supplieroffers.SupplierOfferDraft{
		ID:                 "draft-active",
		CompanyID:          "company-1",
		OfferChainID:       "chain-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-rfq-version-1",
		RecipientIdentity:  "previous@example.test",
		Status:             supplieroffers.DraftActive,
		DraftPurpose:       supplieroffers.DraftPurposeCommercial,
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
		SchemaVersion:      supplieroffers.SupplierOfferDraftSchemaVersion,
	}
	if err := repository.InsertDraft(ctx, competing); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("creating a draft against a held barrier error = %v, want duplicate key",
			err)
	}
}

// EnsureUnfinishedDraft is the authorized create-or-get path. It must treat a
// held barrier as occupying the slot rather than creating a second draft.
func TestEnsureUnfinishedDraftIsBlockedByAHeldBarrier(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	claim := barrierClaim(now)
	if err := repository.InsertDraft(ctx, claim); err != nil {
		t.Fatalf("inserting the replacement barrier: %v", err)
	}

	candidate := supplieroffers.SupplierOfferDraft{
		ID:                 "draft-candidate",
		CompanyID:          "company-1",
		OfferChainID:       "chain-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-rfq-version-1",
		RecipientIdentity:  "previous@example.test",
		Status:             supplieroffers.DraftActive,
		DraftPurpose:       supplieroffers.DraftPurposeCommercial,
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
		SchemaVersion:      supplieroffers.SupplierOfferDraftSchemaVersion,
	}

	persisted, created, err := repository.EnsureUnfinishedDraft(ctx, candidate)
	if err != nil {
		t.Fatalf("EnsureUnfinishedDraft against a held barrier: %v", err)
	}
	if created {
		t.Fatal("EnsureUnfinishedDraft created a draft while a replacement " +
			"barrier was held: the replaced recipient regained a workspace")
	}
	if persisted.ID != claim.ID {
		t.Fatalf("persisted draft ID = %q, want the held barrier %q",
			persisted.ID, claim.ID)
	}
	if persisted.Status != supplieroffers.DraftRecipientReplacementClaimed {
		t.Fatalf("persisted status = %q, want the barrier to remain claimed",
			persisted.Status)
	}
}

// Claim and archival bookkeeping must survive persistence exactly: recovery
// verifies both recipient identities and the owning operation from storage.
func TestReplacementBarrierClaimFieldsRoundTrip(t *testing.T) {
	db := setupDB(t)
	repository := newOfferDraftRepository(t, db)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	claim := barrierClaim(now)
	if err := repository.InsertDraft(ctx, claim); err != nil {
		t.Fatalf("inserting the replacement barrier: %v", err)
	}

	found, ok, err := repository.FindDraft(ctx, claim.CompanyID, claim.ID)
	if err != nil || !ok {
		t.Fatalf("FindDraft ok = %v, err = %v", ok, err)
	}

	if found.DraftPurpose != supplieroffers.DraftPurposeRecipientReplacementBarrier {
		t.Errorf("DraftPurpose = %q, want the barrier discriminator",
			found.DraftPurpose)
	}
	if found.ReplacementOperationID != claim.ReplacementOperationID {
		t.Errorf("ReplacementOperationID = %q, want %q",
			found.ReplacementOperationID, claim.ReplacementOperationID)
	}
	if found.PreviousRecipientIdentity != claim.PreviousRecipientIdentity {
		t.Errorf("PreviousRecipientIdentity = %q, want %q",
			found.PreviousRecipientIdentity, claim.PreviousRecipientIdentity)
	}
	if found.CandidateRecipientIdentity != claim.CandidateRecipientIdentity {
		t.Errorf("CandidateRecipientIdentity = %q, want %q",
			found.CandidateRecipientIdentity, claim.CandidateRecipientIdentity)
	}
	if found.ClaimedAt == nil || !found.ClaimedAt.Equal(*claim.ClaimedAt) {
		t.Errorf("ClaimedAt = %v, want %v", found.ClaimedAt, claim.ClaimedAt)
	}
	if err := found.ValidateBarrierInvariants(); err != nil {
		t.Errorf("a round-tripped barrier must stay valid, got %v", err)
	}
}
