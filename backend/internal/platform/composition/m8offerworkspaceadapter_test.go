package composition_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// fakeOfferWorkspaceSource stands in for supplieroffers.Service and records the
// primitive-only values that cross the module boundary.
type fakeOfferWorkspaceSource struct {
	prepared  supplieroffers.RecipientReplacementPreparation
	completed supplieroffers.RecipientReplacementCompletion
	aborted   supplieroffers.RecipientReplacementAbort

	prepareErr error
}

func (f *fakeOfferWorkspaceSource) PrepareRecipientReplacement(
	_ context.Context, input supplieroffers.RecipientReplacementPreparation) error {
	f.prepared = input
	return f.prepareErr
}

func (f *fakeOfferWorkspaceSource) CompleteRecipientReplacement(
	_ context.Context, input supplieroffers.RecipientReplacementCompletion) error {
	f.completed = input
	return nil
}

func (f *fakeOfferWorkspaceSource) AbortRecipientReplacement(
	_ context.Context, input supplieroffers.RecipientReplacementAbort) error {
	f.aborted = input
	return nil
}

// The adapter is the ONLY path between the two modules: rfqissuance names no
// supplieroffers type and reaches no supplieroffers collection.
func TestOfferWorkspaceAdapterTranslatesEveryOperation(t *testing.T) {
	source := &fakeOfferWorkspaceSource{}
	adapter := composition.NewOfferWorkspaceAdapter(source)
	ctx := context.Background()

	// The adapter must satisfy the capability rfqissuance declares.
	var _ rfqissuance.OfferWorkspaceCoordinator = adapter

	if err := adapter.PrepareRecipientReplacement(ctx,
		rfqissuance.RecipientReplacementPreparation{
			CompanyID:                  "company-1",
			InvitationID:               "invitation-1",
			IssuedRFQVersionID:         "issued-1",
			PreviousRecipientIdentity:  "previous@example.test",
			CandidateRecipientIdentity: "next@example.test",
			ReplacementOperationID:     "op-1",
		}); err != nil {
		t.Fatalf("PrepareRecipientReplacement: %v", err)
	}
	if source.prepared.CompanyID != "company-1" ||
		source.prepared.PreviousRecipientIdentity != "previous@example.test" ||
		source.prepared.CandidateRecipientIdentity != "next@example.test" ||
		source.prepared.ReplacementOperationID != "op-1" {
		t.Errorf("prepared = %+v, want every identity carried across", source.prepared)
	}

	if err := adapter.CompleteRecipientReplacement(ctx,
		rfqissuance.RecipientReplacementCompletion{
			CompanyID:                    "company-1",
			InvitationID:                 "invitation-1",
			IssuedRFQVersionID:           "issued-1",
			PreviousRecipientIdentity:    "previous@example.test",
			ReplacementRecipientIdentity: "next@example.test",
			ReplacementOperationID:       "op-1",
		}); err != nil {
		t.Fatalf("CompleteRecipientReplacement: %v", err)
	}
	if source.completed.ReplacementRecipientIdentity != "next@example.test" ||
		source.completed.ReplacementOperationID != "op-1" {
		t.Errorf("completed = %+v, want the archival provenance carried across",
			source.completed)
	}

	if err := adapter.AbortRecipientReplacement(ctx,
		rfqissuance.RecipientReplacementAbort{
			CompanyID:                 "company-1",
			InvitationID:              "invitation-1",
			IssuedRFQVersionID:        "issued-1",
			PreviousRecipientIdentity: "previous@example.test",
			ReplacementOperationID:    "op-1",
		}); err != nil {
		t.Fatalf("AbortRecipientReplacement: %v", err)
	}
	if source.aborted.ReplacementOperationID != "op-1" {
		t.Errorf("aborted = %+v, want the owning operation carried across",
			source.aborted)
	}
}

// A claim conflict must surface as the consumer's own bounded error, so
// rfqissuance never has to interpret a supplieroffers sentinel.
func TestOfferWorkspaceAdapterMapsAClaimConflict(t *testing.T) {
	source := &fakeOfferWorkspaceSource{
		prepareErr: supplieroffers.ErrOfferDraftConflict,
	}
	adapter := composition.NewOfferWorkspaceAdapter(source)

	err := adapter.PrepareRecipientReplacement(context.Background(),
		rfqissuance.RecipientReplacementPreparation{
			CompanyID:              "company-1",
			InvitationID:           "invitation-1",
			ReplacementOperationID: "op-1",
		})
	if !errors.Is(err, rfqissuance.ErrOfferWorkspaceConflict) {
		t.Fatalf("error = %v, want the consumer-owned workspace conflict", err)
	}
}

// An infrastructure failure must NOT be reported as a conflict: a conflict
// would tell the caller the workspace is busy, when in truth the claim state
// is unknown and the replacement must not proceed.
func TestOfferWorkspaceAdapterPropagatesInfrastructureFailures(t *testing.T) {
	failure := errors.New("mongo unavailable")
	source := &fakeOfferWorkspaceSource{prepareErr: failure}
	adapter := composition.NewOfferWorkspaceAdapter(source)

	err := adapter.PrepareRecipientReplacement(context.Background(),
		rfqissuance.RecipientReplacementPreparation{
			CompanyID:              "company-1",
			InvitationID:           "invitation-1",
			ReplacementOperationID: "op-1",
		})
	if errors.Is(err, rfqissuance.ErrOfferWorkspaceConflict) {
		t.Fatal("an infrastructure failure must not masquerade as a conflict")
	}
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the underlying failure preserved", err)
	}
}
