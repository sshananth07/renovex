package rfqissuance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Amendment drafts and Version N+1 (design spec §3.3, §4.2).
//
// The rule that shapes everything here: an issued version is IMMUTABLE. Even a
// deadline-only extension creates a new version rather than editing the old one,
// so a Supplier keeps reading the exact version they were invited to while the
// contractor prepares the next.

// issueFirstVersion puts a chain into the "one issued version" state the
// amendment flow starts from.
func issueFirstVersion(t *testing.T, svc *rfqissuance.Service) rfqissuance.IssuedRFQVersion {
	t.Helper()
	issued, err := svc.IssueVersion(context.Background(), "company-1", "user-1",
		issueInput("op-issue-1"))
	if err != nil {
		t.Fatalf("setup issue: %v", err)
	}
	return issued
}

func newAmendableService(t *testing.T) (*rfqissuance.Service, *fakeDraftStore) {
	t.Helper()
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	drafts := newFakeDraftStore()
	svc := rfqissuance.NewService(newFakeVersionStore(),
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(newFakeChainStore()),
		rfqissuance.WithAmendmentDrafts(drafts),
	)
	return svc, drafts
}

// The draft CLONES the latest immutable version — it does not reference it.
func TestCreateAmendmentDraftClonesTheLatestIssuedVersion(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issued := issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if draft.BaseIssuedVersionID != issued.ID {
		t.Errorf("BaseIssuedVersionID = %q, want the current version %q",
			draft.BaseIssuedVersionID, issued.ID)
	}
	if draft.BaseVersionNumber != issued.VersionNumber {
		t.Errorf("BaseVersionNumber = %d, want %d", draft.BaseVersionNumber,
			issued.VersionNumber)
	}
	if draft.Currency != issued.Currency {
		t.Errorf("Currency = %q, want the immutable currency %q", draft.Currency,
			issued.Currency)
	}
	if draft.Title != issued.Title || draft.DeliveryAddress != issued.DeliveryAddress {
		t.Error("the draft must clone the issued header")
	}

	if len(draft.Lines) != len(issued.Lines) {
		t.Fatalf("got %d draft lines, want %d", len(draft.Lines), len(issued.Lines))
	}
	// Cloned lines keep lineage and provenance but take NEW per-version IDs.
	original, cloned := issued.Lines[0], draft.Lines[0]
	if cloned.LineageID != original.LineageID {
		t.Errorf("LineageID = %q, want the original %q", cloned.LineageID, original.LineageID)
	}
	if cloned.ID == original.ID {
		t.Error("a cloned line must take a NEW per-version ID (§3.2A)")
	}
	if cloned.SourceMaterialRequirementID == nil ||
		*cloned.SourceMaterialRequirementID != "mr-1" {
		t.Errorf("SourceMaterialRequirementID = %v, want mr-1 preserved",
			cloned.SourceMaterialRequirementID)
	}
}

func TestCreateAmendmentDraftRecordsItsPrimitiveAuditIdentity(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	chains := newFakeChainStore()
	versions := newFakeVersionStore()
	drafts := newFakeDraftStore()
	audit := &recordingIssuanceAudit{}
	svc := rfqissuance.NewService(
		versions,
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(chains),
		rfqissuance.WithAmendmentDrafts(drafts),
		rfqissuance.WithAuditRecorder(audit),
	)
	ctx := context.Background()
	first := issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if len(audit.draftCreated) != 1 {
		t.Fatalf("recorded %d draft-created events, want 1", len(audit.draftCreated))
	}
	call := audit.draftCreated[0]
	if call.companyID != "company-1" ||
		call.projectID != first.ProjectID ||
		call.actorUserID != "user-1" ||
		call.rfqChainID != first.RFQChainID ||
		call.rfqNumber != first.RFQNumber ||
		call.draftID != draft.ID ||
		call.baseVersionNumber != 1 {
		t.Errorf("audit call = %+v, want primitive identity for the created draft", call)
	}
}

// Only ONE draft may exist per chain (§3.3, §11.2).
func TestCreateAmendmentDraftRefusesASecondDraft(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	if _, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")

	if !errors.Is(err, rfqissuance.ErrAmendmentDraftAlreadyExists) {
		t.Errorf("error = %v, want ErrAmendmentDraftAlreadyExists", err)
	}
}

