package awards_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// TestService_DeleteAllForCompany proves Task 1a's multi-collection guidance
// (spec §6.6): ONE Service.DeleteAllForCompany call removes AwardDecisionChain,
// AwardDraft, AwardLineClaim, AwardRevision, AwardOutcome,
// AwardOutcomeDelivery, and AwardOutcomeAcknowledgement records for
// companyID — all seven collections.
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	chains := awards.NewMongoAwardChainRepository(db)
	drafts := awards.NewMongoAwardDraftRepository(db)
	lineClaims := awards.NewMongoAwardLineClaimRepository(db)
	revisions := awards.NewMongoAwardRevisionRepository(db)
	outcomes := awards.NewMongoAwardOutcomeRepository(db)
	deliveries := awards.NewMongoAwardDeliveryRepository(db)
	acknowledgements := awards.NewMongoAwardAcknowledgementRepository(db)
	ctx := context.Background()
	for _, ensure := range []func(context.Context) error{
		chains.EnsureIndexes, drafts.EnsureIndexes, lineClaims.EnsureIndexes,
		revisions.EnsureIndexes, outcomes.EnsureIndexes, deliveries.EnsureIndexes,
		acknowledgements.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			t.Fatalf("ensuring indexes: %v", err)
		}
	}

	svc := awards.NewService(
		awards.WithAwardChainRepository(chains),
		awards.WithAwardDraftRepository(drafts),
		awards.WithAwardLineClaimRepository(lineClaims),
		awards.WithAwardRevisionRepository(revisions),
		awards.WithAwardOutcomeRepository(outcomes),
		awards.WithAwardDeliveryRepository(deliveries),
		awards.WithAwardAcknowledgementRepository(acknowledgements),
	)

	type seeded struct {
		chainID    string
		revisionID string
		outcomeID  string
	}

	seedCompany := func(companyID string) seeded {
		chain, err := chains.EnsureAwardChain(ctx, companyID, "rfqchain-1", "issued-1")
		if err != nil {
			t.Fatalf("create %s chain: %v", companyID, err)
		}
		if _, _, err := drafts.EnsureOpenDraft(ctx, awards.AwardDraft{
			CompanyID: companyID, AwardChainID: chain.ID,
			IssuedRFQVersionID: "issued-1", CreatedByUserID: "user-1",
		}); err != nil {
			t.Fatalf("create %s draft: %v", companyID, err)
		}
		if _, err := lineClaims.ClaimLineage(ctx, awards.AwardLineClaimInput{
			CompanyID: companyID, RFQChainID: "rfqchain-1",
			StableLineageID:     "lineage-1",
			IssuedRFQVersionID:  "issued-1",
			AwardOperationID:    companyID + "-award-op",
			CandidateRevisionID: companyID + "-candidate-revision",
		}); err != nil {
			t.Fatalf("create %s line claim: %v", companyID, err)
		}
		revision, err := revisions.InsertRevision(ctx, awards.AwardRevision{
			CompanyID: companyID, AwardChainID: chain.ID,
			RFQChainID: "rfqchain-1", IssuedRFQVersionID: "issued-1",
			RevisionNumber:          1,
			FinalisationOperationID: companyID + "-finalise-op",
			SelectionFingerprint:    companyID + "-fingerprint",
			AwardedLines: []awards.AwardedLine{{
				IssuedRFQLineID: "line-1", StableLineageID: "lineage-1",
				SupplierID: "supplier-a", OfferVersionID: "offer-1",
				OfferLineID:  "ol-1",
				LineSubtotal: money.New(10_000, "MYR"),
			}},
			GrandAwardTotal:   money.New(10_000, "MYR"),
			FinalisedByUserID: "user-1",
			FinalisedAt:       time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("create %s revision: %v", companyID, err)
		}
		outcome, _, err := outcomes.EnsureOutcome(ctx, awards.AwardOutcome{
			CompanyID: companyID, AwardChainID: chain.ID,
			AwardRevisionID: revision.ID, RFQChainID: "rfqchain-1",
			IssuedRFQVersionID: "issued-1",
			SupplierID:         "supplier-a", InvitationID: "invitation-a",
			Result: awards.OutcomeSelected,
			Projection: awards.OutcomeProjection{
				Result:     awards.OutcomeSelected,
				AwardTotal: money.New(10_000, "MYR"),
			},
		})
		if err != nil {
			t.Fatalf("create %s outcome: %v", companyID, err)
		}
		if _, _, err := deliveries.EnsureDeliveryIntent(ctx, awards.AwardOutcomeDelivery{
			CompanyID: companyID, AwardOutcomeID: outcome.ID,
			AwardRevisionID: revision.ID, SupplierID: "supplier-a",
			RecipientIdentity: "buyer@example.test", AccessGeneration: 1,
			DeliveryOperationID: companyID + "-delivery-op",
			Channel:             awards.DeliveryChannelEmail,
			Status:              awards.DeliveryPending,
		}); err != nil {
			t.Fatalf("create %s delivery: %v", companyID, err)
		}
		if _, _, err := acknowledgements.EnsureAcknowledgement(ctx, awards.AwardOutcomeAcknowledgement{
			CompanyID: companyID, AwardOutcomeID: outcome.ID,
			SupplierID: "supplier-a", InvitationID: "invitation-a",
			SessionID: "session-1", RecipientIdentity: "buyer@example.test",
			OperationID: companyID + "-ack-op", AcknowledgedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("create %s acknowledgement: %v", companyID, err)
		}
		return seeded{chainID: chain.ID, revisionID: revision.ID, outcomeID: outcome.ID}
	}

	seedA := seedCompany("company_a")
	seedB := seedCompany("company_b")

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	if _, found, err := chains.FindChainByIssuedVersion(ctx, "company_a", "issued-1"); err != nil || found {
		t.Fatalf("expected company_a's chain to be deleted, found=%v err=%v", found, err)
	}
	if _, found, err := drafts.FindOpenDraft(ctx, "company_a", seedA.chainID); err != nil || found {
		t.Fatalf("expected company_a's draft to be deleted, found=%v err=%v", found, err)
	}
	if _, found, err := lineClaims.FindClaimByLineage(ctx, "company_a", "rfqchain-1", "lineage-1"); err != nil || found {
		t.Fatalf("expected company_a's line claim to be deleted, found=%v err=%v", found, err)
	}
	if revisionsA, err := revisions.ListRevisions(ctx, "company_a", seedA.chainID); err != nil || len(revisionsA) != 0 {
		t.Fatalf("expected 0 remaining company_a revisions, got %d err=%v", len(revisionsA), err)
	}
	if outcomesA, err := outcomes.ListOutcomes(ctx, "company_a", seedA.revisionID); err != nil || len(outcomesA) != 0 {
		t.Fatalf("expected 0 remaining company_a outcomes, got %d err=%v", len(outcomesA), err)
	}
	if _, found, err := deliveries.FindDeliveryByOperation(ctx, "company_a", "company_a-delivery-op"); err != nil || found {
		t.Fatalf("expected company_a's delivery to be deleted, found=%v err=%v", found, err)
	}
	if _, found, err := acknowledgements.FindAcknowledgement(ctx, "company_a", seedA.outcomeID); err != nil || found {
		t.Fatalf("expected company_a's acknowledgement to be deleted, found=%v err=%v", found, err)
	}

	if _, found, err := chains.FindChainByIssuedVersion(ctx, "company_b", "issued-1"); err != nil || !found {
		t.Fatalf("expected company_b's chain to be untouched, found=%v err=%v", found, err)
	}
	if _, found, err := drafts.FindOpenDraft(ctx, "company_b", seedB.chainID); err != nil || !found {
		t.Fatalf("expected company_b's draft to be untouched, found=%v err=%v", found, err)
	}
	if _, found, err := lineClaims.FindClaimByLineage(ctx, "company_b", "rfqchain-1", "lineage-1"); err != nil || !found {
		t.Fatalf("expected company_b's line claim to be untouched, found=%v err=%v", found, err)
	}
	if revisionsB, err := revisions.ListRevisions(ctx, "company_b", seedB.chainID); err != nil || len(revisionsB) != 1 {
		t.Fatalf("expected company_b's revision to be untouched, got %d err=%v", len(revisionsB), err)
	}
	if outcomesB, err := outcomes.ListOutcomes(ctx, "company_b", seedB.revisionID); err != nil || len(outcomesB) != 1 {
		t.Fatalf("expected company_b's outcome to be untouched, got %d err=%v", len(outcomesB), err)
	}
	if _, found, err := acknowledgements.FindAcknowledgement(ctx, "company_b", seedB.outcomeID); err != nil || !found {
		t.Fatalf("expected company_b's acknowledgement to be untouched, found=%v err=%v", found, err)
	}
	if _, found, err := deliveries.FindDeliveryByOperation(ctx, "company_b", "company_b-delivery-op"); err != nil || !found {
		t.Fatalf("expected company_b's delivery to be untouched, found=%v err=%v", found, err)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupDB(t)
	chains := awards.NewMongoAwardChainRepository(db)
	drafts := awards.NewMongoAwardDraftRepository(db)
	lineClaims := awards.NewMongoAwardLineClaimRepository(db)
	revisions := awards.NewMongoAwardRevisionRepository(db)
	outcomes := awards.NewMongoAwardOutcomeRepository(db)
	deliveries := awards.NewMongoAwardDeliveryRepository(db)
	acknowledgements := awards.NewMongoAwardAcknowledgementRepository(db)
	ctx := context.Background()
	for _, ensure := range []func(context.Context) error{
		chains.EnsureIndexes, drafts.EnsureIndexes, lineClaims.EnsureIndexes,
		revisions.EnsureIndexes, outcomes.EnsureIndexes, deliveries.EnsureIndexes,
		acknowledgements.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			t.Fatalf("ensuring indexes: %v", err)
		}
	}

	svc := awards.NewService(
		awards.WithAwardChainRepository(chains),
		awards.WithAwardDraftRepository(drafts),
		awards.WithAwardLineClaimRepository(lineClaims),
		awards.WithAwardRevisionRepository(revisions),
		awards.WithAwardOutcomeRepository(outcomes),
		awards.WithAwardDeliveryRepository(deliveries),
		awards.WithAwardAcknowledgementRepository(acknowledgements),
	)

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
