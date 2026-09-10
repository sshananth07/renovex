package supplieroffers

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// submissionRig extends the edit rig with the version, chain and eligibility
// repositories submission needs, and leaves the draft ready to submit.
type submissionRig struct {
	*draftEditRig
	versions    *MongoOfferVersionRepository
	chains      *MongoOfferChainRepository
	eligibility *MongoOfferEligibilityRepository
	submitInput SubmitOfferCommand
	readyDraft  SupplierOfferDraft
}

func newSubmissionRig(t *testing.T) *submissionRig {
	t.Helper()
	base := newDraftEditRig(t)
	ctx := context.Background()

	versions := NewMongoOfferVersionRepository(base.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	chains := NewMongoOfferChainRepository(base.db)
	eligibility := NewMongoOfferEligibilityRepository(base.db)
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}

	base.service.versions = versions
	base.service.eligibility = eligibility

	// Price the single line and set a validity date so the draft is complete.
	quoted, err := base.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context:          base.input,
		DraftID:          base.draft.ID,
		ExpectedRevision: base.draft.Revision,
		DraftLineID:      base.draft.Lines[0].ID,
		UnitPriceMinor:   2_000,
	})
	if err != nil {
		t.Fatalf("quoting the line: %v", err)
	}

	validUntil := base.now.Add(30 * 24 * time.Hour)
	ready, err := base.service.SetOfferValidity(ctx, SetOfferValidityCommand{
		Context:          base.input,
		DraftID:          base.draft.ID,
		ExpectedRevision: quoted.Revision,
		OfferValidUntil:  validUntil,
	})
	if err != nil {
		t.Fatalf("setting offer validity: %v", err)
	}

	return &submissionRig{
		draftEditRig: base,
		versions:     versions,
		chains:       chains,
		eligibility:  eligibility,
		readyDraft:   ready,
		submitInput: SubmitOfferCommand{
			Context:          base.input,
			DraftID:          base.draft.ID,
			ExpectedRevision: ready.Revision,
			OperationID:      "op-submit-1",
		},
	}
}

// A complete submission produces the immutable version, its eligibility gate,
// an advanced chain, and an archived draft.
func TestSubmitOfferCompletesEveryStep(t *testing.T) {
	rig := newSubmissionRig(t)
	ctx := context.Background()

	version, err := rig.service.SubmitOffer(ctx, rig.submitInput)
	if err != nil {
		t.Fatalf("SubmitOffer: %v", err)
	}

	if version.VersionNumber != 1 {
		t.Errorf("VersionNumber = %d, want the first version", version.VersionNumber)
	}
	if version.GrandTotal != money.New(20_000, Phase1Currency) {
		t.Errorf("GrandTotal = %v, want the server-calculated 20000",
			version.GrandTotal)
	}
	// SourceDraftRevision is the revision whose CONTENT was fingerprinted, not
	// the incremented revision created by the claim.
	if version.SourceDraftRevision != rig.readyDraft.Revision {
		t.Errorf("SourceDraftRevision = %d, want the fingerprinted %d",
			version.SourceDraftRevision, rig.readyDraft.Revision)
	}

	gate, found, err := rig.eligibility.FindEligibility(ctx, "company-1", version.ID)
	if err != nil || !found {
		t.Fatalf("eligibility found = %v, err = %v", found, err)
	}
	if gate.State != EligibilityEligible {
		t.Errorf("eligibility state = %q, want the initial eligible", gate.State)
	}

	chain, err := rig.chains.EnsureOfferChain(
		ctx, "company-1", "invitation-1", "issued-version-2")
	if err != nil {
		t.Fatalf("reading the chain: %v", err)
	}
	if chain.LatestSubmittedID == nil || *chain.LatestSubmittedID != version.ID {
		t.Errorf("chain latest = %v, want the submitted version", chain.LatestSubmittedID)
	}

	archived, found, err := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading the draft: %v", err)
	}
	if archived.Status != DraftArchived {
		t.Errorf("draft status = %q, want archived after submission", archived.Status)
	}
}

// A same-operation retry returns the SAME version rather than creating a second
// one or allocating a new number.
func TestSubmitOfferIsIdempotentForTheSameOperation(t *testing.T) {
	rig := newSubmissionRig(t)
	ctx := context.Background()

	first, err := rig.service.SubmitOffer(ctx, rig.submitInput)
	if err != nil {
		t.Fatalf("first submission: %v", err)
	}
	retry, err := rig.service.SubmitOffer(ctx, rig.submitInput)
	if err != nil {
		t.Fatalf("same-operation retry must converge, got %v", err)
	}

	if retry.ID != first.ID || retry.VersionNumber != first.VersionNumber {
		t.Errorf("retry produced %s/#%d, want the original %s/#%d",
			retry.ID, retry.VersionNumber, first.ID, first.VersionNumber)
	}
}

// A submission that fails validation must NOT claim the draft: claiming for a
// submission that cannot complete would freeze the Supplier's workspace.
func TestSubmitOfferDoesNotClaimTheDraftWhenValidationFails(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	rig.service.versions = versions

	// The line is still unanswered and validity is unset.
	if _, err := rig.service.SubmitOffer(ctx, SubmitOfferCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
		OperationID:      "op-submit-1",
	}); err == nil {
		t.Fatal("an incomplete offer must not submit")
	}

	current, found, err := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if err != nil || !found {
		t.Fatalf("reloading the draft: %v", err)
	}
	if current.Status != DraftActive {
		t.Errorf("draft status = %q, want it to remain active and editable",
			current.Status)
	}
}