// A chain with no issued version has nothing to amend.
func TestCreateAmendmentDraftRequiresAnIssuedVersion(t *testing.T) {
	svc, _ := newAmendableService(t)

	_, err := svc.CreateAmendmentDraft(context.Background(), "company-1", "user-1", "chain-1")

	if !errors.Is(err, rfqissuance.ErrIssuanceChainNotFound) {
		t.Errorf("error = %v, want ErrIssuanceChainNotFound", err)
	}
}

func TestUpdateAmendmentDraftReplacesLinesWithServerOwnedIdentity(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	original := draft.Lines[0]
	replacement := []rfqissuance.AmendmentDraftLinePatch{
		{
			ID: original.ID, MaterialID: "material-1", MaterialName: "Low-carbon cement",
			Specification: "MS EN 197-1", QuantityValue: "125.5", QuantityUnit: "bag",
			ProcurementNotes: "palletised", SortOrder: 1,
		},
		{
			MaterialID: "material-9", MaterialName: "Rebar",
			Specification: "Y12", QuantityValue: "40", QuantityUnit: "length",
			SortOrder: 2,
		},
	}

	updated, err := svc.UpdateAmendmentDraft(
		ctx, "company-1", "user-1", "chain-1", draft.Revision,
		rfqissuance.AmendmentDraftPatch{Lines: &replacement})
	if err != nil {
		t.Fatalf("replace draft lines: %v", err)
	}
	if len(updated.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(updated.Lines))
	}

	edited := updated.Lines[0]
	if edited.ID != original.ID || edited.LineageID != original.LineageID {
		t.Error("editing an existing line changed its server-owned ID or lineage")
	}
	if edited.SourceM7RFQLineID == nil ||
		*edited.SourceM7RFQLineID != *original.SourceM7RFQLineID ||
		edited.SourceMaterialRequirementID == nil ||
		*edited.SourceMaterialRequirementID != *original.SourceMaterialRequirementID {
		t.Error("editing an existing line changed its M7 provenance")
	}
	if edited.MaterialName != "Low-carbon cement" ||
		edited.Quantity.Value.String() != "125.5" {
		t.Errorf("editable content was not applied: %+v", edited)
	}

	added := updated.Lines[1]
	if added.ID == "" || added.LineageID == "" || added.ID == added.LineageID {
		t.Error("a new M8 line must receive distinct server-generated ID and lineage")
	}
	if added.SourceM7RFQLineID != nil || added.SourceMaterialRequirementID != nil {
		t.Error("a new M8-native line must not claim M7 provenance")
	}
}

