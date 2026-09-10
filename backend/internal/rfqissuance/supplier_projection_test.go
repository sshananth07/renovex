package rfqissuance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

type fakeSupplierRFQAuthorizer struct {
	access rfqissuance.AuthorizedSupplierRFQAccess
	err    error
}

func (fake *fakeSupplierRFQAuthorizer) AuthorizeSupplierRFQRead(
	_ context.Context, _ rfqissuance.SupplierRFQReadAuthorization,
) (rfqissuance.AuthorizedSupplierRFQAccess, error) {
	return fake.access, fake.err
}

func supplierProjectionRig(t *testing.T) (*rfqissuance.Service, *invitationTestRig,
	rfqissuance.SupplierInvitation, *fakeSupplierRFQAuthorizer) {
	t.Helper()
	service, rig, invitation := advancementRig(t)
	stored := *rig.invitations.stored[invitation.ID]
	authorizer := &fakeSupplierRFQAuthorizer{access: rfqissuance.AuthorizedSupplierRFQAccess{
		CompanyID: "company-1", SupplierID: stored.SupplierID,
		InvitationID: stored.ID, AccessGeneration: stored.AccessGeneration,
	}}
	service = rig.rebuild(t, rfqissuance.WithSupplierRFQAccessAuthorizer(authorizer))
	return service, rig, stored, authorizer
}

func TestSupplierInvitationProjectionReturnsOnlyCurrentAuthorizedRFQ(t *testing.T) {
	service, _, invitation, _ := supplierProjectionRig(t)

	projection, err := service.GetSupplierInvitationRFQ(context.Background(),
		rfqissuance.SupplierRFQReadInput{
			SessionToken: "session-token", InvitationID: invitation.ID,
			AccessedAt: time.Now().UTC(),
		})
	if err != nil {
		t.Fatalf("GetSupplierInvitationRFQ: %v", err)
	}
	if projection.InvitationID != invitation.ID || projection.Status != "active" ||
		projection.CurrentRFQVersion.ID != invitation.CurrentIssuedRFQVersionID {
		t.Fatalf("projection = %+v", projection)
	}
	if projection.ResponseWindow.Status != "open" || !projection.ResponseWindow.CanRespond {
		t.Errorf("response window = %+v", projection.ResponseWindow)
	}
}

func TestSupplierRFQHistoryIsNewestFirstAndCursorBounded(t *testing.T) {
	service, rig, invitation, authorizer := supplierProjectionRig(t)
	second := issueAmendment(t, service, "supplier-history-v2")
	stored := rig.invitations.stored[invitation.ID]
	authorizer.access.AccessGeneration = stored.AccessGeneration

	read := rfqissuance.SupplierRFQReadInput{
		SessionToken: "session-token", InvitationID: invitation.ID,
		AccessedAt: time.Now().UTC(),
	}
	firstPage, err := service.ListSupplierRFQVersions(context.Background(),
		rfqissuance.SupplierRFQHistoryInput{
			SupplierRFQReadInput: read,
			PageSize:             1,
		})
	if err != nil {
		t.Fatalf("first history page: %v", err)
	}
	if len(firstPage.Versions) != 1 || firstPage.Versions[0].ID != second.ID ||
		firstPage.NextCursor == nil || *firstPage.NextCursor != 2 {
		t.Fatalf("first page = %+v", firstPage)
	}

	secondPage, err := service.ListSupplierRFQVersions(context.Background(),
		rfqissuance.SupplierRFQHistoryInput{
			SupplierRFQReadInput: read,
			PageSize:             1, Cursor: *firstPage.NextCursor,
		})
	if err != nil || len(secondPage.Versions) != 1 ||
		secondPage.Versions[0].VersionNumber != 1 || secondPage.NextCursor != nil {
		t.Fatalf("second page = %+v, %v", secondPage, err)
	}
}

func TestSupplierRFQProjectionFailsClosedAcrossGenerationChange(t *testing.T) {
	service, rig, invitation, _ := supplierProjectionRig(t)
	stored := rig.invitations.stored[invitation.ID]
	stored.AccessGeneration++

	_, err := service.GetSupplierInvitationRFQ(context.Background(),
		rfqissuance.SupplierRFQReadInput{
			SessionToken: "session-token", InvitationID: invitation.ID,
			AccessedAt: time.Now().UTC(),
		})
	if !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
		t.Errorf("generation race error = %v, want ErrSupplierRFQAccessInvalid", err)
	}
}

func TestSupplierRFQVersionDetailRejectsVersionBeyondInvitationPointer(t *testing.T) {
	service, rig, invitation, _ := supplierProjectionRig(t)
	second := issueAmendment(t, service, "supplier-detail-v2")

	// Rewind only this test's invitation snapshot to model a read authorized
	// before a concurrent chain advance. The newer immutable version exists,
	// but it is not yet in the invitation's authorized history.
	stored := rig.invitations.stored[invitation.ID]
	stored.CurrentIssuedRFQVersionID = invitation.CurrentIssuedRFQVersionID

	_, err := service.GetSupplierRFQVersion(context.Background(),
		rfqissuance.SupplierRFQVersionInput{
			SupplierRFQReadInput: rfqissuance.SupplierRFQReadInput{
				SessionToken: "session-token", InvitationID: invitation.ID,
				AccessedAt: time.Now().UTC(),
			},
			VersionID: second.ID,
		})
	if !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
		t.Errorf("future detail error = %v, want ErrSupplierRFQAccessInvalid", err)
	}
}

func TestSupplierRFQVersionDetailReturnsAllowlistedHistoricalVersion(t *testing.T) {
	service, _, invitation, _ := supplierProjectionRig(t)
	first, err := service.GetSupplierRFQVersion(context.Background(),
		rfqissuance.SupplierRFQVersionInput{
			SupplierRFQReadInput: rfqissuance.SupplierRFQReadInput{
				SessionToken: "session-token", InvitationID: invitation.ID,
				AccessedAt: time.Now().UTC(),
			},
			VersionID: invitation.CurrentIssuedRFQVersionID,
		})
	if err != nil {
		t.Fatalf("GetSupplierRFQVersion: %v", err)
	}
	if first.ID != invitation.CurrentIssuedRFQVersionID || first.VersionNumber != 1 ||
		len(first.Lines) == 0 {
		t.Fatalf("detail = %+v", first)
	}
}
