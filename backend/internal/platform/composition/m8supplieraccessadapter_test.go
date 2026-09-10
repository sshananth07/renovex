package composition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

type fakeInvitationAccessSource struct {
	projection rfqissuance.InvitationAccessProjection
	found      bool
	err        error

	gotHash       string
	gotAccessedAt time.Time
	gotIdentity   struct {
		companyID, invitationID string
		accessedAt              time.Time
	}
	gotView struct {
		companyID, invitationID string
		generation              int64
		viewedAt                time.Time
	}
}

func (f *fakeInvitationAccessSource) ResolveInvitationAccessByIdentity(
	_ context.Context, companyID, invitationID string, accessedAt time.Time,
) (rfqissuance.InvitationAccessProjection, bool, error) {
	f.gotIdentity.companyID = companyID
	f.gotIdentity.invitationID = invitationID
	f.gotIdentity.accessedAt = accessedAt
	return f.projection, f.found, f.err
}

func (f *fakeInvitationAccessSource) ResolveInvitationAccessByHash(
	_ context.Context, hash string, accessedAt time.Time,
) (rfqissuance.InvitationAccessProjection, bool, error) {
	f.gotHash = hash
	f.gotAccessedAt = accessedAt
	return f.projection, f.found, f.err
}

func (f *fakeInvitationAccessSource) RecordInvitationViewed(
	_ context.Context, companyID, invitationID string,
	accessGeneration int64, viewedAt time.Time,
) error {
	f.gotView.companyID = companyID
	f.gotView.invitationID = invitationID
	f.gotView.generation = accessGeneration
	f.gotView.viewedAt = viewedAt
	return f.err
}

func TestInvitationAccessAdapterMapsOnlyTheNarrowSupplierSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	source := &fakeInvitationAccessSource{
		found: true,
		projection: rfqissuance.InvitationAccessProjection{
			CompanyID:                 "company-1",
			SupplierID:                "supplier-1",
			InvitationID:              "invitation-1",
			NormalizedRecipientEmail:  "sales@supplier.test",
			AccessGeneration:          4,
			CurrentIssuedRFQVersionID: "version-2",
		},
	}
	adapter := composition.NewInvitationAccessAdapter(source)

	got, found, err := adapter.ResolveInvitationAccess(
		context.Background(), "canonical-hash", now)
	if err != nil || !found {
		t.Fatalf("found/error = %v/%v, want a successful resolution", found, err)
	}
	if got.CompanyID != "company-1" || got.SupplierID != "supplier-1" ||
		got.InvitationID != "invitation-1" ||
		got.NormalizedRecipientEmail != "sales@supplier.test" ||
		got.AccessGeneration != 4 ||
		got.CurrentIssuedRFQVersionID != "version-2" {
		t.Fatalf("mapped snapshot = %#v", got)
	}
	if source.gotHash != "canonical-hash" || !source.gotAccessedAt.Equal(now) {
		t.Fatalf("source input = %q/%v", source.gotHash, source.gotAccessedAt)
	}
}

func TestInvitationAccessAdapterForwardsViewGenerationAndInfrastructureErrors(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	infrastructureErr := errors.New("database unavailable")
	source := &fakeInvitationAccessSource{err: infrastructureErr}
	adapter := composition.NewInvitationAccessAdapter(source)

	err := adapter.RecordInvitationViewed(context.Background(), "company-1",
		"invitation-1", 7, now)
	if !errors.Is(err, infrastructureErr) {
		t.Fatalf("error = %v, want the source infrastructure error", err)
	}
	if source.gotView.companyID != "company-1" ||
		source.gotView.invitationID != "invitation-1" ||
		source.gotView.generation != 7 ||
		!source.gotView.viewedAt.Equal(now) {
		t.Fatalf("forwarded view = %#v", source.gotView)
	}
}

func TestInvitationAccessAdapterClassifiesAConcurrentGenerationChange(t *testing.T) {
	source := &fakeInvitationAccessSource{err: rfqissuance.ErrInvitationNotFound}
	adapter := composition.NewInvitationAccessAdapter(source)

	err := adapter.RecordInvitationViewed(
		context.Background(), "company-1", "invitation-1", 7, time.Now())
	if !errors.Is(err, supplieraccess.ErrInvitationAccessInvalid) {
		t.Fatalf("error = %v, want consumer-owned ErrInvitationAccessInvalid", err)
	}
}

func TestInvitationAccessAdapterRevalidatesExchangeIdentityThroughTheOwner(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 30, 0, 0, time.UTC)
	source := &fakeInvitationAccessSource{
		found: true,
		projection: rfqissuance.InvitationAccessProjection{
			CompanyID: "company-1", SupplierID: "supplier-1",
			InvitationID:              "invitation-1",
			NormalizedRecipientEmail:  "recipient@supplier.test",
			AccessGeneration:          5,
			CurrentIssuedRFQVersionID: "version-3",
		},
	}
	adapter := composition.NewInvitationAccessAdapter(source)

	got, found, err := adapter.ValidateInvitationAccess(
		context.Background(), "company-1", "invitation-1", now)
	if err != nil || !found {
		t.Fatalf("found/error = %v/%v", found, err)
	}
	if got.AccessGeneration != 5 ||
		source.gotIdentity.companyID != "company-1" ||
		source.gotIdentity.invitationID != "invitation-1" ||
		!source.gotIdentity.accessedAt.Equal(now) {
		t.Fatalf("snapshot/source input = %#v/%#v", got, source.gotIdentity)
	}
}

func TestInvitationAccessAdapterSatisfiesSupplierAccessCapabilities(t *testing.T) {
	source := &fakeInvitationAccessSource{}
	adapter := composition.NewInvitationAccessAdapter(source)

	var _ supplieraccess.InvitationAccessResolver = adapter
	var _ supplieraccess.InvitationViewRecorder = adapter
	var _ supplieraccess.InvitationAccessValidator = adapter
}