func TestUpdateAmendmentDraftRejectsUnknownAndDuplicateLineIDs(t *testing.T) {
	tests := []struct {
		name  string
		lines func(existing rfqissuance.IssuedRFQLine) []rfqissuance.AmendmentDraftLinePatch
		want  error
	}{
		{
			name: "unknown line",
			lines: func(rfqissuance.IssuedRFQLine) []rfqissuance.AmendmentDraftLinePatch {
				return []rfqissuance.AmendmentDraftLinePatch{{
					ID: "caller-invented-id", MaterialID: "material-1",
					QuantityValue: "1", QuantityUnit: "bag",
				}}
			},
			want: rfqissuance.ErrAmendmentLineNotFound,
		},
		{
			name: "duplicate line",
			lines: func(existing rfqissuance.IssuedRFQLine) []rfqissuance.AmendmentDraftLinePatch {
				line := rfqissuance.AmendmentDraftLinePatch{
					ID: existing.ID, MaterialID: existing.MaterialID,
					QuantityValue: existing.Quantity.Value.String(),
					QuantityUnit:  existing.Quantity.Unit,
				}
				return []rfqissuance.AmendmentDraftLinePatch{line, line}
			},
			want: rfqissuance.ErrDuplicateAmendmentLine,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newAmendableService(t)
			ctx := context.Background()
			issueFirstVersion(t, svc)
			draft, err := svc.CreateAmendmentDraft(
				ctx, "company-1", "user-1", "chain-1")
			if err != nil {
				t.Fatalf("create draft: %v", err)
			}
			lines := tc.lines(draft.Lines[0])

			_, err = svc.UpdateAmendmentDraft(
				ctx, "company-1", "user-1", "chain-1", draft.Revision,
				rfqissuance.AmendmentDraftPatch{Lines: &lines})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// Issuing the draft produces the NEXT immutable version and leaves the previous
// one untouched — the core immutability guarantee.
func TestIssueAmendmentCreatesVersionTwoAndLeavesVersionOneUnchanged(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	first := issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: draft.Revision,
			OperationID: "op-amend-1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if second.VersionNumber != 2 {
		t.Errorf("VersionNumber = %d, want 2", second.VersionNumber)
	}
	if second.ID == first.ID {
		t.Error("the amendment must be a NEW version record")
	}

	// Version 1 must be byte-for-byte what it was: a Supplier invited to it is
	// still reading it.
	stillFirst, err := svc.GetIssuedVersion(ctx, "company-1", first.ID)
	if err != nil {
		t.Fatalf("reading version 1: %v", err)
	}
	if stillFirst.VersionNumber != 1 {
		t.Errorf("version 1's number changed to %d", stillFirst.VersionNumber)
	}
	if !stillFirst.ResponseDeadline.Equal(first.ResponseDeadline) {
		t.Error("version 1's response deadline was mutated by the amendment")
	}
	if len(stillFirst.Lines) != len(first.Lines) ||
		stillFirst.Lines[0].ID != first.Lines[0].ID {
		t.Error("version 1's lines were mutated by the amendment")
	}
}

// Version N+1 records the exact draft identity used to create it while
// preserving the original M7 revision as provenance. Without both, an
// operation-ID retry cannot prove it is completing the same logical amendment.
func TestIssueAmendmentPersistsItsDraftSourceIdentity(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	first := issueFirstVersion(t, svc)
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	second, err := svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: draft.Revision,
			OperationID: "op-amend-1",
		})
	if err != nil {
		t.Fatalf("issue amendment: %v", err)
	}

	if second.SourceM7RFQRevision != first.SourceM7RFQRevision {
		t.Errorf("SourceM7RFQRevision = %d, want original provenance revision %d",
			second.SourceM7RFQRevision, first.SourceM7RFQRevision)
	}
	if second.SourceFingerprint == "" {
		t.Fatal("SourceFingerprint is empty; the issued amendment is not bound to its draft")
	}
	if second.SourceFingerprint == first.SourceFingerprint {
		t.Error("amendment fingerprint equals the M7 source fingerprint; it must identify the draft")
	}
}

func TestIssueAmendmentRecordsOnePrimitiveOnlyAuditEvent(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	audit := &recordingIssuanceAudit{}
	svc := rfqissuance.NewService(
		newFakeVersionStore(),
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(newFakeChainStore()),
		rfqissuance.WithAmendmentDrafts(newFakeDraftStore()),
		rfqissuance.WithAuditRecorder(audit),
	)
	ctx := context.Background()
	first := issueFirstVersion(t, svc)
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	input := rfqissuance.IssueAmendmentInput{
		RFQChainID: "chain-1", ExpectedRevision: draft.Revision,
		OperationID: "op-amend-audit",
	}

	second, err := svc.IssueAmendment(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("issue amendment: %v", err)
	}
	if _, err := svc.IssueAmendment(ctx, "company-1", "user-1", input); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}

	if len(audit.amendmentIssued) != 1 {
		t.Fatalf("recorded %d amendment-issued events, want exactly 1",
			len(audit.amendmentIssued))
	}
	call := audit.amendmentIssued[0]
	if call.companyID != "company-1" ||
		call.projectID != first.ProjectID ||
		call.actorUserID != "user-1" ||
		call.rfqChainID != second.RFQChainID ||
		call.rfqNumber != second.RFQNumber ||
		call.draftID != draft.ID ||
		call.versionID != second.ID ||
		call.versionNumber != 2 {
		t.Errorf("audit call = %+v, want primitive identity for issued amendment", call)
	}
}

// A deadline-only extension still creates a new version (§3.3).
func TestDeadlineOnlyExtensionCreatesANewImmutableVersion(t *testing.T) {
	svc, drafts := newAmendableService(t)
	ctx := context.Background()
	first := issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	extended := first.ResponseDeadline.Add(7 * 24 * time.Hour)
	updated, err := svc.UpdateAmendmentDraft(ctx, "company-1", "user-1", "chain-1",
		draft.Revision, rfqissuance.AmendmentDraftPatch{ResponseDeadline: &extended})
	if err != nil {
		t.Fatalf("updating the draft: %v", err)
	}

	second, err := svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: updated.Revision,
			OperationID: "op-amend-1",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !second.ResponseDeadline.Equal(extended) {
		t.Errorf("ResponseDeadline = %v, want the extended %v",
			second.ResponseDeadline, extended)
	}
	if second.VersionNumber != 2 {
		t.Errorf("VersionNumber = %d, want 2: even a deadline-only change is a new "+
			"immutable version, never an edit", second.VersionNumber)
	}
	// The draft is consumed by issuance.
	if _, found := drafts.drafts[chainKey("company-1", "chain-1")]; found {
		t.Error("the amendment draft must be archived once issued")
	}
}

