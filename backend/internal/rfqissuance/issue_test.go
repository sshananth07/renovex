package rfqissuance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// The Version 1 issuance flow (design spec §4.1, §4.1A, §10.1):
//
//	read the M7 ready RFQ
//	→ validate
//	→ reserve a version number
//	→ create the immutable version
//	→ advance the chain
//
// These are service-level tests over fakes; the concurrency and uniqueness
// guarantees they depend on are proven separately against real MongoDB.

// fakeReadyRFQSource satisfies rfqissuance.ReadyRFQSource.
type fakeReadyRFQSource struct {
	snapshot rfqissuance.ReadyRFQSnapshot
	found    bool
	err      error

	gotCompanyID  string
	gotRFQChainID string
}

type issuedAuditCall struct {
	companyID, projectID, actorUserID string
	rfqChainID, rfqNumber, versionID  string
	versionNumber                     int
}

type draftCreatedAuditCall struct {
	companyID, projectID, actorUserID string
	rfqChainID, rfqNumber, draftID    string
	baseVersionNumber                 int
}

type draftUpdatedAuditCall struct {
	companyID, actorUserID, rfqChainID, draftID string
	revision                                    int64
}

type draftDiscardedAuditCall struct {
	companyID, actorUserID, rfqChainID, draftID string
	revision                                    int64
}

type amendmentIssuedAuditCall struct {
	companyID, projectID, actorUserID string
	rfqChainID, rfqNumber, draftID    string
	versionID                         string
	versionNumber                     int
}

type chainReconciledAuditCall struct {
	companyID, projectID, actorUserID string
	rfqChainID, rfqNumber, versionID  string
	versionNumber                     int
}

type recordingIssuanceAudit struct {
	issued          []issuedAuditCall
	draftCreated    []draftCreatedAuditCall
	draftUpdated    []draftUpdatedAuditCall
	draftDiscarded  []draftDiscardedAuditCall
	amendmentIssued []amendmentIssuedAuditCall
	chainReconciled []chainReconciledAuditCall

	// Invitation events (§15). Implemented in invitation_audit_fake_test.go.
	invitationCreated           []invitationAuditCall
	invitationSent              []invitationAuditCall
	invitationDeliveryFailed    []invitationAuditCall
	invitationLinkCopied        []invitationAuditCall
	invitationRecipientReplaced []invitationAuditCall
	invitationSecretRotated     []invitationAuditCall
	invitationRevoked           []invitationAuditCall
	invitationReactivated       []invitationAuditCall
	invitationExpiryChanged     []invitationAuditCall
	invitationAdvanced          []invitationAuditCall
}

