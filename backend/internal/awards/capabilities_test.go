package awards

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

func mustQuantity(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("quantity.New(%q, %q): %v", value, unit, err)
	}
	return q
}

// F0 proves only that the five consumer-owned capabilities of §8A.1 exist with
// the shapes Phase F needs, and that awards depends on nothing but those
// interfaces. There is no business logic to test yet; these tests exist so a
// later checkpoint cannot silently widen a boundary.

type fakeIssuedRFQSource struct {
	snapshot IssuedRFQSnapshot
	found    bool
	err      error
	company  string
	version  string
}

func (f *fakeIssuedRFQSource) GetIssuedRFQForAward(
	_ context.Context,
	companyID, issuedRFQVersionID string,
) (IssuedRFQSnapshot, bool, error) {
	f.company = companyID
	f.version = issuedRFQVersionID
	return f.snapshot, f.found, f.err
}

type fakeOfferVersionSource struct {
	versions map[string]OfferVersionSnapshot
	listed   []OfferVersionSnapshot
	err      error
	company  string
	version  string
}

func (f *fakeOfferVersionSource) GetOfferVersionForAward(
	_ context.Context,
	companyID, offerVersionID string,
) (OfferVersionSnapshot, bool, error) {
	f.company = companyID
	f.version = offerVersionID
	if f.err != nil {
		return OfferVersionSnapshot{}, false, f.err
	}
	snapshot, found := f.versions[offerVersionID]
	return snapshot, found, nil
}

func (f *fakeOfferVersionSource) ListOfferVersionsForIssuedRFQVersion(
	_ context.Context,
	companyID, issuedRFQVersionID string,
) ([]OfferVersionSnapshot, error) {
	f.company = companyID
	f.version = issuedRFQVersionID
	return f.listed, f.err
}

type fakeOfferEligibilityClaimant struct {
	claimed   []string
	completed []string
	released  []string
	err       error
}

func (f *fakeOfferEligibilityClaimant) ClaimOfferForAward(
	_ context.Context,
	input OfferEligibilityClaimRequest,
) (OfferEligibilitySnapshot, error) {
	f.claimed = append(f.claimed, input.OfferVersionID)
	return OfferEligibilitySnapshot{
		OfferVersionID: input.OfferVersionID,
		State:          OfferEligibilityAwardClaimed,
		OperationID:    input.OperationID,
		ClaimID:        input.ClaimID,
		Revision:       input.ExpectedRevision + 1,
	}, f.err
}

func (f *fakeOfferEligibilityClaimant) CompleteOfferAward(
	_ context.Context,
	input OfferEligibilityCompletionRequest,
) (OfferEligibilitySnapshot, error) {
	f.completed = append(f.completed, input.OfferVersionID)
	return OfferEligibilitySnapshot{
		OfferVersionID: input.OfferVersionID,
		State:          OfferEligibilityAwarded,
		OperationID:    input.OperationID,
		Revision:       input.ExpectedRevision + 1,
	}, f.err
}

func (f *fakeOfferEligibilityClaimant) ReleaseOfferAwardClaim(
	_ context.Context,
	input OfferEligibilityReleaseRequest,
) (OfferEligibilitySnapshot, error) {
	f.released = append(f.released, input.OfferVersionID)
	return OfferEligibilitySnapshot{
		OfferVersionID: input.OfferVersionID,
		State:          OfferEligibilityEligible,
		Revision:       input.ExpectedRevision + 1,
	}, f.err
}

func (f *fakeOfferEligibilityClaimant) GetOfferEligibility(
	_ context.Context,
	companyID, offerVersionID string,
) (OfferEligibilitySnapshot, bool, error) {
	return OfferEligibilitySnapshot{OfferVersionID: offerVersionID}, true, f.err
}

type fakeAwardNotificationMailer struct {
	sent        []AwardOutcomeNotification
	projections []OutcomeProjection
	err         error
}

func (f *fakeAwardNotificationMailer) SendAwardOutcomeNotification(
	_ context.Context,
	notification AwardOutcomeNotification,
	projection OutcomeProjection,
) error {
	f.sent = append(f.sent, notification)
	f.projections = append(f.projections, projection)
	return f.err
}

