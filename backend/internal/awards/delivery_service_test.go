package awards

import (
	"context"
	"errors"
	neturl "net/url"
	"strings"
	"testing"
	"time"
)

// F8 send ordering and idempotency (§8I).

type fakeDeliveryStore struct {
	deliveries  map[string]AwardOutcomeDelivery
	byOperation map[string]string
	nextID      int

	// order records every mutation so the test can assert that intent was
	// persisted BEFORE the send, not after.
	order []string
}

func newFakeDeliveryStore() *fakeDeliveryStore {
	return &fakeDeliveryStore{
		deliveries:  map[string]AwardOutcomeDelivery{},
		byOperation: map[string]string{},
	}
}

func (f *fakeDeliveryStore) EnsureDeliveryIntent(
	_ context.Context, candidate AwardOutcomeDelivery,
) (AwardOutcomeDelivery, bool, error) {
	if err := candidate.Validate(); err != nil {
		return AwardOutcomeDelivery{}, false, err
	}
	key := candidate.CompanyID + "|" + candidate.DeliveryOperationID
	if id, exists := f.byOperation[key]; exists {
		return f.deliveries[id], false, nil
	}
	f.nextID++
	candidate.ID = string(rune('a'+f.nextID)) + "-delivery"
	f.deliveries[candidate.ID] = candidate
	f.byOperation[key] = candidate.ID
	f.order = append(f.order, "intent")
	return candidate, true, nil
}

func (f *fakeDeliveryStore) FindDelivery(
	_ context.Context, companyID, deliveryID string,
) (AwardOutcomeDelivery, bool, error) {
	delivery, ok := f.deliveries[deliveryID]
	if !ok || delivery.CompanyID != companyID {
		return AwardOutcomeDelivery{}, false, nil
	}
	return delivery, true, nil
}

func (f *fakeDeliveryStore) FindDeliveryByOperation(
	_ context.Context, companyID, operationID string,
) (AwardOutcomeDelivery, bool, error) {
	id, ok := f.byOperation[companyID+"|"+operationID]
	if !ok {
		return AwardOutcomeDelivery{}, false, nil
	}
	return f.deliveries[id], true, nil
}

func (f *fakeDeliveryStore) MarkDeliverySent(
	_ context.Context, companyID, deliveryID string, sentAt time.Time,
) error {
	delivery, ok := f.deliveries[deliveryID]
	if !ok || delivery.CompanyID != companyID ||
		delivery.Status != DeliveryPending {
		return ErrAwardDeliveryNotFound
	}
	delivery.Status = DeliverySent
	delivery.SentAt = &sentAt
	f.deliveries[deliveryID] = delivery
	f.order = append(f.order, "sent")
	return nil
}

func (f *fakeDeliveryStore) MarkDeliveryFailed(
	_ context.Context, companyID, deliveryID string, code DeliveryFailureCode,
) error {
	delivery, ok := f.deliveries[deliveryID]
	if !ok || delivery.CompanyID != companyID ||
		delivery.Status != DeliveryPending {
		return ErrAwardDeliveryNotFound
	}
	delivery.Status = DeliveryFailed
	delivery.FailureCode = code
	f.deliveries[deliveryID] = delivery
	f.order = append(f.order, "failed")
	return nil
}

func (f *fakeDeliveryStore) ObsoletePendingDeliveries(
	_ context.Context, companyID, revisionID string,
) ([]AwardOutcomeDelivery, error) {
	var affected []AwardOutcomeDelivery
	for id, delivery := range f.deliveries {
		if delivery.CompanyID != companyID ||
			delivery.AwardRevisionID != revisionID ||
			delivery.Status != DeliveryPending {
			continue
		}
		delivery.Status = DeliveryObsolete
		f.deliveries[id] = delivery
		affected = append(affected, delivery)
	}
	return affected, nil
}

func (f *fakeDeliveryStore) ListDeliveries(
	_ context.Context, companyID, revisionID string,
) ([]AwardOutcomeDelivery, error) {
	var deliveries []AwardOutcomeDelivery
	for _, delivery := range f.deliveries {
		if delivery.CompanyID == companyID &&
			delivery.AwardRevisionID == revisionID {
			deliveries = append(deliveries, delivery)
		}
	}
	return deliveries, nil
}

type fakeOutcomeStore struct {
	outcomes map[string]AwardOutcome
}