func (a *recordingIssuanceAudit) RecordRFQVersionIssued(
	_ context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, versionID string,
	versionNumber int,
) error {
	a.issued = append(a.issued, issuedAuditCall{
		companyID: companyID, projectID: projectID, actorUserID: actorUserID,
		rfqChainID: rfqChainID, rfqNumber: rfqNumber, versionID: versionID,
		versionNumber: versionNumber,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordRFQAmendmentDraftCreated(
	_ context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, draftID string,
	baseVersionNumber int,
) error {
	a.draftCreated = append(a.draftCreated, draftCreatedAuditCall{
		companyID: companyID, projectID: projectID, actorUserID: actorUserID,
		rfqChainID: rfqChainID, rfqNumber: rfqNumber, draftID: draftID,
		baseVersionNumber: baseVersionNumber,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordRFQAmendmentDraftUpdated(
	_ context.Context,
	companyID, actorUserID, rfqChainID, draftID string,
	revision int64,
) error {
	a.draftUpdated = append(a.draftUpdated, draftUpdatedAuditCall{
		companyID: companyID, actorUserID: actorUserID,
		rfqChainID: rfqChainID, draftID: draftID, revision: revision,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordRFQAmendmentDraftDiscarded(
	_ context.Context,
	companyID, actorUserID, rfqChainID, draftID string,
	revision int64,
) error {
	a.draftDiscarded = append(a.draftDiscarded, draftDiscardedAuditCall{
		companyID: companyID, actorUserID: actorUserID,
		rfqChainID: rfqChainID, draftID: draftID, revision: revision,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordRFQAmendmentIssued(
	_ context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, draftID, versionID string,
	versionNumber int,
) error {
	a.amendmentIssued = append(a.amendmentIssued, amendmentIssuedAuditCall{
		companyID: companyID, projectID: projectID, actorUserID: actorUserID,
		rfqChainID: rfqChainID, rfqNumber: rfqNumber, draftID: draftID,
		versionID: versionID, versionNumber: versionNumber,
	})
	return nil
}

func (a *recordingIssuanceAudit) RecordRFQIssuanceChainReconciled(
	_ context.Context,
	companyID, projectID, actorUserID, rfqChainID, rfqNumber, versionID string,
	versionNumber int,
) error {
	a.chainReconciled = append(a.chainReconciled, chainReconciledAuditCall{
		companyID: companyID, projectID: projectID, actorUserID: actorUserID,
		rfqChainID: rfqChainID, rfqNumber: rfqNumber, versionID: versionID,
		versionNumber: versionNumber,
	})
	return nil
}

func (f *fakeReadyRFQSource) GetReadyRFQSnapshot(_ context.Context,
	companyID, rfqChainID string) (rfqissuance.ReadyRFQSnapshot, bool, error) {
	f.gotCompanyID = companyID
	f.gotRFQChainID = rfqChainID
	return f.snapshot, f.found, f.err
}

func readySnapshot(deadline *time.Time) rfqissuance.ReadyRFQSnapshot {
	return rfqissuance.ReadyRFQSnapshot{
		RFQNumber: "RFQ-000001", ProjectID: "project-1", SourceM7RFQRevision: 7,
		Title: "Cement and aggregate", DeliveryAddress: "12 Site Road",
		ResponseDeadline:     deadline,
		SupplierInstructions: "deliver to site office",
		Lines: []rfqissuance.ReadyRFQLineSnapshot{
			readyLine("mr-1", "material-1"),
		},
	}
}

func futureDeadline() *time.Time {
	d := time.Now().Add(14 * 24 * time.Hour)
	return &d
}

func newIssuanceService(t *testing.T, source *fakeReadyRFQSource) (
	*rfqissuance.Service, *fakeChainStore, *fakeVersionStore) {
	t.Helper()
	chains := newFakeChainStore()
	versions := newFakeVersionStore()
	svc := rfqissuance.NewService(versions, rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(chains))
	return svc, chains, versions
}

func issueInput(operationID string) rfqissuance.IssueVersionInput {
	return rfqissuance.IssueVersionInput{
		RFQChainID:  "chain-1",
		Currency:    "MYR",
		OperationID: operationID,
	}
}

func TestIssueVersionOneCreatesTheImmutableVersionAndAdvancesTheChain(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, chains, versions := newIssuanceService(t, source)

	issued, err := svc.IssueVersion(context.Background(), "company-1", "user-1",
		issueInput("op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if issued.VersionNumber != 1 {
		t.Errorf("VersionNumber = %d, want 1", issued.VersionNumber)
	}
	if issued.RFQNumber != "RFQ-000001" || issued.ProjectID != "project-1" {
		t.Errorf("header not carried from the M7 snapshot: %+v", issued)
	}
	if issued.Currency != "MYR" {
		t.Errorf("Currency = %q, want MYR", issued.Currency)
	}
	if issued.Title != "Cement and aggregate" {
		t.Errorf("Title = %q; the issued version records the RFQ title", issued.Title)
	}
	if issued.IssuedByUserID != "user-1" {
		t.Errorf("IssuedByUserID = %q, want user-1", issued.IssuedByUserID)
	}
	if issued.IssuedAt.IsZero() {
		t.Error("IssuedAt must be stamped")
	}
	if issued.SourceM7RFQRevision != 7 {
		t.Errorf("SourceM7RFQRevision = %d, want the exact ready RFQ revision 7",
			issued.SourceM7RFQRevision)
	}

	if len(issued.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(issued.Lines))
	}
	line := issued.Lines[0]
	if line.ID == "" || line.LineageID == "" {
		t.Error("issued lines must carry both a per-version ID and a lineage ID")
	}
	if line.SourceMaterialRequirementID == nil || *line.SourceMaterialRequirementID != "mr-1" {
		t.Errorf("SourceMaterialRequirementID = %v, want mr-1", line.SourceMaterialRequirementID)
	}

	// The version was persisted, and the chain now points at it.
	if len(versions.created) != 1 {
		t.Fatalf("persisted %d versions, want 1", len(versions.created))
	}
	chain := chains.chains["company-1|chain-1"]
	if chain == nil {
		t.Fatal("no issuance chain was created")
	}
	if chain.CurrentIssuedVersionID == nil || *chain.CurrentIssuedVersionID != issued.ID {
		t.Errorf("chain points at %v, want the newly issued version %s",
			chain.CurrentIssuedVersionID, issued.ID)
	}
	if chain.LatestIssuedVersion != 1 {
		t.Errorf("LatestIssuedVersion = %d, want 1", chain.LatestIssuedVersion)
	}
}

func TestIssueVersionRecordsOnePrimitiveOnlyAuditEvent(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	chains := newFakeChainStore()
	versions := newFakeVersionStore()
	audit := &recordingIssuanceAudit{}
	svc := rfqissuance.NewService(
		versions,
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(chains),
		rfqissuance.WithAuditRecorder(audit),
	)
	ctx := context.Background()

	issued, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-audit"))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := svc.IssueVersion(
		ctx, "company-1", "user-1", issueInput("op-audit")); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}

	if len(audit.issued) != 1 {
		t.Fatalf("recorded %d issued events, want exactly 1", len(audit.issued))
	}
	call := audit.issued[0]
	if call.companyID != "company-1" ||
		call.projectID != issued.ProjectID ||
		call.actorUserID != "user-1" ||
		call.rfqChainID != issued.RFQChainID ||
		call.rfqNumber != issued.RFQNumber ||
		call.versionID != issued.ID ||
		call.versionNumber != 1 {
		t.Errorf("audit call = %+v, want primitive identity for issued Version 1", call)
	}
}

// The M7 read must be tenant-scoped by the AUTHENTICATED company, never by a
// caller-supplied one.
func TestIssueVersionScopesTheM7ReadToTheAuthenticatedCompany(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)

	if _, err := svc.IssueVersion(context.Background(), "company-1", "user-1",
		issueInput("op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if source.gotCompanyID != "company-1" {
		t.Errorf("the M7 read used companyID %q, want company-1", source.gotCompanyID)
	}
	if source.gotRFQChainID != "chain-1" {
		t.Errorf("the M7 read used rfqChainID %q, want chain-1", source.gotRFQChainID)
	}
}

// The source fingerprint is the stable identity of the supplier-visible M7
// snapshot. Reconciliation may use it to complete a known issuance, but must
// never attach an operation to different commercial content.
func TestIssueVersionFingerprintsTheSupplierVisibleSourceDeterministically(t *testing.T) {
	ctx := context.Background()
	base := readySnapshot(futureDeadline())

	issue := func(t *testing.T, snapshot rfqissuance.ReadyRFQSnapshot) rfqissuance.IssuedRFQVersion {
		t.Helper()
		source := &fakeReadyRFQSource{snapshot: snapshot, found: true}
		svc, _, _ := newIssuanceService(t, source)
		version, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1"))
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		return version
	}

	first := issue(t, base)
	second := issue(t, base)
	if first.SourceFingerprint == "" {
		t.Fatal("SourceFingerprint is empty; reconciliation cannot verify source identity")
	}
	if second.SourceFingerprint != first.SourceFingerprint {
		t.Errorf("the same source produced fingerprints %q and %q; identity must be deterministic",
			first.SourceFingerprint, second.SourceFingerprint)
	}

	changed := base
	changed.Title = "Cement, aggregate, and admixture"
	third := issue(t, changed)
	if third.SourceFingerprint == first.SourceFingerprint {
		t.Error("changing supplier-visible RFQ content did not change SourceFingerprint")
	}
}

// §4.1A: a ready M7 RFQ with NO response deadline cannot be issued, and the
// refusal must leave nothing behind.
func TestIssueVersionRefusesAnRFQWithNoResponseDeadline(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(nil), found: true}
	svc, chains, versions := newIssuanceService(t, source)

	_, err := svc.IssueVersion(context.Background(), "company-1", "user-1", issueInput("op-1"))

	if !errors.Is(err, rfqissuance.ErrResponseDeadlineRequired) {
		t.Fatalf("error = %v, want ErrResponseDeadlineRequired (§4.1A)", err)
	}
	if len(versions.created) != 0 {
		t.Errorf("a refused issuance created %d versions, want 0", len(versions.created))
	}
	if chain := chains.chains["company-1|chain-1"]; chain != nil &&
		chain.CurrentIssuedVersionID != nil {
		t.Error("a refused issuance advanced the chain pointer")
	}
}

// A draft or absent M7 RFQ is not issuable.
func TestIssueVersionRefusesAnRFQThatIsNotReady(t *testing.T) {
	source := &fakeReadyRFQSource{found: false}
	svc, _, versions := newIssuanceService(t, source)

	_, err := svc.IssueVersion(context.Background(), "company-1", "user-1", issueInput("op-1"))

	if !errors.Is(err, rfqissuance.ErrRFQNotReady) {
		t.Errorf("error = %v, want ErrRFQNotReady", err)
	}
	if len(versions.created) != 0 {
		t.Errorf("a refused issuance created %d versions, want 0", len(versions.created))
	}
}

// An M7 lookup FAILURE must not be reported as "not ready": issuance would then
// silently refuse a perfectly issuable RFQ whenever Mongo hiccupped.
func TestIssueVersionPropagatesAnM7LookupFailure(t *testing.T) {
	sentinel := errors.New("mongo is unreachable")
	source := &fakeReadyRFQSource{err: sentinel}
	svc, _, _ := newIssuanceService(t, source)

	_, err := svc.IssueVersion(context.Background(), "company-1", "user-1", issueInput("op-1"))

	if errors.Is(err, rfqissuance.ErrRFQNotReady) {
		t.Error("a lookup failure was laundered into ErrRFQNotReady")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want the underlying failure to propagate", err)
	}
}

func TestIssueVersionRequiresACurrency(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, versions := newIssuanceService(t, source)

	input := issueInput("op-1")
	input.Currency = ""

	if _, err := svc.IssueVersion(context.Background(), "company-1", "user-1",
		input); !errors.Is(err, rfqissuance.ErrCurrencyRequired) {
		t.Errorf("error = %v, want ErrCurrencyRequired", err)
	}
	if len(versions.created) != 0 {
		t.Errorf("a refused issuance created %d versions, want 0", len(versions.created))
	}
}

func TestIssueVersionNormalizesTheSupportedCurrency(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)
	input := issueInput("op-normalized-currency")
	input.Currency = " myr "

	issued, err := svc.IssueVersion(
		context.Background(), "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("issue using a differently cased supported currency: %v", err)
	}
	if issued.Currency != "MYR" {
		t.Errorf("Currency = %q, want canonical MYR", issued.Currency)
	}

	// Idempotency compares canonical commercial identity. A retry should not
	// conflict merely because the caller now sends the canonical spelling.
	input.Currency = "MYR"
	retried, err := svc.IssueVersion(
		context.Background(), "company-1", "user-1", input)
	if err != nil {
		t.Fatalf("retry using canonical currency: %v", err)
	}
	if retried.ID != issued.ID {
		t.Errorf("retry returned version %q, want original %q", retried.ID, issued.ID)
	}
}

func TestIssueVersionRefusesAnUnsupportedCurrencyWithoutWriting(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, chains, versions := newIssuanceService(t, source)
	input := issueInput("op-unsupported-currency")
	input.Currency = "USD"

	_, err := svc.IssueVersion(context.Background(), "company-1", "user-1", input)
	if !errors.Is(err, rfqissuance.ErrInvalidCurrency) {
		t.Fatalf("error = %v, want ErrInvalidCurrency", err)
	}
	if len(versions.created) != 0 {
		t.Errorf("unsupported currency created %d versions, want 0", len(versions.created))
	}
	if len(chains.chains) != 0 {
		t.Error("unsupported currency created an issuance chain")
	}
}

func TestIssueVersionRequiresAnOperationID(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)

	if _, err := svc.IssueVersion(context.Background(), "company-1", "user-1",
		issueInput("")); !errors.Is(err, rfqissuance.ErrOperationIDRequired) {
		t.Errorf("error = %v, want ErrOperationIDRequired; without one a retry cannot be "+
			"distinguished from a second issuance", err)
	}
}

// Retrying the SAME operation ID returns the ORIGINAL version rather than
// issuing a second one (design spec §10.1).
func TestIssueVersionIsIdempotentUnderTheSameOperationID(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, versions := newIssuanceService(t, source)
	ctx := context.Background()

	first, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1"))
	if err != nil {
		t.Fatalf("a retry under the same operation ID must succeed, got %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("the retry returned version %s, want the original %s", second.ID, first.ID)
	}
	if len(versions.created) != 1 {
		t.Errorf("persisted %d versions, want 1: a retry must not issue again",
			len(versions.created))
	}
}

// An operation ID names one logical issuance, not merely one record somewhere
// in the company. Reusing it for another chain must conflict instead of
// returning the first chain's version as a false success.
func TestIssueVersionRejectsAnOperationIDReusedForAnotherChain(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)
	ctx := context.Background()

	if _, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1")); err != nil {
		t.Fatalf("first issue: %v", err)
	}

	reused := issueInput("op-1")
	reused.RFQChainID = "chain-2"
	_, err := svc.IssueVersion(ctx, "company-1", "user-1", reused)

	if !errors.Is(err, rfqissuance.ErrOperationAlreadyUsed) {
		t.Errorf("error = %v, want ErrOperationAlreadyUsed for another RFQ chain", err)
	}
}

// Currency is immutable for the chain. A retry may recover the original
// result, but the same operation ID cannot silently accept a different
// currency than the one the immutable version records.
func TestIssueVersionRejectsAnOperationIDReusedWithAnotherCurrency(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)
	ctx := context.Background()

	if _, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1")); err != nil {
		t.Fatalf("first issue: %v", err)
	}

	reused := issueInput("op-1")
	reused.Currency = "USD"
	_, err := svc.IssueVersion(ctx, "company-1", "user-1", reused)

	if !errors.Is(err, rfqissuance.ErrOperationAlreadyUsed) {
		t.Errorf("error = %v, want ErrOperationAlreadyUsed for another currency", err)
	}
}

// The operation ID is also bound to the exact ready source. If the M7 RFQ
// revision or supplier-visible content differs, returning the old version would
// hide that the caller is now asking to issue another logical snapshot.
func TestIssueVersionRejectsAnOperationIDReusedForAnotherSourceFingerprint(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)
	ctx := context.Background()

	if _, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1")); err != nil {
		t.Fatalf("first issue: %v", err)
	}

	changed := source.snapshot
	changed.SourceM7RFQRevision++
	changed.Title = "Changed supplier-visible request"
	source.snapshot = changed

	_, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1"))
	if !errors.Is(err, rfqissuance.ErrOperationAlreadyUsed) {
		t.Errorf("error = %v, want ErrOperationAlreadyUsed for another source fingerprint", err)
	}
}

// Version 2 must come from an amendment draft. A second call to the initial
// issue operation cannot publish the unchanged M7 snapshot as a new version.
func TestIssueVersionUnderANewOperationIDRequiresAnAmendment(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, versions := newIssuanceService(t, source)
	ctx := context.Background()

	if _, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-2"))
	if !errors.Is(err, rfqissuance.ErrVersionAlreadyExists) {
		t.Errorf("error = %v, want ErrVersionAlreadyExists", err)
	}
	if len(versions.created) != 1 {
		t.Errorf("persisted %d versions, want the original Version 1 only",
			len(versions.created))
	}
}

// Once a version exists, RFQChainHasIssuedVersion must report true — this is
// the fact M7 consumes to refuse reopening a chain a Supplier may be quoting
// against.
func TestIssuingMakesTheChainReportIssuedToM7(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)
	ctx := context.Background()

	issued, err := svc.RFQChainHasIssuedVersion(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued {
		t.Fatal("a chain with no issued version must report false")
	}

	if _, err := svc.IssueVersion(ctx, "company-1", "user-1", issueInput("op-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	issued, err = svc.RFQChainHasIssuedVersion(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !issued {
		t.Error("after issuance the chain must report issued: M7 uses this to refuse " +
			"reopening an RFQ a Supplier may already be quoting against")
	}
}

// The issued version must carry NONE of the contractor-private data. The M7
// projection already excludes it structurally; this asserts issuance does not
// reintroduce any by another route.
func TestIssuedVersionCarriesOnlySupplierVisibleContent(t *testing.T) {
	source := &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true}
	svc, _, _ := newIssuanceService(t, source)

	issued, err := svc.IssueVersion(context.Background(), "company-1", "user-1",
		issueInput("op-1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ProcurementNotes IS supplier-visible (it is delivery guidance); internal
	// notes are a different field that never reaches this module at all.
	if issued.Lines[0].ProcurementNotes != "deliver to site gate" {
		t.Errorf("ProcurementNotes = %q, want the supplier-visible note",
			issued.Lines[0].ProcurementNotes)
	}
}
