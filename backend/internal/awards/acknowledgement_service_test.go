package awards

import (
	"context"
	"errors"
	"testing"
	"time"
)

// F9 service behavior (§8J). The public route lives in supplieraccess, which
// owns the Phase D session and CSRF; this module owns the record.

type fakeAcknowledgementStore struct {
	byOutcome map[string]AwardOutcomeAcknowledgement
	nextID    int
}

func newFakeAcknowledgementStore() *fakeAcknowledgementStore {
	return &fakeAcknowledgementStore{
		byOutcome: map[string]AwardOutcomeAcknowledgement{},
	}
}

func (f *fakeAcknowledgementStore) EnsureAcknowledgement(
	_ context.Context, candidate AwardOutcomeAcknowledgement,
) (AwardOutcomeAcknowledgement, bool, error) {
	if candidate.AcknowledgedAt.IsZero() {
		candidate.AcknowledgedAt = time.Now().UTC()
	}
	if err := candidate.Validate(); err != nil {
		return AwardOutcomeAcknowledgement{}, false, err
	}
	key := candidate.CompanyID + "|" + candidate.AwardOutcomeID
	if existing, ok := f.byOutcome[key]; ok {
		return existing, false, nil
	}
	f.nextID++
	candidate.ID = string(rune('a'+f.nextID)) + "-ack"
	f.byOutcome[key] = candidate
	return candidate, true, nil
}

func (f *fakeAcknowledgementStore) FindAcknowledgement(
	_ context.Context, companyID, outcomeID string,
) (AwardOutcomeAcknowledgement, bool, error) {
	acknowledgement, ok := f.byOutcome[companyID+"|"+outcomeID]
	return acknowledgement, ok, nil
}

func acknowledgementService() (
	*Service, *fakeAcknowledgementStore, *fakeAwardAuditRecorder) {
	store := newFakeAcknowledgementStore()
	audit := &fakeAwardAuditRecorder{}
	outcomes := &fakeOutcomeStore{outcomes: map[string]AwardOutcome{
		"outcome-1": {
			ID: "outcome-1", CompanyID: "company-1",
			AwardChainID: "chain-1", AwardRevisionID: "revision-1",
			SupplierID: "supplier-a", InvitationID: "invitation-a",
			Result: OutcomeSelected,
		},
	}}
	service := NewService(
		WithAwardOutcomeRepository(outcomes),
		WithAwardAcknowledgementRepository(store),
		WithAwardAuditRecorder(audit),
	)
	return service, store, audit
}

func acknowledgeInput(operationID, recipient string) AcknowledgeOutcomeInput {
	return AcknowledgeOutcomeInput{
		OutcomeID: "outcome-1", SupplierID: "supplier-a",
		InvitationID: "invitation-a", SessionID: "session-1",
		RecipientIdentity: recipient, OperationID: operationID,
		AcknowledgedAt: calcAt,
	}
}

// The first receipt records the acknowledging identity and reports creation.
func TestAcknowledgeOutcomeRecordsTheFirstReceipt(t *testing.T) {
	service, _, _ := acknowledgementService()

	acknowledgement, created, err := service.AcknowledgeOutcome(
		context.Background(), "company-1",
		acknowledgeInput("op-1", "buyer@example.test"))
	if err != nil {
		t.Fatalf("AcknowledgeOutcome: %v", err)
	}
	if !created {
		t.Fatal("the first acknowledgement must report creation")
	}
	if acknowledgement.RecipientIdentity != "buyer@example.test" {
		t.Errorf("recipient = %q, want the acknowledging identity",
			acknowledgement.RecipientIdentity)
	}
}

// A replacement recipient MAY acknowledge, and the record captures the identity
// that actually did so — but it never overwrites an existing receipt.
func TestAReplacementRecipientDoesNotOverwriteTheReceipt(t *testing.T) {
	service, _, _ := acknowledgementService()
	ctx := context.Background()

	if _, _, err := service.AcknowledgeOutcome(ctx, "company-1",
		acknowledgeInput("op-1", "first@example.test")); err != nil {
		t.Fatalf("AcknowledgeOutcome: %v", err)
	}

	second, created, err := service.AcknowledgeOutcome(ctx, "company-1",
		acknowledgeInput("op-2", "replacement@example.test"))
	if err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if created {
		t.Error("a second attempt must not report creation")
	}
	if second.RecipientIdentity != "first@example.test" {
		t.Fatalf("recipient = %q, want the ORIGINAL identity",
			second.RecipientIdentity)
	}
}