func (f *fakeOutcomeStore) EnsureOutcome(
	_ context.Context, candidate AwardOutcome,
) (AwardOutcome, bool, error) {
	if existing, ok := f.outcomes[candidate.ID]; ok {
		return existing, false, nil
	}
	f.outcomes[candidate.ID] = candidate
	return candidate, true, nil
}

func (f *fakeOutcomeStore) FindOutcome(
	_ context.Context, companyID, outcomeID string,
) (AwardOutcome, bool, error) {
	outcome, ok := f.outcomes[outcomeID]
	if !ok || outcome.CompanyID != companyID {
		return AwardOutcome{}, false, nil
	}
	return outcome, true, nil
}

func (f *fakeOutcomeStore) FindOutcomeForSupplier(
	_ context.Context, companyID, outcomeID, supplierID, invitationID string,
) (AwardOutcome, bool, error) {
	outcome, ok := f.outcomes[outcomeID]
	if !ok || outcome.CompanyID != companyID ||
		outcome.SupplierID != supplierID ||
		outcome.InvitationID != invitationID {
		return AwardOutcome{}, false, nil
	}
	return outcome, true, nil
}

func (f *fakeOutcomeStore) ListOutcomes(
	_ context.Context, companyID, revisionID string,
) ([]AwardOutcome, error) {
	var outcomes []AwardOutcome
	for _, outcome := range f.outcomes {
		if outcome.CompanyID == companyID &&
			outcome.AwardRevisionID == revisionID {
			outcomes = append(outcomes, outcome)
		}
	}
	return outcomes, nil
}

// fakeInvitationLinkSource stubs the re-derived Supplier Access link. It
// never rotates, mutates or records anything — the same guarantee
// rfqissuance.Service.DeriveInvitationLink itself makes.
type fakeInvitationLinkSource struct {
	link InvitationLink
	err  error
	// calls records every (companyID, invitationID) asked for, so a test can
	// assert the service asked for exactly the outcome's own invitation.
	calls []string
}

func (f *fakeInvitationLinkSource) DeriveInvitationLink(
	_ context.Context, companyID, invitationID string,
) (InvitationLink, error) {
	f.calls = append(f.calls, companyID+"|"+invitationID)
	if f.err != nil {
		return InvitationLink{}, f.err
	}
	return f.link, nil
}

func deliveryService() (
	*Service, *fakeDeliveryStore, *fakeAwardNotificationMailer,
	*fakeAwardAuditRecorder) {
	service, deliveries, mailer, audit, _ := deliveryServiceWithLinks()
	return service, deliveries, mailer, audit
}

func deliveryServiceWithLinks() (
	*Service, *fakeDeliveryStore, *fakeAwardNotificationMailer,
	*fakeAwardAuditRecorder, *fakeInvitationLinkSource) {
	deliveries := newFakeDeliveryStore()
	mailer := &fakeAwardNotificationMailer{}
	audit := &fakeAwardAuditRecorder{}
	links := &fakeInvitationLinkSource{link: InvitationLink{
		Token: "derived-token",
		URL:   "https://app.example.test/supplier-access/open?token=derived-token",
	}}
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
		WithAwardDeliveryRepository(deliveries),
		WithAwardNotificationMailer(mailer),
		WithAwardAuditRecorder(audit),
		WithInvitationLinkSource(links),
	)
	return service, deliveries, mailer, audit, links
}

func sendInput(operationID string) SendOutcomeNotificationInput {
	return SendOutcomeNotificationInput{
		OutcomeID: "outcome-1", DeliveryOperationID: operationID,
		RecipientIdentity: "buyer@example.test", AccessGeneration: 1,
		SentAt: calcAt,
	}
}

