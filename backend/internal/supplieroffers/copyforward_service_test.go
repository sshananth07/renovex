package supplieroffers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// enableAutomaticCopySource swaps the rig's fixed-snapshot IssuedRFQSource for
// a multi-version one that still serves the rig's own target RFQ (so ordinary
// draft operations are unaffected) but also lets tests register an EARLIER
// issued RFQ version for the same RFQ chain, which is what the M8.1 automatic
// source resolver searches across.
func (rig *draftEditRig) enableAutomaticCopySource(t *testing.T) *fakeMultiVersionIssuedRFQSource {
	t.Helper()
	multi := &fakeMultiVersionIssuedRFQSource{byID: map[string]IssuedRFQSnapshot{
		"issued-version-2": {
			ID: "issued-version-2", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
			VersionNumber: 2, Currency: Phase1Currency,
			ResponseDeadline: rig.now.Add(time.Hour),
			Lines: []IssuedRFQLineSnapshot{
				{ID: "rfq-line-1", LineageID: "lineage-1", Quantity: rig.lineQuantity},
			},
		},
	}}
	rig.service.issuedRFQ = multi
	return multi
}

// seedAutomaticSource creates offer-version-1 as an eligible, non-withdrawn
// submission against an EARLIER issued RFQ version of the same RFQ chain, and
// registers that earlier version with the multi-version fake so the resolver
// can see it.
func (rig *draftEditRig) seedAutomaticSource(
	t *testing.T,
	multi *fakeMultiVersionIssuedRFQSource,
	chains *MongoOfferChainRepository,
	versions *MongoOfferVersionRepository,
	eligibility *MongoOfferEligibilityRepository,
) SupplierOfferVersion {
	t.Helper()
	ctx := context.Background()

	// The source RFQ's own line uses a DIFFERENT id than the target's
	// "rfq-line-1" — exactly as a real amendment mints a fresh per-version id
	// while preserving lineageId — with the SAME lineage and commercial
	// fields, so the fingerprint gate lets the response copy. A regression
	// where the matching mapping is built from the wrong RFQ version would
	// fail to resolve this source line at all.
	multi.byID["issued-version-1"] = IssuedRFQSnapshot{
		ID: "issued-version-1", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
		VersionNumber: 1, Currency: Phase1Currency,
		Lines: []IssuedRFQLineSnapshot{
			{ID: "source-issued-line-1", LineageID: "lineage-1", Quantity: rig.lineQuantity},
		},
	}

	chain, err := chains.EnsureOfferChain(ctx, "company-1", "invitation-1", "issued-version-1")
	if err != nil {
		t.Fatalf("ensuring source chain: %v", err)
	}

	unitPrice := money.New(2_000, Phase1Currency)
	subtotal := money.New(20_000, Phase1Currency)
	quoted := qty(t, "10", "unit")
	version := SupplierOfferVersion{
		ID:                 "offer-version-1",
		CompanyID:          "company-1",
		OfferChainID:       chain.ID,
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-version-1",
		VersionNumber:      1,
		Currency:           Phase1Currency,
		RecipientIdentity:  "sales@supplier.test",
		// E3 enforces company-scoped submission-operation identity, so each
		// seeded version needs its own operation ID.
		SubmissionOperationID: "op-submit-1",
		Lines: []SupplierOfferLine{{
			ID: "source-line-1", RFQLineID: "source-issued-line-1",
			ResponseStatus:           OfferLineQuoted,
			QuotedQuantity:           &quoted,
			UnitPriceExcludingTax:    &unitPrice,
			LineSubtotalExcludingTax: &subtotal,
			Brand:                    "Acme",
		}},
		Tax:             SupplierOfferTax{Mode: TaxModeNotApplicable},
		SupplierNotes:   "carried over",
		OfferValidUntil: rig.now.Add(30 * 24 * time.Hour),
		SubmittedAt:     rig.now.Add(-time.Hour),
		SchemaVersion:   SupplierOfferVersionSchemaVersion,
	}
	if err := versions.InsertVersion(ctx, version); err != nil {
		t.Fatalf("seeding submitted version: %v", err)
	}
	if _, err := chains.AdvanceChainToVersion(ctx, "company-1", chain.ID, chain.Revision,
		version.ID, version.VersionNumber, rig.now.Add(-time.Hour)); err != nil {
		t.Fatalf("advancing source chain: %v", err)
	}
	if err := eligibility.InsertEligibility(ctx, SupplierOfferEligibility{
		ID: "eligibility-offer-version-1", CompanyID: "company-1",
		OfferChainID: chain.ID, OfferVersionID: version.ID,
		State: EligibilityEligible, Revision: 1,
	}); err != nil {
		t.Fatalf("seeding eligibility: %v", err)
	}
	return version
}

func (rig *draftEditRig) wireCopyForwardCapabilities(
	versions *MongoOfferVersionRepository,
	chains *MongoOfferChainRepository,
	eligibility *MongoOfferEligibilityRepository,
) {
	rig.service.versions = versions
	rig.service.chains = chains
	rig.service.eligibility = eligibility
}

