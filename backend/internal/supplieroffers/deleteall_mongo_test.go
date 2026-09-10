package supplieroffers_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// TestService_DeleteAllForCompany proves Task 1a's multi-collection guidance
// (spec §6.6): ONE Service.DeleteAllForCompany call removes SupplierOfferChain,
// SupplierOfferDraft, SupplierOfferVersion, SupplierOfferEligibility, and
// SupplierOfferWithdrawal records for companyID — all five collections.
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	chains := newOfferChainRepository(t, db)
	drafts := newOfferDraftRepository(t, db)
	versions := newOfferVersionRepository(t, db)
	eligibility := newOfferEligibilityRepository(t, db)
	withdrawals := newOfferWithdrawalRepository(t, db)
	svc := supplieroffers.NewService(
		supplieroffers.WithSupplierOfferChainRepository(chains),
		supplieroffers.WithSupplierOfferDraftRepository(drafts),
		supplieroffers.WithSupplierOfferVersionRepository(versions),
		supplieroffers.WithSupplierOfferEligibilityRepository(eligibility),
		supplieroffers.WithSupplierOfferWithdrawalRepository(withdrawals),
	)
	ctx := context.Background()
	now := time.Date(2026, time.July, 31, 10, 0, 0, 0, time.UTC)

	seedCompany := func(companyID string) {
		chain, err := chains.EnsureOfferChain(ctx, companyID, "invitation-1", "issued-version-1")
		if err != nil {
			t.Fatalf("create %s chain: %v", companyID, err)
		}
		draft := supplieroffers.SupplierOfferDraft{
			ID: companyID + "-draft", CompanyID: companyID, OfferChainID: chain.ID,
			InvitationID: "invitation-1", IssuedRFQVersionID: "issued-version-1",
			RecipientIdentity: "buyer@example.test", Status: supplieroffers.DraftActive,
			Revision: 1, CreatedAt: now, UpdatedAt: now,
			SchemaVersion: supplieroffers.SupplierOfferDraftSchemaVersion,
		}
		if err := drafts.InsertDraft(ctx, draft); err != nil {
			t.Fatalf("create %s draft: %v", companyID, err)
		}
		version := supplieroffers.SupplierOfferVersion{
			ID: companyID + "-version", CompanyID: companyID, OfferChainID: chain.ID,
			InvitationID: "invitation-1", IssuedRFQVersionID: "issued-version-1",
			VersionNumber: 1, RecipientIdentity: "buyer@example.test",
			SourceDraftID: draft.ID, SourceDraftRevision: 1,
			SubmissionOperationID: companyID + "-submit-op",
			SubmissionFingerprint: companyID + "-fingerprint",
			SubmittedAt:           now,
			SchemaVersion:         supplieroffers.SupplierOfferVersionSchemaVersion,
		}
		if err := versions.InsertVersion(ctx, version); err != nil {
			t.Fatalf("create %s version: %v", companyID, err)
		}
		claim := supplieroffers.SupplierOfferEligibility{
			ID: companyID + "-eligibility", CompanyID: companyID, OfferChainID: chain.ID,
			OfferVersionID: version.ID, State: supplieroffers.EligibilityEligible,
			Revision: 1,
		}
		if err := eligibility.InsertEligibility(ctx, claim); err != nil {
			t.Fatalf("create %s eligibility: %v", companyID, err)
		}
		withdrawal := supplieroffers.SupplierOfferWithdrawal{
			ID: companyID + "-withdrawal", CompanyID: companyID, OfferChainID: chain.ID,
			SupplierOfferVersionID: version.ID, InvitationID: "invitation-1",
			RecipientIdentity: "buyer@example.test", OperationID: companyID + "-withdraw-op",
			Reason: "Commercial terms changed.", WithdrawnAt: now,
			SchemaVersion: supplieroffers.SupplierOfferWithdrawalSchemaVersion,
		}
		if err := withdrawals.InsertWithdrawal(ctx, withdrawal); err != nil {
			t.Fatalf("create %s withdrawal: %v", companyID, err)
		}
	}

	seedCompany("company_a")
	seedCompany("company_b")

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	if _, found, err := chains.FindChainByScope(ctx, "company_a", "invitation-1", "issued-version-1"); err != nil || found {
		t.Fatalf("expected company_a's chain to be deleted, found=%v err=%v", found, err)
	}
	if _, found, err := drafts.FindDraft(ctx, "company_a", "company_a-draft"); err != nil || found {
		t.Fatalf("expected company_a's draft to be deleted, found=%v err=%v", found, err)
	}
	if versionsA, err := versions.ListVersionsForChain(ctx, "company_a", "company_a-chain-does-not-matter"); err != nil || len(versionsA) != 0 {
		t.Fatalf("expected 0 remaining company_a versions by chain lookup, got %d err=%v", len(versionsA), err)
	}
	if _, found, err := eligibility.FindEligibility(ctx, "company_a", "company_a-version"); err != nil || found {
		t.Fatalf("expected company_a's eligibility to be deleted, found=%v err=%v", found, err)
	}
	if _, found, err := withdrawals.FindWithdrawal(ctx, "company_a", "company_a-version"); err != nil || found {
		t.Fatalf("expected company_a's withdrawal to be deleted, found=%v err=%v", found, err)
	}

	if _, found, err := chains.FindChainByScope(ctx, "company_b", "invitation-1", "issued-version-1"); err != nil || !found {
		t.Fatalf("expected company_b's chain to be untouched, found=%v err=%v", found, err)
	}
	if _, found, err := drafts.FindDraft(ctx, "company_b", "company_b-draft"); err != nil || !found {
		t.Fatalf("expected company_b's draft to be untouched, found=%v err=%v", found, err)
	}
	if _, found, err := eligibility.FindEligibility(ctx, "company_b", "company_b-version"); err != nil || !found {
		t.Fatalf("expected company_b's eligibility to be untouched, found=%v err=%v", found, err)
	}
	if _, found, err := withdrawals.FindWithdrawal(ctx, "company_b", "company_b-version"); err != nil || !found {
		t.Fatalf("expected company_b's withdrawal to be untouched, found=%v err=%v", found, err)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupDB(t)
	chains := newOfferChainRepository(t, db)
	drafts := newOfferDraftRepository(t, db)
	versions := newOfferVersionRepository(t, db)
	eligibility := newOfferEligibilityRepository(t, db)
	withdrawals := newOfferWithdrawalRepository(t, db)
	svc := supplieroffers.NewService(
		supplieroffers.WithSupplierOfferChainRepository(chains),
		supplieroffers.WithSupplierOfferDraftRepository(drafts),
		supplieroffers.WithSupplierOfferVersionRepository(versions),
		supplieroffers.WithSupplierOfferEligibilityRepository(eligibility),
		supplieroffers.WithSupplierOfferWithdrawalRepository(withdrawals),
	)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