// SendOutcomeNotification derives the outcome's OWN invitation link — never
// some other invitation — and builds the outcome URL from it, including a
// returnTo pointing at the specific invitation/outcome pair. This is the
// service-level orchestration Send, Resend and Retry all share.
func TestSendDerivesTheOutcomesOwnInvitationLinkAndBuildsTheOutcomeURL(t *testing.T) {
	service, _, mailer, _, links := deliveryServiceWithLinks()

	if _, err := service.SendOutcomeNotification(context.Background(),
		"company-1", "user-1", sendInput("op-1")); err != nil {
		t.Fatalf("SendOutcomeNotification: %v", err)
	}

	if len(links.calls) != 1 || links.calls[0] != "company-1|invitation-a" {
		t.Fatalf("DeriveInvitationLink calls = %v, want exactly one call for company-1|invitation-a",
			links.calls)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("mails sent = %d, want 1", len(mailer.sent))
	}
	url := mailer.sent[0].OutcomeURL
	if !strings.HasPrefix(url, links.link.URL) {
		t.Fatalf("OutcomeURL = %q, want it built from the derived link %q",
			url, links.link.URL)
	}
	wantReturnTo := "returnTo=" + neturl.QueryEscape(
		"/supplier-access/invitations/invitation-a/outcomes/outcome-1")
	if !strings.Contains(url, wantReturnTo) {
		t.Fatalf("OutcomeURL = %q, want it to contain %q", url, wantReturnTo)
	}
}

// A failure deriving the invitation link (e.g. the invitation was revoked
// since the award was finalised) must fail the send rather than mail a
// Supplier a link that can never resolve.
func TestSendPropagatesAnInvitationLinkDerivationFailure(t *testing.T) {
	service, _, mailer, _, links := deliveryServiceWithLinks()
	links.err = errors.New("invitation revoked")

	if _, err := service.SendOutcomeNotification(context.Background(),
		"company-1", "user-1", sendInput("op-1")); err == nil {
		t.Fatal("expected the send to fail when the invitation link cannot be derived")
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("mails sent = %d, want 0 when the link could not be derived",
			len(mailer.sent))
	}
}

// Intent is persisted BEFORE the send, so a crash mid-send leaves a recoverable
// record rather than a sent email with no trace.
func TestSendPersistsIntentBeforeSending(t *testing.T) {
	service, deliveries, mailer, _ := deliveryService()

	if _, err := service.SendOutcomeNotification(context.Background(),
		"company-1", "user-1", sendInput("op-1")); err != nil {
		t.Fatalf("SendOutcomeNotification: %v", err)
	}

	if len(deliveries.order) < 2 || deliveries.order[0] != "intent" {
		t.Fatalf("mutation order = %v, want intent first", deliveries.order)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("mails sent = %d, want 1", len(mailer.sent))
	}
}

// A same-operation retry resolves the same record and does NOT re-send.
func TestSendIsIdempotentPerOperationAndDoesNotResend(t *testing.T) {
	service, _, mailer, _ := deliveryService()
	ctx := context.Background()

	first, err := service.SendOutcomeNotification(
		ctx, "company-1", "user-1", sendInput("op-1"))
	if err != nil {
		t.Fatalf("SendOutcomeNotification: %v", err)
	}
	second, err := service.SendOutcomeNotification(
		ctx, "company-1", "user-1", sendInput("op-1"))
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("retry produced a different record: %s then %s",
			first.ID, second.ID)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("mails sent = %d; a retry must not tell the Supplier twice",
			len(mailer.sent))
	}
}

// A transport failure records a BOUNDED code and leaves the award intact.
func TestSendFailureRecordsABoundedCodeAndLeavesTheAwardIntact(t *testing.T) {
	service, deliveries, mailer, audit := deliveryService()
	mailer.err = errors.New("dial tcp 10.0.0.5:25: connection refused")

	delivery, err := service.SendOutcomeNotification(context.Background(),
		"company-1", "user-1", sendInput("op-1"))
	if err != nil {
		t.Fatalf("a send failure must not fail the operation: %v", err)
	}
	if delivery.Status != DeliveryFailed {
		t.Fatalf("status = %q, want failed", delivery.Status)
	}
	if !delivery.FailureCode.Valid() {
		t.Fatalf("failure code = %q, want a bounded code", delivery.FailureCode)
	}

	// The raw transport text must not reach the record.
	rendered := renderDelivery(deliveries.deliveries[delivery.ID])
	if containsFold(rendered, "10.0.0.5") ||
		containsFold(rendered, "connection refused") {
		t.Fatalf("the delivery record leaked transport detail: %s", rendered)
	}

	// A failed send is not an authoritative notification, so it is not audited
	// as one.
	for _, event := range audit.events {
		if event == "award_outcome_notified" {
			t.Fatal("a failed send must not be audited as notified")
		}
	}
}