// Copy-forward applies to the caller's own active draft through the same
// revision-guarded CAS every other edit uses, resolving the source
// automatically rather than accepting one from the caller.
func TestCopyForwardIntoDraftAppliesCopiedState(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	chains := NewMongoOfferChainRepository(rig.db)
	eligibility := NewMongoOfferEligibilityRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}
	multi := rig.enableAutomaticCopySource(t)
	source := rig.seedAutomaticSource(t, multi, chains, versions, eligibility)
	rig.wireCopyForwardCapabilities(versions, chains, eligibility)

	copied, err := rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
	})
	if err != nil {
		t.Fatalf("CopyForwardIntoDraft: %v", err)
	}

	if copied.Draft.SupplierNotes != "carried over" {
		t.Errorf("SupplierNotes = %q, want the copied text",
			copied.Draft.SupplierNotes)
	}
	if copied.Draft.Revision != rig.draft.Revision+1 {
		t.Errorf("Revision = %d, want one increment", copied.Draft.Revision)
	}
	// Validity never copies; submission must require a fresh date.
	if copied.Draft.OfferValidUntil != nil {
		t.Error("OfferValidUntil was copied into the draft")
	}
	if copied.Draft.SourceOfferVersionID == nil || *copied.Draft.SourceOfferVersionID != source.ID {
		t.Errorf("SourceOfferVersionID = %v, want the resolved source %s",
			copied.Draft.SourceOfferVersionID, source.ID)
	}

	// Regression coverage: CopyForwardIntoDraft must build the matching
	// (lineage/Material-Requirement) mapping from the SOURCE's own issued RFQ
	// lines, not the target's. Building it from the target silently breaks
	// every source-line lookup and leaves every line unanswered even when the
	// commercial fingerprint genuinely matches.
	if len(copied.Draft.Lines) != 1 {
		t.Fatalf("copied draft lines = %d, want 1", len(copied.Draft.Lines))
	}
	line := copied.Draft.Lines[0]
	if line.ResponseStatus != OfferLineQuoted {
		t.Fatalf("ResponseStatus = %q, want quoted — the source line was not matched/copied",
			line.ResponseStatus)
	}
	if line.UnitPriceExcludingTax == nil || line.UnitPriceExcludingTax.Amount != 2_000 {
		t.Errorf("UnitPriceExcludingTax = %v, want the copied 2000", line.UnitPriceExcludingTax)
	}
}

// With no eligible prior submission anywhere, copy-forward reports the
// bounded not-found rather than an internal error, and never touches the
// draft's revision.
func TestCopyForwardIntoDraftReportsNotFoundWithNoEligibleSource(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	chains := NewMongoOfferChainRepository(rig.db)
	eligibility := NewMongoOfferEligibilityRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}
	rig.enableAutomaticCopySource(t)
	rig.wireCopyForwardCapabilities(versions, chains, eligibility)

	_, err := rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
	})
	if !errors.Is(err, ErrCopySourceNotFound) {
		t.Fatalf("no-source copy error = %v, want copy_source_not_found", err)
	}

	reloaded, found, findErr := rig.drafts.FindDraft(ctx, "company-1", rig.draft.ID)
	if findErr != nil || !found {
		t.Fatalf("reloading draft: found=%v err=%v", found, findErr)
	}
	if reloaded.Revision != rig.draft.Revision {
		t.Errorf("Revision = %d, want unchanged %d after a failed resolution",
			reloaded.Revision, rig.draft.Revision)
	}
}

// A non-empty draft refuses copy-forward with the bounded conflict rather
// than merging into or overwriting Supplier-entered content.
func TestCopyForwardIntoDraftRefusesANonEmptyDraft(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	chains := NewMongoOfferChainRepository(rig.db)
	eligibility := NewMongoOfferEligibilityRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}
	multi := rig.enableAutomaticCopySource(t)
	rig.seedAutomaticSource(t, multi, chains, versions, eligibility)
	rig.wireCopyForwardCapabilities(versions, chains, eligibility)

	quoted, err := rig.service.QuoteDraftLine(ctx, QuoteDraftLineCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
		DraftLineID: rig.draft.Lines[0].ID, UnitPriceMinor: 1_000,
	})
	if err != nil {
		t.Fatalf("quoting a line to make the draft non-empty: %v", err)
	}

	_, err = rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: quoted.Revision,
	})
	if !errors.Is(err, ErrOfferDraftNotEmpty) {
		t.Fatalf("non-empty copy error = %v, want draft_not_empty", err)
	}
}