// Optimistic concurrency: a stale editor cannot overwrite a newer edit (§11.3).
func TestUpdateAmendmentDraftRequiresTheExpectedRevision(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newTitle := "Revised scope"
	if _, err := svc.UpdateAmendmentDraft(ctx, "company-1", "user-1", "chain-1",
		draft.Revision, rfqissuance.AmendmentDraftPatch{Title: &newTitle}); err != nil {
		t.Fatalf("the first edit must succeed: %v", err)
	}

	other := "Conflicting scope"
	_, err = svc.UpdateAmendmentDraft(ctx, "company-1", "user-1", "chain-1",
		draft.Revision, rfqissuance.AmendmentDraftPatch{Title: &other})

	if !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch for a stale editor", err)
	}
}

func TestUpdateAmendmentDraftRecordsTheNewRevision(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	audit := &recordingIssuanceAudit{}
	svc := rfqissuance.NewService(
		newFakeVersionStore(),
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(newFakeChainStore()),
		rfqissuance.WithAmendmentDrafts(newFakeDraftStore()),
		rfqissuance.WithAuditRecorder(audit),
	)
	ctx := context.Background()
	issueFirstVersion(t, svc)
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	title := "Audited revised scope"
	updated, err := svc.UpdateAmendmentDraft(
		ctx, "company-1", "user-1", "chain-1", draft.Revision,
		rfqissuance.AmendmentDraftPatch{Title: &title})
	if err != nil {
		t.Fatalf("update draft: %v", err)
	}

	if len(audit.draftUpdated) != 1 {
		t.Fatalf("recorded %d draft-updated events, want 1", len(audit.draftUpdated))
	}
	call := audit.draftUpdated[0]
	if call.companyID != "company-1" ||
		call.actorUserID != "user-1" ||
		call.rfqChainID != "chain-1" ||
		call.draftID != updated.ID ||
		call.revision != updated.Revision {
		t.Errorf("audit call = %+v, want the updated draft identity and revision", call)
	}
}

func TestIssueAmendmentRequiresTheExpectedRevision(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: draft.Revision + 99,
			OperationID: "op-amend-1",
		})

	if !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch", err)
	}
}

// §4.2 step 2: the base version must still be the current version. If another
// amendment was issued meanwhile, this draft was built on a superseded base.
func TestIssueAmendmentRefusesAStaleBaseVersion(t *testing.T) {
	svc, drafts := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	if _, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate another amendment having been issued: the draft's base is no
	// longer what the chain points at.
	stored := drafts.drafts[chainKey("company-1", "chain-1")]
	stored.BaseIssuedVersionID = "some-superseded-version-id"

	_, err := svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: stored.Revision,
			OperationID: "op-amend-1",
		})

	if !errors.Is(err, rfqissuance.ErrStaleBaseVersion) {
		t.Errorf("error = %v, want ErrStaleBaseVersion: the draft was built on a "+
			"version that is no longer current (§4.2)", err)
	}
}

// An amendment that removes the deadline is refused, exactly as a first
// issuance would be (§4.1A).
func TestIssueAmendmentStillRequiresAResponseDeadline(t *testing.T) {
	svc, drafts := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored := drafts.drafts[chainKey("company-1", "chain-1")]
	stored.ResponseDeadline = nil

	_, err = svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: draft.Revision,
			OperationID: "op-amend-1",
		})

	if !errors.Is(err, rfqissuance.ErrResponseDeadlineRequired) {
		t.Errorf("error = %v, want ErrResponseDeadlineRequired", err)
	}
}