// A successful send is audited exactly once.
func TestSuccessfulSendIsAuditedOnce(t *testing.T) {
	service, _, _, audit := deliveryService()
	ctx := context.Background()

	for attempt := 0; attempt < 3; attempt++ {
		if _, err := service.SendOutcomeNotification(
			ctx, "company-1", "user-1", sendInput("op-1")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	notified := 0
	for _, event := range audit.events {
		if event == "award_outcome_notified" {
			notified++
		}
	}
	if notified != 1 {
		t.Fatalf("award_outcome_notified recorded %d times, want 1", notified)
	}
}

// Retrying anything other than a FAILED attempt is refused: retrying a sent one
// would tell the Supplier twice.
func TestRetryIsRefusedForANonFailedDelivery(t *testing.T) {
	service, _, _, _ := deliveryService()
	ctx := context.Background()

	sent, err := service.SendOutcomeNotification(
		ctx, "company-1", "user-1", sendInput("op-1"))
	if err != nil {
		t.Fatalf("SendOutcomeNotification: %v", err)
	}

	if _, err := service.RetryOutcomeNotification(ctx, "company-1", "user-1",
		sent.ID, sendInput("op-2")); !errors.Is(err, ErrAwardDeliveryConflict) {
		t.Fatalf("err = %v, want ErrAwardDeliveryConflict", err)
	}
}

// An explicit retry after a failure creates a NEW record and is audited.
func TestRetryAfterFailureCreatesANewAuditedRecord(t *testing.T) {
	service, _, mailer, audit := deliveryService()
	ctx := context.Background()

	mailer.err = errors.New("transport down")
	failed, err := service.SendOutcomeNotification(
		ctx, "company-1", "user-1", sendInput("op-1"))
	if err != nil {
		t.Fatalf("SendOutcomeNotification: %v", err)
	}

	mailer.err = nil
	retried, err := service.RetryOutcomeNotification(
		ctx, "company-1", "user-1", failed.ID, sendInput("op-2"))
	if err != nil {
		t.Fatalf("RetryOutcomeNotification: %v", err)
	}
	if retried.ID == failed.ID {
		t.Fatal("a retry must create a NEW record, preserving the failed one")
	}
	if retried.Status != DeliverySent {
		t.Fatalf("retried status = %q, want sent", retried.Status)
	}

	retries := 0
	for _, event := range audit.events {
		if event == "award_outcome_notification_retried" {
			retries++
		}
	}
	if retries != 1 {
		t.Fatalf("retry audit events = %d, want 1", retries)
	}
}

// A correction obsoletes only PENDING deliveries of the superseded revision.
func TestObsoletingSupersededDeliveriesTouchesOnlyPendingRecords(t *testing.T) {
	service, deliveries, mailer, _ := deliveryService()
	ctx := context.Background()

	sent, err := service.SendOutcomeNotification(
		ctx, "company-1", "user-1", sendInput("op-sent"))
	if err != nil {
		t.Fatalf("SendOutcomeNotification: %v", err)
	}

	// A record that never resolved, exactly as a crash mid-send would leave it.
	pending := DeliveryFromNotification(AwardOutcomeNotification{
		CompanyID: "company-1", SupplierID: "supplier-a",
		RecipientIdentity: "buyer@example.test", OutcomeID: "outcome-1",
	}, "op-pending", 1)
	pending.AwardRevisionID = "revision-1"
	if _, _, err := deliveries.EnsureDeliveryIntent(ctx, pending); err != nil {
		t.Fatalf("EnsureDeliveryIntent: %v", err)
	}

	if err := service.ObsoleteSupersededDeliveries(
		ctx, "company-1", "user-1", "revision-1"); err != nil {
		t.Fatalf("ObsoleteSupersededDeliveries: %v", err)
	}

	if got := deliveries.deliveries[sent.ID].Status; got != DeliverySent {
		t.Errorf("the sent record became %q; a Supplier really was told", got)
	}
	_ = mailer
}

// An outcome that does not exist in this tenant is a bounded not-found.
func TestSendRefusesAnUnknownOutcome(t *testing.T) {
	service, _, _, _ := deliveryService()

	input := sendInput("op-1")
	input.OutcomeID = "outcome-ghost"
	if _, err := service.SendOutcomeNotification(context.Background(),
		"company-1", "user-1", input); !errors.Is(err, ErrAwardOutcomeNotFound) {
		t.Fatalf("err = %v, want ErrAwardOutcomeNotFound", err)
	}
}
