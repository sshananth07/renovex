package supplieroffers

import (
	"context"
	"testing"
)

// M8.1 checkpoint 5 (revised): every effective mutation is state-based
// convergent. Repeating a request whose postcondition ALREADY holds must
// return the current draft without requiring the caller to know the new
// revision and without incrementing it again.

// Acknowledging an already-acknowledged offer tax converges: the second call
// uses the STALE (pre-acknowledgement) revision, proving convergence does not
// require the caller to track the new revision after the first success.
func TestAcknowledgeOfferTaxConvergesOnRepeat(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	first, err := rig.service.AcknowledgeOfferTax(ctx, AcknowledgeOfferTaxCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("first AcknowledgeOfferTax: %v", err)
	}

	second, err := rig.service.AcknowledgeOfferTax(ctx, AcknowledgeOfferTaxCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("second AcknowledgeOfferTax (should converge): %v", err)
	}
	if second.Revision != first.Revision {
		t.Errorf("Revision = %d, want unchanged %d on convergence", second.Revision, first.Revision)
	}
	if second.OfferTaxReviewRequired {
		t.Error("converged draft still shows the tax gate as required")
	}
}

func TestAcknowledgeDeliveryChargeConvergesOnRepeat(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	first, err := rig.service.AcknowledgeDeliveryCharge(ctx, AcknowledgeDeliveryChargeCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("first AcknowledgeDeliveryCharge: %v", err)
	}

	second, err := rig.service.AcknowledgeDeliveryCharge(ctx, AcknowledgeDeliveryChargeCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("second AcknowledgeDeliveryCharge (should converge): %v", err)
	}
	if second.Revision != first.Revision {
		t.Errorf("Revision = %d, want unchanged %d on convergence", second.Revision, first.Revision)
	}
}

func TestAcknowledgeChargeGroupConvergesOnRepeat(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	first, err := rig.service.AcknowledgeChargeGroup(ctx, AcknowledgeChargeGroupCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		ChargeGroupID: "group-1",
	})
	if err != nil {
		t.Fatalf("first AcknowledgeChargeGroup: %v", err)
	}

	second, err := rig.service.AcknowledgeChargeGroup(ctx, AcknowledgeChargeGroupCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		ChargeGroupID: "group-1",
	})
	if err != nil {
		t.Fatalf("second AcknowledgeChargeGroup (should converge): %v", err)
	}
	if second.Revision != first.Revision {
		t.Errorf("Revision = %d, want unchanged %d on convergence", second.Revision, first.Revision)
	}
}

// Removing an already-absent delivery charge converges: an absent component
// IS the successful postcondition of a remove operation.
func TestRemoveDeliveryChargeConvergesWhenAlreadyAbsent(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	first, err := rig.service.RemoveDeliveryCharge(ctx, RemoveDeliveryChargeCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("first RemoveDeliveryCharge: %v", err)
	}

	second, err := rig.service.RemoveDeliveryCharge(ctx, RemoveDeliveryChargeCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
	})
	if err != nil {
		t.Fatalf("second RemoveDeliveryCharge (should converge): %v", err)
	}
	if second.Revision != first.Revision {
		t.Errorf("Revision = %d, want unchanged %d on convergence", second.Revision, first.Revision)
	}
	if second.DeliveryCharge != nil {
		t.Error("converged draft still shows a delivery charge")
	}
}

// Removing an already-absent charge group converges the same way.
func TestRemoveChargeGroupConvergesWhenAlreadyAbsent(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	first, err := rig.service.RemoveChargeGroup(ctx, RemoveChargeGroupCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		ChargeGroupID: "group-1",
	})
	if err != nil {
		t.Fatalf("first RemoveChargeGroup: %v", err)
	}

	second, err := rig.service.RemoveChargeGroup(ctx, RemoveChargeGroupCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		ChargeGroupID: "group-1",
	})
	if err != nil {
		t.Fatalf("second RemoveChargeGroup (should converge): %v", err)
	}
	if second.Revision != first.Revision {
		t.Errorf("Revision = %d, want unchanged %d on convergence", second.Revision, first.Revision)
	}
}

// Resetting an already-unanswered line converges on repeat with a stale
// expected revision, the same as the other five operations.
func TestResetDraftLineResponseConvergesOnRepeatWithStaleRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()

	first, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: rig.draft.Lines[0].ID, UnitPriceMinor: 2_500,
	})
	if err != nil {
		t.Fatalf("QuoteDraftLine: %v", err)
	}

	resetFirst, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: first.Revision,
		DraftLineID: rig.draft.Lines[0].ID,
	})
	if err != nil {
		t.Fatalf("first ResetDraftLineResponse: %v", err)
	}

	// Repeat using the STALE pre-reset revision, proving the caller does not
	// need to track the new revision to converge.
	resetSecond, err := rig.service.ResetDraftLineResponse(ctx, ResetDraftLineResponseCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: first.Revision,
		DraftLineID: rig.draft.Lines[0].ID,
	})
	if err != nil {
		t.Fatalf("second ResetDraftLineResponse (should converge): %v", err)
	}
	if resetSecond.Revision != resetFirst.Revision {
		t.Errorf("Revision = %d, want unchanged %d on convergence",
			resetSecond.Revision, resetFirst.Revision)
	}
}

// A CAS loss whose requested postcondition does NOT hold is a genuine
// conflict, not a convergence: two different effective mutations racing on
// the same expected revision cannot both win.
func TestAcknowledgeChargeGroupStaleRevisionWithUnmetPostconditionIsConflict(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	seeded := rig.seedCopiedOfferLevelState(t)

	// A concurrent edit advances the revision without acknowledging the group.
	requoted, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		DraftLineID: rig.draft.Lines[0].ID, UnitPriceMinor: 3_000,
	})
	if err != nil {
		t.Fatalf("concurrent QuoteDraftLine: %v", err)
	}
	_ = requoted

	// This call still uses the now-stale seeded revision, and the group's
	// review gate has NOT been cleared by anything else, so this must fail
	// as a genuine conflict rather than silently converge.
	_, err = rig.service.AcknowledgeChargeGroup(ctx, AcknowledgeChargeGroupCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: seeded.Revision,
		ChargeGroupID: "group-1",
	})
	if err == nil {
		t.Fatal("expected a conflict when the postcondition does not already hold")
	}
}
