package supplieroffers

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// seedCopiedOfferLevelState puts the draft in the state copy-forward produces:
// offer-level tax, a delivery charge and a conditional group, each carrying an
// unresolved server-owned review gate.
func (rig *draftEditRig) seedCopiedOfferLevelState(t *testing.T) SupplierOfferDraft {
	t.Helper()
	ctx := context.Background()

	state := SupplierOfferDraftCommercialState{
		Lines: rig.draft.Lines,
		Tax: SupplierOfferTax{
			Mode: TaxModeOfferLevel,
			OfferLevel: &QuotedOfferTax{
				TaxType:            TaxTypeSalesTax,
				TaxAmount:          money.New(1_500, Phase1Currency),
				BasisNote:          "6% SST on the whole offer",
				RegistrationNumber: "SST-123",
			},
		},
		OfferTaxReviewRequired: true,
		ChargeGroups: []SupplierChargeGroupDraft{{
			ConditionalChargeGroup: ConditionalChargeGroup{
				ID: "group-1", Name: "Bulk handling",
			},
			ReviewRequired: true,
		}},
		DeliveryCharge:               &DeliveryCharge{Amount: money.New(15_000, Phase1Currency)},
		DeliveryChargeReviewRequired: true,
	}

	updated, err := rig.drafts.ReplaceActiveCommercialState(ctx, "company-1",
		rig.draft.ID, rig.draft.Revision, state, rig.now)
	if err != nil {
		t.Fatalf("seeding copied offer-level state: %v", err)
	}
	return updated
}

// Acknowledging offer-level tax clears ONLY that gate. If it cleared the others
// too, a Supplier could submit a copied delivery charge they never reviewed.
func TestAcknowledgeOfferTaxClearsOnlyItsOwnGate(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	acknowledged, err := rig.service.AcknowledgeOfferTax(ctx, AcknowledgeOfferTaxCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("AcknowledgeOfferTax: %v", err)
	}

	if acknowledged.OfferTaxReviewRequired {
		t.Error("the offer-tax review gate was not cleared")
	}
	if !acknowledged.DeliveryChargeReviewRequired {
		t.Error("acknowledging tax also cleared the DELIVERY gate; a copied " +
			"delivery charge could be submitted without review")
	}
	if !acknowledged.ChargeGroups[0].ReviewRequired {
		t.Error("acknowledging tax also cleared a CHARGE GROUP gate")
	}
	// The commercial values themselves are untouched by an acknowledgement.
	if acknowledged.Tax.Mode != TaxModeOfferLevel ||
		acknowledged.Tax.OfferLevel == nil ||
		acknowledged.Tax.OfferLevel.TaxAmount != money.New(1_500, Phase1Currency) {
		t.Errorf("tax = %+v, want the copied values preserved", acknowledged.Tax)
	}
}

// Acknowledging the delivery charge clears only the delivery gate.
func TestAcknowledgeDeliveryChargeClearsOnlyItsOwnGate(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	acknowledged, err := rig.service.AcknowledgeDeliveryCharge(ctx,
		AcknowledgeDeliveryChargeCommand{
			Context:          rig.input,
			DraftID:          rig.draft.ID,
			ExpectedRevision: seeded.Revision,
		})
	if err != nil {
		t.Fatalf("AcknowledgeDeliveryCharge: %v", err)
	}

	if acknowledged.DeliveryChargeReviewRequired {
		t.Error("the delivery review gate was not cleared")
	}
	if !acknowledged.OfferTaxReviewRequired {
		t.Error("acknowledging delivery also cleared the TAX gate")
	}
}

// Acknowledging one charge group must not clear another's gate.
func TestAcknowledgeChargeGroupClearsOnlyTheNamedGroup(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	// Add a second group that must stay gated.
	state := commercialStateOf(seeded)
	state.ChargeGroups = append(state.ChargeGroups, SupplierChargeGroupDraft{
		ConditionalChargeGroup: ConditionalChargeGroup{ID: "group-2", Name: "Remote site"},
		ReviewRequired:         true,
	})
	twoGroups, err := rig.drafts.ReplaceActiveCommercialState(ctx, "company-1",
		rig.draft.ID, seeded.Revision, state, rig.now)
	if err != nil {
		t.Fatalf("seeding a second charge group: %v", err)
	}

	acknowledged, err := rig.service.AcknowledgeChargeGroup(ctx,
		AcknowledgeChargeGroupCommand{
			Context:          rig.input,
			DraftID:          rig.draft.ID,
			ExpectedRevision: twoGroups.Revision,
			ChargeGroupID:    "group-1",
		})
	if err != nil {
		t.Fatalf("AcknowledgeChargeGroup: %v", err)
	}

	if acknowledged.ChargeGroups[0].ReviewRequired {
		t.Error("the named group's gate was not cleared")
	}
	if !acknowledged.ChargeGroups[1].ReviewRequired {
		t.Error("acknowledging one group cleared ANOTHER group's gate")
	}
}

// Removing the delivery charge removes its gate with it: a gate protecting
// something that no longer exists would block submission forever.
func TestRemoveDeliveryChargeClearsItsGate(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	removed, err := rig.service.RemoveDeliveryCharge(ctx, RemoveDeliveryChargeCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("RemoveDeliveryCharge: %v", err)
	}

	if removed.DeliveryCharge != nil {
		t.Error("the delivery charge was not removed")
	}
	if removed.DeliveryChargeReviewRequired {
		t.Error("a review gate survived removal of the thing it protected; " +
			"the draft could never be submitted")
	}
}

// Removing a charge group removes the whole group and its gate.
func TestRemoveChargeGroupRemovesTheGroupAndItsGate(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	removed, err := rig.service.RemoveChargeGroup(ctx, RemoveChargeGroupCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: seeded.Revision,
		ChargeGroupID:    "group-1",
	})
	if err != nil {
		t.Fatalf("RemoveChargeGroup: %v", err)
	}

	for _, group := range removed.ChargeGroups {
		if group.ID == "group-1" {
			t.Fatal("the charge group was not removed")
		}
	}
}

// Removal is revision-guarded like every other edit.
func TestRemoveDeliveryChargeRejectsAStaleRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	if _, err := rig.service.RemoveDeliveryCharge(ctx, RemoveDeliveryChargeCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: seeded.Revision + 99,
	}); !errors.Is(err, ErrOfferDraftConflict) {
		t.Fatalf("stale-revision removal error = %v, want conflict", err)
	}
}

// Acknowledging a group that does not exist must not silently succeed.
func TestAcknowledgeChargeGroupRejectsAnUnknownGroup(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	if _, err := rig.service.AcknowledgeChargeGroup(ctx,
		AcknowledgeChargeGroupCommand{
			Context:          rig.input,
			DraftID:          rig.draft.ID,
			ExpectedRevision: seeded.Revision,
			ChargeGroupID:    "group-missing",
		}); !errors.Is(err, ErrOfferDraftNotFound) {
		t.Fatalf("unknown-group acknowledgement error = %v, want not-found", err)
	}
}
