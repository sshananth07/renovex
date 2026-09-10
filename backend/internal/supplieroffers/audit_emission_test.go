package supplieroffers

import (
	"context"
	"testing"
	"time"
)

// recordedAuditEvent captures one primitive-only emission.
type recordedAuditEvent struct {
	kind           string
	companyID      string
	supplierID     string
	invitationID   string
	offerChainID   string
	draftID        string
	offerVersionID string
	withdrawalID   string
	versionNumber  int
	revision       int64
}

type recordingAuditRecorder struct {
	events []recordedAuditEvent
}

func (r *recordingAuditRecorder) RecordOfferDraftCreated(
	_ context.Context, companyID, supplierID, invitationID, offerChainID,
	draftID string, revision int64, _ time.Time) error {
	r.events = append(r.events, recordedAuditEvent{
		kind: "draft_created", companyID: companyID, supplierID: supplierID,
		invitationID: invitationID, offerChainID: offerChainID,
		draftID: draftID, revision: revision,
	})
	return nil
}

func (r *recordingAuditRecorder) RecordOfferDraftCopied(
	_ context.Context, companyID, supplierID, invitationID, offerChainID,
	draftID, sourceOfferVersionID string, revision int64, _ time.Time) error {
	r.events = append(r.events, recordedAuditEvent{
		kind: "draft_copied", companyID: companyID, supplierID: supplierID,
		invitationID: invitationID, offerChainID: offerChainID,
		draftID: draftID, offerVersionID: sourceOfferVersionID, revision: revision,
	})
	return nil
}

func (r *recordingAuditRecorder) RecordOfferDraftUpdated(
	_ context.Context, companyID, supplierID, invitationID, offerChainID,
	draftID string, revision int64, _ time.Time) error {
	r.events = append(r.events, recordedAuditEvent{
		kind: "draft_updated", companyID: companyID, supplierID: supplierID,
		invitationID: invitationID, offerChainID: offerChainID,
		draftID: draftID, revision: revision,
	})
	return nil
}

func (r *recordingAuditRecorder) RecordOfferDraftArchived(
	_ context.Context, companyID, supplierID, invitationID, offerChainID,
	draftID string, revision int64, _ time.Time) error {
	r.events = append(r.events, recordedAuditEvent{
		kind: "draft_archived", companyID: companyID, supplierID: supplierID,
		invitationID: invitationID, offerChainID: offerChainID,
		draftID: draftID, revision: revision,
	})
	return nil
}

func (r *recordingAuditRecorder) RecordOfferSubmitted(
	_ context.Context, companyID, supplierID, invitationID, offerChainID,
	draftID, offerVersionID string, versionNumber int, _ time.Time) error {
	for _, event := range r.events {
		if event.kind == "offer_submitted" && event.companyID == companyID &&
			event.offerVersionID == offerVersionID {
			return nil
		}
	}
	r.events = append(r.events, recordedAuditEvent{
		kind: "offer_submitted", companyID: companyID, supplierID: supplierID,
		invitationID: invitationID, offerChainID: offerChainID, draftID: draftID,
		offerVersionID: offerVersionID, versionNumber: versionNumber,
	})
	return nil
}

func (r *recordingAuditRecorder) RecordOfferSubmissionReconciled(
	_ context.Context, companyID, supplierID, invitationID, offerChainID,
	offerVersionID string, versionNumber int, _ time.Time) error {
	r.events = append(r.events, recordedAuditEvent{
		kind: "submission_reconciled", companyID: companyID, supplierID: supplierID,
		invitationID: invitationID, offerChainID: offerChainID,
		offerVersionID: offerVersionID, versionNumber: versionNumber,
	})
	return nil
}

func (r *recordingAuditRecorder) RecordOfferWithdrawn(
	_ context.Context, companyID, supplierID, invitationID, offerChainID,
	offerVersionID, withdrawalID string, _ time.Time) error {
	for _, event := range r.events {
		if event.kind == "offer_withdrawn" && event.companyID == companyID &&
			event.withdrawalID == withdrawalID {
			return nil
		}
	}
	r.events = append(r.events, recordedAuditEvent{
		kind: "offer_withdrawn", companyID: companyID, supplierID: supplierID,
		invitationID: invitationID, offerChainID: offerChainID,
		offerVersionID: offerVersionID, withdrawalID: withdrawalID,
	})
	return nil
}

func (r *recordingAuditRecorder) countOf(kind string) int {
	count := 0
	for _, event := range r.events {
		if event.kind == kind {
			count++
		}
	}
	return count
}

// Submission emits exactly one audit event for the authoritative write.
func TestSubmissionEmitsOneAuditEvent(t *testing.T) {
	rig := newSubmissionRig(t)
	recorder := &recordingAuditRecorder{}
	rig.service.audit = recorder
	ctx := context.Background()

	version, err := rig.service.SubmitOffer(ctx, rig.submitInput)
	if err != nil {
		t.Fatalf("SubmitOffer: %v", err)
	}

	if got := recorder.countOf("offer_submitted"); got != 1 {
		t.Fatalf("offer_submitted events = %d, want exactly 1", got)
	}
	event := recorder.events[len(recorder.events)-1]
	if event.companyID != "company-1" || event.supplierID != "supplier-1" ||
		event.invitationID != "invitation-1" ||
		event.offerVersionID != version.ID ||
		event.versionNumber != version.VersionNumber {
		t.Errorf("event = %+v, want the exact submission identity", event)
	}
}

