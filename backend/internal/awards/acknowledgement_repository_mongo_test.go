package awards_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
)

// F9 persistence (§8J). One acknowledgement per outcome, and the FIRST receipt
// is never overwritten.

func acknowledgementRepository(
	t *testing.T,
) *awards.MongoAwardAcknowledgementRepository {
	t.Helper()
	repository := awards.NewMongoAwardAcknowledgementRepository(setupDB(t))
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}
	return repository
}

func candidateAcknowledgement(
	operationID, recipient string,
) awards.AwardOutcomeAcknowledgement {
	return awards.AwardOutcomeAcknowledgement{
		CompanyID: "company-1", AwardOutcomeID: "outcome-1",
		SupplierID: "supplier-a", InvitationID: "invitation-a",
		SessionID: "session-1", RecipientIdentity: recipient,
		OperationID: operationID, AcknowledgedAt: time.Now().UTC(),
	}
}

// A same-operation retry returns the existing receipt unchanged.
func TestAcknowledgementRetryReturnsTheSameReceipt(t *testing.T) {
	repository := acknowledgementRepository(t)
	ctx := context.Background()

	first, created, err := repository.EnsureAcknowledgement(
		ctx, candidateAcknowledgement("op-1", "buyer@example.test"))
	if err != nil {
		t.Fatalf("EnsureAcknowledgement: %v", err)
	}
	if !created {
		t.Fatal("the first acknowledgement must report creation")
	}

	second, created, err := repository.EnsureAcknowledgement(
		ctx, candidateAcknowledgement("op-1", "buyer@example.test"))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if created {
		t.Error("a retry must not create a second receipt")
	}
	if first.ID != second.ID {
		t.Fatalf("the retry created a different receipt: %s then %s",
			first.ID, second.ID)
	}
	// Exact equality: the repository returns the STORED receipt on creation as
	// well as on retry, so a caller never sees the in-memory timestamp and the
	// persisted one disagree.
	if !first.AcknowledgedAt.Equal(second.AcknowledgedAt) {
		t.Fatalf("the retry changed the receipt time: %v then %v",
			first.AcknowledgedAt, second.AcknowledgedAt)
	}
}

// A LATER attempt by a DIFFERENT identity returns the existing receipt: the
// first receipt is the true one, and a second attempt does not rewrite history.
func TestASecondIdentityDoesNotOverwriteTheFirstReceipt(t *testing.T) {
	repository := acknowledgementRepository(t)
	ctx := context.Background()

	first, _, err := repository.EnsureAcknowledgement(
		ctx, candidateAcknowledgement("op-1", "first@example.test"))
	if err != nil {
		t.Fatalf("EnsureAcknowledgement: %v", err)
	}

	second, created, err := repository.EnsureAcknowledgement(
		ctx, candidateAcknowledgement("op-2", "replacement@example.test"))
	if err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if created {
		t.Error("a second identity must not create another receipt")
	}
	if second.RecipientIdentity != "first@example.test" {
		t.Fatalf("recipient = %q, want the ORIGINAL acknowledging identity",
			second.RecipientIdentity)
	}
	if second.ID != first.ID {
		t.Fatal("the original receipt must be returned unchanged")
	}
}

// Concurrent acknowledgements produce exactly ONE record, and exactly one
// caller is told it created it — which is what makes the audit event singular.
func TestConcurrentAcknowledgementsProduceOneReceipt(t *testing.T) {
	repository := acknowledgementRepository(t)
	ctx := context.Background()

	const attempts = 8
	ids := make([]string, attempts)
	creations := make([]bool, attempts)
	errs := make([]error, attempts)
	var wait sync.WaitGroup
	wait.Add(attempts)
	for index := 0; index < attempts; index++ {
		go func(index int) {
			defer wait.Done()
			acknowledgement, created, err := repository.EnsureAcknowledgement(
				ctx, candidateAcknowledgement("op-1", "buyer@example.test"))
			ids[index] = acknowledgement.ID
			creations[index] = created
			errs[index] = err
		}(index)
	}
	wait.Wait()

	unique := map[string]bool{}
	created := 0
	for index, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: %v", index, err)
		}
		unique[ids[index]] = true
		if creations[index] {
			created++
		}
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent acknowledgements produced %d receipts, want 1",
			len(unique))
	}
	if created != 1 {
		t.Fatalf("%d callers were told they created the receipt, want exactly 1",
			created)
	}
}

// A foreign tenant cannot read another company's receipt.
func TestAcknowledgementsAreTenantScoped(t *testing.T) {
	repository := acknowledgementRepository(t)
	ctx := context.Background()

	if _, _, err := repository.EnsureAcknowledgement(
		ctx, candidateAcknowledgement("op-1", "buyer@example.test")); err != nil {
		t.Fatalf("EnsureAcknowledgement: %v", err)
	}

	if _, found, err := repository.FindAcknowledgement(
		ctx, "company-2", "outcome-1"); err != nil {
		t.Fatalf("FindAcknowledgement: %v", err)
	} else if found {
		t.Fatal("a foreign company must not read another tenant's receipt")
	}
}

// Two different outcomes each carry their own receipt.
func TestEachOutcomeCarriesItsOwnAcknowledgement(t *testing.T) {
	repository := acknowledgementRepository(t)
	ctx := context.Background()

	if _, _, err := repository.EnsureAcknowledgement(
		ctx, candidateAcknowledgement("op-1", "buyer@example.test")); err != nil {
		t.Fatalf("EnsureAcknowledgement: %v", err)
	}

	other := candidateAcknowledgement("op-2", "buyer@example.test")
	other.AwardOutcomeID = "outcome-2"
	if _, created, err := repository.EnsureAcknowledgement(ctx, other); err != nil {
		t.Fatalf("EnsureAcknowledgement: %v", err)
	} else if !created {
		t.Fatal("a different outcome must carry its own receipt")
	}
}
