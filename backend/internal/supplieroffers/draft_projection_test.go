package supplieroffers

import (
	"context"
	"testing"
)

func TestGetActiveDraftProjectionDerivesEditAndSubmissionCapabilities(t *testing.T) {
	rig := newSubmissionRig(t)

	projection, err := rig.service.GetActiveDraftProjection(context.Background(),
		SupplierOfferReadContextInput{
			SessionToken: rig.input.SessionToken, InvitationID: rig.input.InvitationID,
			AccessedAt: rig.input.AccessedAt,
		})
	if err != nil {
		t.Fatalf("GetActiveDraftProjection: %v", err)
	}
	if projection.Draft.ID != rig.readyDraft.ID {
		t.Fatalf("draft ID = %q, want %q", projection.Draft.ID, rig.readyDraft.ID)
	}
	if !projection.CanEdit || !projection.CanSubmit {
		t.Fatalf("capabilities = edit:%v submit:%v, want both true",
			projection.CanEdit, projection.CanSubmit)
	}
}

func TestGetActiveDraftProjectionFailsSubmissionCapabilityClosed(t *testing.T) {
	rig := newDraftEditRig(t)

	projection, err := rig.service.GetActiveDraftProjection(context.Background(),
		SupplierOfferReadContextInput{
			SessionToken: rig.input.SessionToken, InvitationID: rig.input.InvitationID,
			AccessedAt: rig.input.AccessedAt,
		})
	if err != nil {
		t.Fatalf("GetActiveDraftProjection: %v", err)
	}
	if !projection.CanEdit || projection.CanSubmit {
		t.Fatalf("capabilities = edit:%v submit:%v, want editable but not submittable",
			projection.CanEdit, projection.CanSubmit)
	}
}