// An idempotent retry must NOT duplicate the audit event: the offer was
// submitted once, so the audit trail must show one submission.
func TestSubmissionRetryDoesNotDuplicateAudit(t *testing.T) {
	rig := newSubmissionRig(t)
	recorder := &recordingAuditRecorder{}
	rig.service.audit = recorder
	ctx := context.Background()

	if _, err := rig.service.SubmitOffer(ctx, rig.submitInput); err != nil {
		t.Fatalf("first submission: %v", err)
	}
	if _, err := rig.service.SubmitOffer(ctx, rig.submitInput); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if got := recorder.countOf("offer_submitted"); got != 1 {
		t.Errorf("offer_submitted events = %d after a retry, want exactly 1: "+
			"an idempotent replay is not a second submission", got)
	}
}

func TestSubmissionRetryRepairsMissingAuditAfterAuthoritativeCompletion(t *testing.T) {
	rig := newSubmissionRig(t)
	if _, err := rig.service.SubmitOffer(context.Background(), rig.submitInput); err != nil {
		t.Fatalf("submission without recorder: %v", err)
	}
	recorder := &recordingAuditRecorder{}
	rig.service.audit = recorder
	if _, err := rig.service.SubmitOffer(context.Background(), rig.submitInput); err != nil {
		t.Fatalf("recovery submission: %v", err)
	}
	if got := recorder.countOf("offer_submitted"); got != 1 {
		t.Fatalf("repaired offer_submitted events = %d, want one", got)
	}
}

// A refused submission emits nothing: audit records authoritative writes only.
func TestRefusedSubmissionEmitsNoAudit(t *testing.T) {
	rig := newDraftEditRig(t)
	recorder := &recordingAuditRecorder{}
	rig.service.audit = recorder
	versions := NewMongoOfferVersionRepository(rig.db)
	if err := versions.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring version indexes: %v", err)
	}
	rig.service.versions = versions

	// The draft is incomplete, so submission is refused.
	if _, err := rig.service.SubmitOffer(context.Background(), SubmitOfferCommand{
		Context:          rig.input,
		DraftID:          rig.draft.ID,
		ExpectedRevision: rig.draft.Revision,
		OperationID:      "op-submit-1",
	}); err == nil {
		t.Fatal("expected the incomplete submission to be refused")
	}

	if len(recorder.events) != 0 {
		t.Errorf("events = %+v, want none for a refused submission",
			recorder.events)
	}
}

// Withdrawal emits exactly one event carrying both the version and the
// immutable withdrawal identity.
func TestWithdrawalEmitsOneAuditEvent(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	recorder := &recordingAuditRecorder{}
	rig.service.audit = recorder
	ctx := context.Background()

	withdrawal, err := rig.service.WithdrawOffer(ctx, WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "priced in error",
		OperationID:    "op-withdraw-1",
	})
	if err != nil {
		t.Fatalf("WithdrawOffer: %v", err)
	}

	if got := recorder.countOf("offer_withdrawn"); got != 1 {
		t.Fatalf("offer_withdrawn events = %d, want exactly 1", got)
	}
	event := recorder.events[len(recorder.events)-1]
	if event.offerVersionID != version.ID ||
		event.withdrawalID != withdrawal.ID {
		t.Errorf("event = %+v, want the exact withdrawal identity", event)
	}
}

// A withdrawal retry must not duplicate its audit event either.
func TestWithdrawalRetryDoesNotDuplicateAudit(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	recorder := &recordingAuditRecorder{}
	rig.service.audit = recorder
	ctx := context.Background()

	command := WithdrawOfferCommand{
		Context:        rig.input,
		OfferVersionID: version.ID,
		Reason:         "priced in error",
		OperationID:    "op-withdraw-1",
	}
	if _, err := rig.service.WithdrawOffer(ctx, command); err != nil {
		t.Fatalf("first withdrawal: %v", err)
	}
	if _, err := rig.service.WithdrawOffer(ctx, command); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if got := recorder.countOf("offer_withdrawn"); got != 1 {
		t.Errorf("offer_withdrawn events = %d after a retry, want exactly 1", got)
	}
}

func TestWithdrawalRetryRepairsMissingAuditAfterAuthoritativeCompletion(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	command := WithdrawOfferCommand{Context: rig.input, OfferVersionID: version.ID,
		Reason: "priced in error", OperationID: "op-withdraw-repair-audit"}
	if _, err := rig.service.WithdrawOffer(context.Background(), command); err != nil {
		t.Fatalf("withdrawal without recorder: %v", err)
	}
	recorder := &recordingAuditRecorder{}
	rig.service.audit = recorder
	if _, err := rig.service.WithdrawOffer(context.Background(), command); err != nil {
		t.Fatalf("recovery withdrawal: %v", err)
	}
	if got := recorder.countOf("offer_withdrawn"); got != 1 {
		t.Fatalf("repaired offer_withdrawn events = %d, want one", got)
	}
}