// fakeAwardAuditRecorder models the ensure-once sink of §8F: events are keyed
// by their deterministic identity, so recording the same authoritative subject
// twice yields one event, and a failing sink records nothing at all.
type fakeAwardAuditRecorder struct {
	events   []string
	recorded map[string]int
	err      error
}

func (f *fakeAwardAuditRecorder) record(identity, event string) error {
	if f.err != nil {
		return f.err
	}
	if f.recorded == nil {
		f.recorded = map[string]int{}
	}
	// Ensure-once, not first-writer-only (§8F): an identity already present
	// no-ops, so exactly one event exists per authoritative subject no matter
	// which completion path runs or how many times it runs.
	if f.recorded[identity] > 0 {
		return nil
	}
	f.recorded[identity]++
	f.events = append(f.events, event)
	return nil
}

func (f *fakeAwardAuditRecorder) RecordAwardDraftCreated(
	_ context.Context, _, _, _, _ string, _ int64, _ time.Time,
) error {
	f.events = append(f.events, "award_draft_created")
	return f.err
}

func (f *fakeAwardAuditRecorder) RecordAwardDraftUpdated(
	_ context.Context, _, _, _, _ string, _ int64, _ time.Time,
) error {
	f.events = append(f.events, "award_draft_updated")
	return f.err
}

func (f *fakeAwardAuditRecorder) RecordAwardDraftDiscarded(
	_ context.Context, _, _, _, _ string, _ int64, _ time.Time,
) error {
	f.events = append(f.events, "award_draft_discarded")
	return f.err
}

func (f *fakeAwardAuditRecorder) RecordAwardFinalised(
	_ context.Context,
	companyID, _, _, awardRevisionID, operationID string,
	_ int, _ time.Time,
) error {
	// The deterministic identity of §8F: company + event + revision +
	// operation. Recording twice for the same subject must yield one event.
	return f.record(
		companyID+"|award_finalised|"+awardRevisionID+"|"+operationID,
		"award_finalised")
}

func (f *fakeAwardAuditRecorder) RecordAwardCorrected(
	_ context.Context,
	companyID, _, _, awardRevisionID, _, operationID string,
	_ int, _ time.Time,
) error {
	// The same ensure-once identity as award_finalised, substituting this
	// event's own authoritative subject (§8F).
	return f.record(
		companyID+"|award_corrected|"+awardRevisionID+"|"+operationID,
		"award_corrected")
}

func (f *fakeAwardAuditRecorder) RecordAwardOutcomeGenerated(
	_ context.Context, companyID, _, _, awardRevisionID, _, _, outcomeID, _ string,
	_ time.Time,
) error {
	return f.record(companyID+"|award_outcome_generated|"+outcomeID+"|"+awardRevisionID,
		"award_outcome_generated")
}

func (f *fakeAwardAuditRecorder) RecordAwardOutcomeNotified(
	_ context.Context, companyID, _, _, _, _, deliveryID, operationID string,
	_ time.Time,
) error {
	return f.record(companyID+"|award_outcome_notified|"+deliveryID+"|"+operationID,
		"award_outcome_notified")
}

func (f *fakeAwardAuditRecorder) RecordAwardOutcomeNotificationRetried(
	_ context.Context, companyID, _, _, _, _, deliveryID, operationID string,
	_ time.Time,
) error {
	return f.record(companyID+"|award_outcome_notification_retried|"+deliveryID+"|"+operationID,
		"award_outcome_notification_retried")
}

func (f *fakeAwardAuditRecorder) RecordAwardOutcomeNotificationObsoleted(
	_ context.Context, _, _, _, _, _, _ string, _ time.Time,
) error {
	f.events = append(f.events, "award_outcome_notification_obsoleted")
	return f.err
}

func (f *fakeAwardAuditRecorder) RecordAwardOutcomeAcknowledged(
	_ context.Context, companyID, outcomeID, _, _, _ string, _ time.Time,
) error {
	return f.record(companyID+"|award_outcome_acknowledged|"+outcomeID,
		"award_outcome_acknowledged")
}

func (f *fakeAwardAuditRecorder) RecordAwardReconciled(
	_ context.Context, _, _, _, _ string, _ int, _ time.Time,
) error {
	f.events = append(f.events, "award_reconciled")
	return f.err
}

