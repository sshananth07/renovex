package rfqissuance_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

func activeAccessInvitation(now time.Time) rfqissuance.SupplierInvitation {
	return rfqissuance.SupplierInvitation{
		ID:                        "invitation-1",
		CompanyID:                 "company-1",
		RFQChainID:                "chain-1",
		SupplierID:                "supplier-1",
		CurrentIssuedRFQVersionID: "version-2",
		RecipientEmailNormalized:  "sales@supplier.test",
		Status:                    rfqissuance.InvitationStatusActive,
		ExpiresAt:                 now.Add(time.Hour),
		AccessSecretHash:          secrets.HashInvitationSecret("current-token"),
		AccessGeneration:          3,
	}
}

func TestResolveInvitationAccessByHashReturnsOnlyTheCurrentSupplierIdentity(t *testing.T) {
	svc, rig := newInvitationService(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	invitation := activeAccessInvitation(now)
	rig.invitations.stored[invitation.ID] = &invitation

	got, found, err := svc.ResolveInvitationAccessByHash(
		context.Background(), invitation.AccessSecretHash, now)
	if err != nil {
		t.Fatalf("resolving current invitation: %v", err)
	}
	if !found {
		t.Fatal("a current active invitation was not resolved")
	}
	if got.CompanyID != invitation.CompanyID ||
		got.SupplierID != invitation.SupplierID ||
		got.InvitationID != invitation.ID ||
		got.NormalizedRecipientEmail != invitation.RecipientEmailNormalized ||
		got.AccessGeneration != invitation.AccessGeneration ||
		got.CurrentIssuedRFQVersionID != invitation.CurrentIssuedRFQVersionID {
		t.Fatalf("resolved snapshot = %#v, want the narrow current identity", got)
	}
}

func TestResolveInvitationAccessByHashCollapsesCredentialAndInvitationFailures(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		mutate func(*rfqissuance.SupplierInvitation, *invitationTestRig)
		hash   string
	}{
		{"unknown hash", func(*rfqissuance.SupplierInvitation, *invitationTestRig) {}, "unknown"},
		{"revoked", func(i *rfqissuance.SupplierInvitation, _ *invitationTestRig) {
			revokedAt := now.Add(-time.Minute)
			i.RevokedAt = &revokedAt
			i.Status = rfqissuance.InvitationStatusRevoked
		}, ""},
		{"expired", func(i *rfqissuance.SupplierInvitation, _ *invitationTestRig) {
			i.ExpiresAt = now
		}, ""},
		{"missing recipient identity", func(i *rfqissuance.SupplierInvitation, _ *invitationTestRig) {
			i.RecipientEmailNormalized = ""
		}, ""},
		{"missing supplier", func(_ *rfqissuance.SupplierInvitation, rig *invitationTestRig) {
			rig.suppliers.invitable = false
		}, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, rig := newInvitationService(t)
			invitation := activeAccessInvitation(now)
			tc.mutate(&invitation, rig)
			rig.invitations.stored[invitation.ID] = &invitation

			hash := tc.hash
			if hash == "" {
				hash = invitation.AccessSecretHash
			}
			if _, found, err := svc.ResolveInvitationAccessByHash(
				context.Background(), hash, now); err != nil || found {
				t.Fatalf("found/error = %v/%v, want the same neutral not-found result", found, err)
			}
		})
	}
}

func TestRecordInvitationViewedUsesTheExactValidatedGeneration(t *testing.T) {
	svc, rig := newInvitationService(t)
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	invitation := activeAccessInvitation(now)
	rig.invitations.stored[invitation.ID] = &invitation

	viewedAt := now.Add(time.Minute)
	if err := svc.RecordInvitationViewed(context.Background(), invitation.CompanyID,
		invitation.ID, invitation.AccessGeneration, viewedAt); err != nil {
		t.Fatalf("recording current-generation view: %v", err)
	}
	if stored := rig.invitations.stored[invitation.ID]; stored.FirstViewedAt == nil ||
		!stored.FirstViewedAt.Equal(viewedAt) || stored.LastViewedAt == nil ||
		!stored.LastViewedAt.Equal(viewedAt) {
		t.Fatalf("view timestamps = %v/%v, want %v",
			stored.FirstViewedAt, stored.LastViewedAt, viewedAt)
	}

	if err := svc.RecordInvitationViewed(context.Background(), invitation.CompanyID,
		invitation.ID, invitation.AccessGeneration-1, viewedAt.Add(time.Minute)); err == nil {
		t.Fatal("an obsolete generation marked the current invitation as viewed")
	}
}

func TestResolveInvitationAccessByIdentityRevalidatesAnExchangeSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	service, rig := newInvitationService(t)
	invitation := activeAccessInvitation(now)
	rig.invitations.stored[invitation.ID] = &invitation

	resolved, found, err := service.ResolveInvitationAccessByIdentity(
		context.Background(), invitation.CompanyID, invitation.ID, now)
	if err != nil || !found {
		t.Fatalf("found/error = %v/%v, want true/nil", found, err)
	}
	if resolved.CompanyID != invitation.CompanyID ||
		resolved.SupplierID != invitation.SupplierID ||
		resolved.InvitationID != invitation.ID ||
		resolved.NormalizedRecipientEmail != invitation.RecipientEmailNormalized ||
		resolved.AccessGeneration != invitation.AccessGeneration {
		t.Fatalf("revalidated projection = %#v", resolved)
	}

	invitation.Status = rfqissuance.InvitationStatusRevoked
	rig.invitations.stored[invitation.ID] = &invitation
	if _, found, err := service.ResolveInvitationAccessByIdentity(
		context.Background(), invitation.CompanyID, invitation.ID, now); err != nil || found {
		t.Fatalf("revoked found/error = %v/%v, want false/nil", found, err)
	}
	if _, found, err := service.ResolveInvitationAccessByIdentity(
		context.Background(), "foreign-company", invitation.ID, now); err != nil || found {
		t.Fatalf("foreign found/error = %v/%v, want false/nil", found, err)
	}
}
