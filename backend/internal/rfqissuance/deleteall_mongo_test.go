package rfqissuance_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// TestService_DeleteAllForCompany proves Task 1a's multi-collection
// guidance (spec §6.6): ONE Service.DeleteAllForCompany call removes
// IssuedRFQVersion, IssuanceChain, RFQAmendmentDraft, SupplierInvitation,
// AND DeliveryAttempt records for companyID — all five collections. Seeds
// via every repository directly, reusing the fixtures each sibling
// *_repository_mongo_test.go file already establishes in this same
// package (rfqissuance_test).
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	versionRepo := newIssuedVersionRepo(t, db)
	chainRepo := newChainRepo(t, db)
	draftRepo := newDraftRepo(t, db)
	invitationRepo := newInvitationRepo(t, db)
	deliveryRepo := newDeliveryRepo(t, db)
	svc := rfqissuance.NewService(versionRepo,
		rfqissuance.WithIssuanceChains(chainRepo),
		rfqissuance.WithAmendmentDrafts(draftRepo),
		rfqissuance.WithInvitations(invitationRepo),
		rfqissuance.WithDeliveryAttempts(deliveryRepo),
	)
	ctx := context.Background()

	line, err := rfqissuance.NewIssuedLineFromM7(readyLine("mr-1", "material-1"))
	if err != nil {
		t.Fatalf("building line: %v", err)
	}
	responseDeadline := time.Now().Add(72 * time.Hour)
	buildVersion := func(companyID, chainID, opID string) rfqissuance.IssuedRFQVersion {
		v, err := rfqissuance.NewIssuedVersion(rfqissuance.NewIssuedVersionInput{
			CompanyID: companyID, ProjectID: "project-1", RFQChainID: chainID,
			RFQNumber: "RFQ-000001", VersionNumber: 1, Currency: "MYR",
			Title: "Cement and aggregate", DeliveryAddress: "12 Site Road",
			ResponseDeadline:    &responseDeadline,
			Lines:               []rfqissuance.IssuedRFQLine{line},
			IssuedByUserID:      "user-1",
			IssuanceOperationID: opID,
		})
		if err != nil {
			t.Fatalf("building version: %v", err)
		}
		return v
	}

	if _, err := versionRepo.CreateVersion(ctx, buildVersion("company_a", "chain_a", "op-a")); err != nil {
		t.Fatalf("create company_a version: %v", err)
	}
	if _, err := versionRepo.CreateVersion(ctx, buildVersion("company_b", "chain_b", "op-b")); err != nil {
		t.Fatalf("create company_b version: %v", err)
	}
	if _, err := chainRepo.EnsureChain(ctx, "company_a", "chain_a"); err != nil {
		t.Fatalf("create company_a chain: %v", err)
	}
	if _, err := chainRepo.EnsureChain(ctx, "company_b", "chain_b"); err != nil {
		t.Fatalf("create company_b chain: %v", err)
	}
	if _, err := draftRepo.CreateDraft(ctx, draftFixture(t, "company_a", "chain_a")); err != nil {
		t.Fatalf("create company_a draft: %v", err)
	}
	if _, err := draftRepo.CreateDraft(ctx, draftFixture(t, "company_b", "chain_b")); err != nil {
		t.Fatalf("create company_b draft: %v", err)
	}
	invitationA, err := invitationRepo.CreateInvitation(ctx, invitationFixture(t, "company_a", "chain_a", "supplier_a"))
	if err != nil {
		t.Fatalf("create company_a invitation: %v", err)
	}
	invitationB, err := invitationRepo.CreateInvitation(ctx, invitationFixture(t, "company_b", "chain_b", "supplier_b"))
	if err != nil {
		t.Fatalf("create company_b invitation: %v", err)
	}
	if _, err := deliveryRepo.CreateAttempt(ctx, deliveryFixture("company_a", invitationA.ID, "delivery-op-a")); err != nil {
		t.Fatalf("create company_a delivery attempt: %v", err)
	}
	if _, err := deliveryRepo.CreateAttempt(ctx, deliveryFixture("company_b", invitationB.ID, "delivery-op-b")); err != nil {
		t.Fatalf("create company_b delivery attempt: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	versionsA, err := versionRepo.ListVersions(ctx, "company_a", "chain_a")
	if err != nil {
		t.Fatalf("ListVersions company_a: %v", err)
	}
	if len(versionsA) != 0 {
		t.Fatalf("expected 0 remaining company_a versions, got %d", len(versionsA))
	}
	if _, err := chainRepo.FindChain(ctx, "company_a", "chain_a"); err != rfqissuance.ErrIssuanceChainNotFound {
		t.Fatalf("expected ErrIssuanceChainNotFound for company_a's deleted chain, got %v", err)
	}
	if _, err := draftRepo.FindDraft(ctx, "company_a", "chain_a"); err != rfqissuance.ErrAmendmentDraftNotFound {
		t.Fatalf("expected ErrAmendmentDraftNotFound for company_a's deleted draft, got %v", err)
	}
	if _, err := invitationRepo.FindInvitation(ctx, "company_a", invitationA.ID); err != rfqissuance.ErrInvitationNotFound {
		t.Fatalf("expected ErrInvitationNotFound for company_a's deleted invitation, got %v", err)
	}
	attemptsA, err := deliveryRepo.ListAttemptsForInvitation(ctx, "company_a", invitationA.ID)
	if err != nil {
		t.Fatalf("ListAttemptsForInvitation company_a: %v", err)
	}
	if len(attemptsA) != 0 {
		t.Fatalf("expected 0 remaining company_a delivery attempts, got %d", len(attemptsA))
	}

	versionsB, err := versionRepo.ListVersions(ctx, "company_b", "chain_b")
	if err != nil {
		t.Fatalf("ListVersions company_b: %v", err)
	}
	if len(versionsB) != 1 {
		t.Fatalf("expected company_b's version to be untouched, got %d", len(versionsB))
	}
	if _, err := chainRepo.FindChain(ctx, "company_b", "chain_b"); err != nil {
		t.Fatalf("expected company_b's chain to be untouched, got %v", err)
	}
	if _, err := draftRepo.FindDraft(ctx, "company_b", "chain_b"); err != nil {
		t.Fatalf("expected company_b's draft to be untouched, got %v", err)
	}
	if _, err := invitationRepo.FindInvitation(ctx, "company_b", invitationB.ID); err != nil {
		t.Fatalf("expected company_b's invitation to be untouched, got %v", err)
	}
	attemptsB, err := deliveryRepo.ListAttemptsForInvitation(ctx, "company_b", invitationB.ID)
	if err != nil {
		t.Fatalf("ListAttemptsForInvitation company_b: %v", err)
	}
	if len(attemptsB) != 1 {
		t.Fatalf("expected company_b's delivery attempt to be untouched, got %d", len(attemptsB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupDB(t)
	versionRepo := newIssuedVersionRepo(t, db)
	chainRepo := newChainRepo(t, db)
	draftRepo := newDraftRepo(t, db)
	invitationRepo := newInvitationRepo(t, db)
	deliveryRepo := newDeliveryRepo(t, db)
	svc := rfqissuance.NewService(versionRepo,
		rfqissuance.WithIssuanceChains(chainRepo),
		rfqissuance.WithAmendmentDrafts(draftRepo),
		rfqissuance.WithInvitations(invitationRepo),
		rfqissuance.WithDeliveryAttempts(deliveryRepo),
	)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