// Each fake must satisfy the capability the composition root will implement.
// If a signature drifts, this fails at compile time, which is the point.
var (
	_ IssuedRFQSource          = (*fakeIssuedRFQSource)(nil)
	_ OfferVersionSource       = (*fakeOfferVersionSource)(nil)
	_ OfferEligibilityClaimant = (*fakeOfferEligibilityClaimant)(nil)
	_ AwardNotificationMailer  = (*fakeAwardNotificationMailer)(nil)
	_ AwardAuditRecorder       = (*fakeAwardAuditRecorder)(nil)
)

// The issued-RFQ capability is company-scoped: the authenticated Company is
// always part of the lookup, so a caller cannot read another tenant's RFQ by
// naming its version ID alone.
func TestIssuedRFQSourceLookupIsCompanyScoped(t *testing.T) {
	source := &fakeIssuedRFQSource{
		snapshot: IssuedRFQSnapshot{
			ID:         "issued-1",
			CompanyID:  "company-1",
			RFQChainID: "chain-1",
			Currency:   "MYR",
			Lines: []IssuedRFQLineSnapshot{{
				ID:        "line-1",
				LineageID: "lineage-1",
				Quantity:  mustQuantity(t, "10", "unit"),
			}},
		},
		found: true,
	}

	snapshot, found, err := source.GetIssuedRFQForAward(
		context.Background(), "company-1", "issued-1")
	if err != nil || !found {
		t.Fatalf("GetIssuedRFQForAward: found=%v err=%v", found, err)
	}
	if source.company != "company-1" || source.version != "issued-1" {
		t.Fatalf("lookup did not carry both identities: company=%q version=%q",
			source.company, source.version)
	}
	if snapshot.Lines[0].LineageID != "lineage-1" {
		t.Fatalf("issued line must carry its stable lineage, got %q",
			snapshot.Lines[0].LineageID)
	}
}

// F4's line claims are keyed on the stable lineage, so an issued line that did
// not carry one would make cross-version duplicate-award prevention impossible.
func TestIssuedRFQLineSnapshotCarriesStableLineage(t *testing.T) {
	line := IssuedRFQLineSnapshot{ID: "line-1", LineageID: "lineage-1"}
	if line.LineageID == "" {
		t.Fatal("issued RFQ line must expose its stable lineage ID (§8E)")
	}
}

// The offer snapshot must carry the Supplier and Invitation identities: F7
// scopes outcomes to Supplier + Invitation (D3), and the immutable Offer
// Version alone does not name its Supplier.
func TestOfferVersionSnapshotCarriesSupplierAndInvitation(t *testing.T) {
	snapshot := OfferVersionSnapshot{
		ID:                 "offer-1",
		CompanyID:          "company-1",
		SupplierID:         "supplier-1",
		InvitationID:       "invitation-1",
		IssuedRFQVersionID: "issued-1",
		Currency:           "MYR",
		GrandTotal:         money.New(1000, "MYR"),
	}
	if snapshot.SupplierID == "" || snapshot.InvitationID == "" {
		t.Fatal("offer version snapshot must name Supplier and Invitation (D3)")
	}
}

// A claim, its completion and its release are three distinct operations. The
// release path is the compensating one, and §8E requires the awards service —
// not Phase E — to verify no revision exists before calling it.
func TestOfferEligibilityClaimantExposesThreeDistinctTransitions(t *testing.T) {
	claimant := &fakeOfferEligibilityClaimant{}
	ctx := context.Background()

	if _, err := claimant.ClaimOfferForAward(ctx, OfferEligibilityClaimRequest{
		CompanyID: "company-1", OfferVersionID: "offer-1",
		OperationID: "op-1", ClaimID: "claim-1", ExpectedRevision: 1,
	}); err != nil {
		t.Fatalf("ClaimOfferForAward: %v", err)
	}
	if _, err := claimant.CompleteOfferAward(ctx, OfferEligibilityCompletionRequest{
		CompanyID: "company-1", OfferVersionID: "offer-1",
		OperationID: "op-1", ExpectedRevision: 2,
	}); err != nil {
		t.Fatalf("CompleteOfferAward: %v", err)
	}
	if _, err := claimant.ReleaseOfferAwardClaim(ctx, OfferEligibilityReleaseRequest{
		CompanyID: "company-1", OfferVersionID: "offer-1",
		OperationID: "op-1", ClaimID: "claim-1", ExpectedRevision: 2,
	}); err != nil {
		t.Fatalf("ReleaseOfferAwardClaim: %v", err)
	}

	if len(claimant.claimed) != 1 || len(claimant.completed) != 1 ||
		len(claimant.released) != 1 {
		t.Fatalf("expected one of each transition, got claim=%d complete=%d release=%d",
			len(claimant.claimed), len(claimant.completed), len(claimant.released))
	}
}