// Copy-forward is revision-guarded like every other edit, so it cannot
// overwrite a concurrent change.
func TestCopyForwardIntoDraftRejectsAStaleRevision(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	chains := NewMongoOfferChainRepository(rig.db)
	eligibility := NewMongoOfferEligibilityRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}
	multi := rig.enableAutomaticCopySource(t)
	rig.seedAutomaticSource(t, multi, chains, versions, eligibility)
	rig.wireCopyForwardCapabilities(versions, chains, eligibility)

	if _, err := rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision + 99,
	}); !errors.Is(err, ErrOfferDraftConflict) {
		t.Fatalf("stale-revision copy error = %v, want conflict", err)
	}
}

// Bounded omission feedback reaches the caller so the Supplier learns a group
// did not come across, rather than silently losing it.
func TestCopyForwardIntoDraftReportsBoundedOmissions(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	chains := NewMongoOfferChainRepository(rig.db)
	eligibility := NewMongoOfferEligibilityRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}
	multi := rig.enableAutomaticCopySource(t)
	multi.byID["issued-version-1"] = IssuedRFQSnapshot{
		ID: "issued-version-1", CompanyID: "company-1", RFQChainID: "rfq-chain-1",
		VersionNumber: 1, Currency: Phase1Currency,
	}
	chain, err := chains.EnsureOfferChain(ctx, "company-1", "invitation-1", "issued-version-1")
	if err != nil {
		t.Fatalf("ensuring source chain: %v", err)
	}
	source := SupplierOfferVersion{
		ID: "offer-version-with-group", CompanyID: "company-1", OfferChainID: chain.ID,
		InvitationID: "invitation-1", IssuedRFQVersionID: "issued-version-1",
		VersionNumber: 1, Currency: Phase1Currency, RecipientIdentity: "sales@supplier.test",
		SubmissionOperationID: "op-submit-2",
		ChargeGroups: []ConditionalChargeGroup{{
			ID: "source-group-1", Name: "Bulk handling",
			ApplicableRFQLineIDs: []string{"rfq-line-REMOVED"},
		}},
		Tax:         SupplierOfferTax{Mode: TaxModeNotApplicable},
		SubmittedAt: rig.now.Add(-time.Hour),
	}
	if err := versions.InsertVersion(ctx, source); err != nil {
		t.Fatalf("seeding a version with a group: %v", err)
	}
	if _, err := chains.AdvanceChainToVersion(ctx, "company-1", chain.ID, chain.Revision,
		source.ID, source.VersionNumber, rig.now.Add(-time.Hour)); err != nil {
		t.Fatalf("advancing source chain: %v", err)
	}
	if err := eligibility.InsertEligibility(ctx, SupplierOfferEligibility{
		ID: "eligibility-with-group", CompanyID: "company-1",
		OfferChainID: chain.ID, OfferVersionID: source.ID,
		State: EligibilityEligible, Revision: 1,
	}); err != nil {
		t.Fatalf("seeding eligibility: %v", err)
	}
	rig.wireCopyForwardCapabilities(versions, chains, eligibility)

	copied, err := rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
	})
	if err != nil {
		t.Fatalf("CopyForwardIntoDraft: %v", err)
	}

	if len(copied.OmittedGroups) != 1 {
		t.Fatalf("omissions = %d, want one bounded omission",
			len(copied.OmittedGroups))
	}
	if copied.OmittedGroups[0].Reason != CopyOmissionMissingLine {
		t.Errorf("reason = %q, want missing_line", copied.OmittedGroups[0].Reason)
	}
}

// A repeated copy-forward call after the draft already has a resolved source
// converges on the existing draft instead of resolving or copying again.
func TestCopyForwardIntoDraftConvergesWhenSourceAlreadyResolved(t *testing.T) {
	rig := newDraftEditRig(t)
	ctx := context.Background()
	versions := NewMongoOfferVersionRepository(rig.db)
	chains := NewMongoOfferChainRepository(rig.db)
	eligibility := NewMongoOfferEligibilityRepository(rig.db)
	if err := versions.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	if err := eligibility.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring eligibility indexes: %v", err)
	}
	multi := rig.enableAutomaticCopySource(t)
	source := rig.seedAutomaticSource(t, multi, chains, versions, eligibility)
	rig.wireCopyForwardCapabilities(versions, chains, eligibility)

	first, err := rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: rig.draft.Revision,
	})
	if err != nil {
		t.Fatalf("first CopyForwardIntoDraft: %v", err)
	}

	second, err := rig.service.CopyForwardIntoDraft(ctx, CopyForwardCommand{
		Context: rig.input, DraftID: rig.draft.ID, ExpectedRevision: first.Draft.Revision,
	})
	if err != nil {
		t.Fatalf("second CopyForwardIntoDraft (should converge): %v", err)
	}
	if second.Draft.Revision != first.Draft.Revision {
		t.Errorf("Revision = %d, want unchanged %d on convergence",
			second.Draft.Revision, first.Draft.Revision)
	}
	if second.Draft.SourceOfferVersionID == nil || *second.Draft.SourceOfferVersionID != source.ID {
		t.Errorf("converged SourceOfferVersionID = %v, want %s",
			second.Draft.SourceOfferVersionID, source.ID)
	}
}
