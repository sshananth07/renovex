package supplieroffers

import (
	"context"
	"errors"
	"testing"
)

// withdrawalRig submits an offer so there is a real immutable version to
// withdraw, which is the only state withdrawal can act on.
func newWithdrawalRig(t *testing.T) (*submissionRig, SupplierOfferVersion) {
	t.Helper()
	rig := newSubmissionRig(t)
	rig.service.withdrawals = NewMongoOfferWithdrawalRepository(rig.db)
	if err := rig.service.withdrawals.(*MongoOfferWithdrawalRepository).
		EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring withdrawal indexes: %v", err)
	}

	version, err := rig.service.SubmitOffer(context.Background(), rig.submitInput)
	if err != nil {
		t.Fatalf("submitting the offer: %v", err)
	}
	return rig, version
}

// Withdrawing records an immutable withdrawal AND moves the separate
// eligibility gate, without ever mutating the frozen Offer Version.
func TestWithdrawOfferRecordsAnImmutableWithdrawal(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	ctx := context.Background()

	withdrawal, err := rig.service.WithdrawOffer(ctx, WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "we can no longer supply at this price",
		OperationID:    "op-withdraw-1",
	})
	if err != nil {
		t.Fatalf("WithdrawOffer: %v", err)
	}

	if withdrawal.Reason != "we can no longer supply at this price" {
		t.Errorf("Reason = %q, want the supplied reason", withdrawal.Reason)
	}
	if withdrawal.SupplierOfferVersionID != version.ID {
		t.Errorf("version = %q, want %q",
			withdrawal.SupplierOfferVersionID, version.ID)
	}

	gate, found, err := rig.eligibility.FindEligibility(ctx, "company-1", version.ID)
	if err != nil || !found {
		t.Fatalf("eligibility found = %v, err = %v", found, err)
	}
	if gate.State != EligibilityWithdrawn {
		t.Errorf("eligibility state = %q, want withdrawn", gate.State)
	}
	if gate.WithdrawalReason != withdrawal.Reason {
		t.Errorf("gate reason = %q, want it to match the withdrawal",
			gate.WithdrawalReason)
	}

	// The immutable version is untouched: withdrawal lives in its own
	// aggregate precisely so frozen commercial content never changes.
	frozen, found, err := rig.versions.FindVersion(ctx, "company-1", version.ID)
	if err != nil || !found {
		t.Fatalf("reloading the version: %v", err)
	}
	if frozen.GrandTotal != version.GrandTotal {
		t.Error("withdrawal mutated the immutable offer version")
	}
}

// A same-operation retry converges on the same withdrawal rather than creating
// a second record or failing.
func TestWithdrawOfferIsIdempotentForTheSameOperation(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	ctx := context.Background()

	command := WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "priced in error",
		OperationID:    "op-withdraw-1",
	}

	first, err := rig.service.WithdrawOffer(ctx, command)
	if err != nil {
		t.Fatalf("first withdrawal: %v", err)
	}
	retry, err := rig.service.WithdrawOffer(ctx, command)
	if err != nil {
		t.Fatalf("same-operation retry must converge, got %v", err)
	}
	if retry.ID != first.ID {
		t.Errorf("retry produced %q, want the original %q", retry.ID, first.ID)
	}
}

// A reasonless withdrawal is refused.
func TestWithdrawOfferRequiresAReason(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	ctx := context.Background()

	if _, err := rig.service.WithdrawOffer(ctx, WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "   ",
		OperationID:    "op-withdraw-1",
	}); !errors.Is(err, ErrInvalidOfferEligibility) {
		t.Fatalf("reasonless withdrawal error = %v, want a refusal", err)
	}
}

// Withdrawing another tenant's offer version is a tenant-safe not-found.
func TestWithdrawOfferRefusesAForeignVersion(t *testing.T) {
	rig, _ := newWithdrawalRig(t)
	ctx := context.Background()

	if _, err := rig.service.WithdrawOffer(ctx, WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: "offer-version-belonging-to-nobody",
		Reason:         "priced in error",
		OperationID:    "op-withdraw-1",
	}); !errors.Is(err, ErrOfferVersionNotFound) {
		t.Fatalf("foreign withdrawal error = %v, want a tenant-safe not-found", err)
	}
}

// An already-withdrawn offer cannot be withdrawn again by a DIFFERENT
// operation: the first withdrawal is final.
func TestWithdrawOfferRefusesADifferentOperationOnAWithdrawnOffer(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	ctx := context.Background()

	if _, err := rig.service.WithdrawOffer(ctx, WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "priced in error",
		OperationID:    "op-withdraw-1",
	}); err != nil {
		t.Fatalf("first withdrawal: %v", err)
	}

	if _, err := rig.service.WithdrawOffer(ctx, WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "changed my mind again",
		OperationID:    "op-withdraw-2",
	}); !errors.Is(err, ErrOfferEligibilityConflict) {
		t.Fatalf("second withdrawal error = %v, want a conflict", err)
	}
}

// The withdrawal timestamp comes from the request context, keeping recovery
// deterministic rather than drifting on each retry.
func TestWithdrawOfferStampsTheRequestTime(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	ctx := context.Background()

	withdrawal, err := rig.service.WithdrawOffer(ctx, WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "priced in error",
		OperationID:    "op-withdraw-1",
	})
	if err != nil {
		t.Fatalf("WithdrawOffer: %v", err)
	}

	if !withdrawal.WithdrawnAt.Equal(rig.input.AccessedAt) {
		t.Errorf("WithdrawnAt = %v, want the request time %v",
			withdrawal.WithdrawnAt, rig.input.AccessedAt)
	}
}