// Another Supplier's outcome is a bounded not-found, never a 403 that would
// confirm the outcome exists (D3).
func TestAcknowledgeRefusesAnotherSuppliersOutcome(t *testing.T) {
	service, _, _ := acknowledgementService()

	input := acknowledgeInput("op-1", "intruder@example.test")
	input.SupplierID = "supplier-b"
	input.InvitationID = "invitation-b"

	if _, _, err := service.AcknowledgeOutcome(
		context.Background(), "company-1", input); !errors.Is(
		err, ErrAwardOutcomeNotFound) {
		t.Fatalf("err = %v, want ErrAwardOutcomeNotFound", err)
	}
}

// The right Supplier under the WRONG invitation is also refused: the scope is
// Supplier AND Invitation, not either alone.
func TestAcknowledgeRefusesAMismatchedInvitation(t *testing.T) {
	service, _, _ := acknowledgementService()

	input := acknowledgeInput("op-1", "buyer@example.test")
	input.InvitationID = "invitation-other"

	if _, _, err := service.AcknowledgeOutcome(
		context.Background(), "company-1", input); !errors.Is(
		err, ErrAwardOutcomeNotFound) {
		t.Fatalf("err = %v, want ErrAwardOutcomeNotFound", err)
	}
}

// The audit event is emitted once, only for the authoritative first receipt.
func TestAcknowledgementAuditIsEmittedOnlyForTheFirstReceipt(t *testing.T) {
	service, _, audit := acknowledgementService()
	ctx := context.Background()

	for attempt := 0; attempt < 3; attempt++ {
		if _, _, err := service.AcknowledgeOutcome(ctx, "company-1",
			acknowledgeInput("op-1", "buyer@example.test")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	acknowledged := 0
	for _, event := range audit.events {
		if event == "award_outcome_acknowledged" {
			acknowledged++
		}
	}
	if acknowledged != 1 {
		t.Fatalf("award_outcome_acknowledged recorded %d times, want 1",
			acknowledged)
	}
}

func TestAcknowledgementRetryRepairsMissingAudit(t *testing.T) {
	service, _, _ := acknowledgementService()
	service.audit = nil
	input := acknowledgeInput("op-1", "buyer@example.test")
	if _, _, err := service.AcknowledgeOutcome(context.Background(), "company-1", input); err != nil {
		t.Fatalf("acknowledgement without audit: %v", err)
	}
	audit := &fakeAwardAuditRecorder{}
	service.audit = audit
	if _, _, err := service.AcknowledgeOutcome(context.Background(), "company-1", input); err != nil {
		t.Fatalf("acknowledgement recovery: %v", err)
	}
	if audit.recorded["company-1|award_outcome_acknowledged|outcome-1"] != 1 {
		t.Fatalf("missing acknowledgement audit was not repaired: %+v", audit.events)
	}
}

// The Supplier and Invitation are taken from the resolved OUTCOME, not from
// request content, so a caller cannot record a receipt against another party.
func TestAcknowledgementIdentitiesComeFromTheResolvedOutcome(t *testing.T) {
	service, store, _ := acknowledgementService()

	if _, _, err := service.AcknowledgeOutcome(context.Background(),
		"company-1", acknowledgeInput("op-1", "buyer@example.test")); err != nil {
		t.Fatalf("AcknowledgeOutcome: %v", err)
	}

	stored := store.byOutcome["company-1|outcome-1"]
	if stored.SupplierID != "supplier-a" ||
		stored.InvitationID != "invitation-a" {
		t.Fatalf("stored identities = %+v, want the outcome's own", stored)
	}
}

// An inability to acknowledge never blocks the award: the service refuses
// cleanly and nothing about the published award changes (§8J).
func TestAcknowledgementFailureLeavesTheAwardUntouched(t *testing.T) {
	service, store, _ := acknowledgementService()

	input := acknowledgeInput("op-1", "buyer@example.test")
	// A caller with no session — as an expired credential would produce.
	input.SessionID = ""

	if _, _, err := service.AcknowledgeOutcome(
		context.Background(), "company-1", input); !errors.Is(
		err, ErrInvalidAcknowledgement) {
		t.Fatalf("err = %v, want ErrInvalidAcknowledgement", err)
	}
	if len(store.byOutcome) != 0 {
		t.Fatal("a refused acknowledgement must record nothing")
	}
}