// Discarding a draft leaves the issued versions untouched.
func TestDiscardAmendmentDraftRemovesOnlyTheDraft(t *testing.T) {
	svc, drafts := newAmendableService(t)
	ctx := context.Background()
	first := issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := svc.DiscardAmendmentDraft(ctx, "company-1", "user-1", "chain-1",
		draft.Revision); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, found := drafts.drafts[chainKey("company-1", "chain-1")]; found {
		t.Error("the draft must be removed")
	}
	if _, err := svc.GetIssuedVersion(ctx, "company-1", first.ID); err != nil {
		t.Errorf("discarding a draft must not affect issued versions: %v", err)
	}
}

func TestDiscardAmendmentDraftRecordsTheDiscardedRevision(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	audit := &recordingIssuanceAudit{}
	svc := rfqissuance.NewService(
		newFakeVersionStore(),
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(newFakeChainStore()),
		rfqissuance.WithAmendmentDrafts(newFakeDraftStore()),
		rfqissuance.WithAuditRecorder(audit),
	)
	ctx := context.Background()
	issueFirstVersion(t, svc)
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	if err := svc.DiscardAmendmentDraft(
		ctx, "company-1", "user-1", "chain-1", draft.Revision); err != nil {
		t.Fatalf("discard draft: %v", err)
	}

	if len(audit.draftDiscarded) != 1 {
		t.Fatalf("recorded %d draft-discarded events, want 1", len(audit.draftDiscarded))
	}
	call := audit.draftDiscarded[0]
	if call.companyID != "company-1" ||
		call.actorUserID != "user-1" ||
		call.rfqChainID != "chain-1" ||
		call.draftID != draft.ID ||
		call.revision != draft.Revision {
		t.Errorf("audit call = %+v, want the discarded draft identity and revision", call)
	}
}

// Amendment issuance is idempotent under its operation ID, like first issuance.
func TestIssueAmendmentIsIdempotentUnderTheSameOperationID(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := rfqissuance.IssueAmendmentInput{
		RFQChainID: "chain-1", ExpectedRevision: draft.Revision,
		OperationID: "op-amend-1",
	}

	firstResult, err := svc.IssueAmendment(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secondResult, err := svc.IssueAmendment(ctx, "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("a retry must resolve, got %v", err)
	}

	if secondResult.ID != firstResult.ID {
		t.Errorf("the retry issued %s, want the original %s", secondResult.ID, firstResult.ID)
	}
}

// Operation IDs are company-wide, so the amendment path must verify the hit is
// an amendment for this chain. An initial-issue operation is a different
// logical action even when it belongs to the same RFQ chain.
func TestIssueAmendmentRejectsAnInitialIssueOperationID(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)
	draft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}

	_, err = svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: draft.Revision,
			OperationID: "op-issue-1",
		})
	if !errors.Is(err, rfqissuance.ErrOperationAlreadyUsed) {
		t.Errorf("error = %v, want ErrOperationAlreadyUsed for an initial-issue operation", err)
	}
}

// Reusing an old amendment operation for a new draft on the same chain is not
// a retry. The new draft has a different base and fingerprint and must receive
// its own operation ID.
func TestIssueAmendmentRejectsAnOperationIDReusedForANewerDraft(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	firstDraft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create first draft: %v", err)
	}
	if _, err := svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: firstDraft.Revision,
			OperationID: "op-amend-1",
		}); err != nil {
		t.Fatalf("issue first amendment: %v", err)
	}

	newerDraft, err := svc.CreateAmendmentDraft(ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("create newer draft: %v", err)
	}
	_, err = svc.IssueAmendment(ctx, "company-1", "user-1",
		rfqissuance.IssueAmendmentInput{
			RFQChainID: "chain-1", ExpectedRevision: newerDraft.Revision,
			OperationID: "op-amend-1",
		})
	if !errors.Is(err, rfqissuance.ErrOperationAlreadyUsed) {
		t.Errorf("error = %v, want ErrOperationAlreadyUsed for the newer draft", err)
	}
}

// Tenant isolation: a foreign company cannot read or amend this chain.
func TestAmendmentOperationsAreTenantScoped(t *testing.T) {
	svc, _ := newAmendableService(t)
	ctx := context.Background()
	issueFirstVersion(t, svc)

	if _, err := svc.CreateAmendmentDraft(ctx, "company-2", "user-1",
		"chain-1"); !errors.Is(err, rfqissuance.ErrIssuanceChainNotFound) {
		t.Errorf("error = %v, want ErrIssuanceChainNotFound for a foreign company", err)
	}
}