// A different operation cannot take over a submitting claim.
func TestSubmitOfferRejectsADifferentOperationAgainstAClaim(t *testing.T) {
	rig := newSubmissionRig(t)
	ctx := context.Background()

	// Claim the draft directly, simulating an in-flight submission whose
	// completion has not yet run.
	if _, err := rig.drafts.ClaimDraftForSubmission(ctx, DraftSubmissionClaimInput{
		CompanyID:             "company-1",
		DraftID:               rig.draft.ID,
		ExpectedRevision:      rig.readyDraft.Revision,
		SubmissionOperationID: "op-submit-1",
		ClaimedAt:             rig.now,
	}); err != nil {
		t.Fatalf("claiming the draft: %v", err)
	}

	other := rig.submitInput
	other.OperationID = "op-submit-OTHER"
	other.ExpectedRevision = rig.readyDraft.Revision + 1
	if _, err := rig.service.SubmitOffer(ctx, other); !errors.Is(
		err, ErrOfferDraftConflict) {
		t.Fatalf("a different operation error = %v, want conflict", err)
	}
}

// Recovery: the draft is claimed but the version was never inserted. The retry
// inserts the RECORDED candidate identity rather than allocating a new one.
func TestSubmitOfferRecoversWhenTheVersionWasNeverInserted(t *testing.T) {
	rig := newSubmissionRig(t)
	ctx := context.Background()

	calculated, err := CalculateSubmission(SubmissionCalculationInput{
		Draft:       rig.readyDraft,
		RFQ:         rig.issuedSnapshot(),
		SubmittedAt: rig.now,
	})
	if err != nil {
		t.Fatalf("pre-calculating: %v", err)
	}

	claimed, err := rig.drafts.ClaimDraftForSubmission(ctx, DraftSubmissionClaimInput{
		CompanyID:               "company-1",
		DraftID:                 rig.draft.ID,
		ExpectedRevision:        rig.readyDraft.Revision,
		SubmissionOperationID:   "op-submit-1",
		SubmissionBaseRevision:  rig.readyDraft.Revision,
		SubmissionFingerprint:   calculated.Fingerprint,
		CandidateOfferVersionID: "candidate-version-1",
		CandidateVersionNumber:  1,
		ClaimedAt:               rig.now,
	})
	if err != nil {
		t.Fatalf("claiming the draft: %v", err)
	}

	resumed := rig.submitInput
	resumed.ExpectedRevision = claimed.Revision
	version, err := rig.service.SubmitOffer(ctx, resumed)
	if err != nil {
		t.Fatalf("recovery must complete the submission, got %v", err)
	}

	if version.ID != "candidate-version-1" {
		t.Errorf("version ID = %q, want the RECORDED candidate identity",
			version.ID)
	}
	if version.VersionNumber != 1 {
		t.Errorf("version number = %d, want the recorded 1", version.VersionNumber)
	}
}

// Recovery: version inserted but eligibility missing. The retry creates only
// the missing aggregate.
func TestSubmitOfferRecoversMissingEligibility(t *testing.T) {
	rig := newSubmissionRig(t)
	ctx := context.Background()

	version, err := rig.service.SubmitOffer(ctx, rig.submitInput)
	if err != nil {
		t.Fatalf("SubmitOffer: %v", err)
	}

	// Simulate the crash window by deleting the eligibility gate directly.
	// Doing this from the test keeps a test-only escape hatch out of the
	// production repository surface.
	if _, err := rig.db.Collection("supplier_offer_eligibilities").
		DeleteMany(ctx, bson.M{"offerVersionId": version.ID}); err != nil {
		t.Fatalf("clearing eligibility: %v", err)
	}

	if _, err := rig.service.SubmitOffer(ctx, rig.submitInput); err != nil {
		t.Fatalf("recovery must recreate eligibility, got %v", err)
	}

	_, found, err := rig.eligibility.FindEligibility(ctx, "company-1", version.ID)
	if err != nil || !found {
		t.Fatalf("eligibility found = %v, err = %v", found, err)
	}
}

// The immutable version records the full provenance an audit needs.
func TestSubmittedVersionRecordsProvenance(t *testing.T) {
	rig := newSubmissionRig(t)
	ctx := context.Background()

	version, err := rig.service.SubmitOffer(ctx, rig.submitInput)
	if err != nil {
		t.Fatalf("SubmitOffer: %v", err)
	}

	if version.SourceDraftID != rig.draft.ID ||
		version.SubmissionOperationID != "op-submit-1" ||
		version.RecipientIdentity != "sales@supplier.test" ||
		version.InvitationID != "invitation-1" ||
		version.IssuedRFQVersionID != "issued-version-2" {
		t.Errorf("provenance = %+v, want the exact submitting identity", version)
	}
	if version.SubmissionFingerprint == "" {
		t.Error("the version must record its content fingerprint")
	}
}

// issuedSnapshot mirrors the authoritative RFQ the rig's authorizer returns.
func (rig *submissionRig) issuedSnapshot() IssuedRFQSnapshot {
	return IssuedRFQSnapshot{
		ID: "issued-version-2", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
		Currency: Phase1Currency, ResponseDeadline: rig.now.Add(time.Hour),
		Lines: []IssuedRFQLineSnapshot{{
			ID: "rfq-line-1", LineageID: "lineage-1",
			Quantity: rig.lineQuantity,
		}},
	}
}