// The audit boundary is primitive-only (§8A.1): no domain aggregate, no money
// and no commercial text may cross it, so an audit sink can never become a
// second copy of the award record.
func TestAwardAuditRecorderIsPrimitiveOnly(t *testing.T) {
	recorder := &fakeAwardAuditRecorder{}
	now := time.Now().UTC()

	if err := recorder.RecordAwardFinalised(
		context.Background(),
		"company-1", "user-1", "chain-1", "revision-1", "op-1", 1, now,
	); err != nil {
		t.Fatalf("RecordAwardFinalised: %v", err)
	}
	if len(recorder.events) != 1 || recorder.events[0] != "award_finalised" {
		t.Fatalf("events = %v, want exactly [award_finalised]", recorder.events)
	}
}

// Every bounded failure the Phase F contract names must have a sentinel. A
// checkpoint that invents an unbounded error string later would leave the HTTP
// layer unable to map it to the status code the spec fixes.
func TestBoundedErrorSentinelsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrAwardsNotConfigured":             ErrAwardsNotConfigured,
		"ErrIssuedRFQNotFound":               ErrIssuedRFQNotFound,
		"ErrAwardChainNotFound":              ErrAwardChainNotFound,
		"ErrAwardDraftNotFound":              ErrAwardDraftNotFound,
		"ErrAwardDraftConflict":              ErrAwardDraftConflict,
		"ErrAwardRevisionNotFound":           ErrAwardRevisionNotFound,
		"ErrAwardRevisionConflict":           ErrAwardRevisionConflict,
		"ErrOfferVersionNotSelectable":       ErrOfferVersionNotSelectable,
		"ErrOfferVersionNotEligible":         ErrOfferVersionNotEligible,
		"ErrOfferVersionExpired":             ErrOfferVersionExpired,
		"ErrOfferLineNotQuoted":              ErrOfferLineNotQuoted,
		"ErrQuantityOrUnitMismatch":          ErrQuantityOrUnitMismatch,
		"ErrCurrencyMismatch":                ErrCurrencyMismatch,
		"ErrOfferLevelTaxRequiresComplete":   ErrOfferLevelTaxRequiresComplete,
		"ErrConditionalChargesNotResolvable": ErrConditionalChargesNotResolvable,
		"ErrRFQLineAlreadyAwarded":           ErrRFQLineAlreadyAwarded,
		"ErrRFQLineAwardConflict":            ErrRFQLineAwardConflict,
		"ErrAwardCorrectionNotMonotonic":     ErrAwardCorrectionNotMonotonic,
		"ErrAwardFinalisationPending":        ErrAwardFinalisationPending,
		"ErrAwardOutcomeNotFound":            ErrAwardOutcomeNotFound,
		"ErrAwardDeliveryNotFound":           ErrAwardDeliveryNotFound,
		"ErrInvalidUnawardedReason":          ErrInvalidUnawardedReason,
		"ErrChangeReasonRequired":            ErrChangeReasonRequired,
	}

	seen := map[string]string{}
	for name, sentinel := range sentinels {
		if sentinel == nil {
			t.Fatalf("%s is nil", name)
		}
		message := sentinel.Error()
		if previous, duplicate := seen[message]; duplicate {
			t.Errorf("%s and %s share the message %q; a caller could not tell "+
				"the two bounded failures apart", name, previous, message)
		}
		seen[message] = name

		// errors.Is must distinguish them, which is how the handler maps each
		// bounded failure to the exact status code §8D fixes.
		for otherName, other := range sentinels {
			if otherName == name {
				continue
			}
			if errors.Is(sentinel, other) {
				t.Errorf("%s matches %s under errors.Is", name, otherName)
			}
		}
	}
}
