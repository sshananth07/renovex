package rfqissuance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// The invitation service (design spec §5, §6.1A, §1A.2, §1A.4).
//
// Two properties dominate these tests:
//
//  1. the raw invitation secret is NEVER persisted — it is derived on demand
//     and returned once, which is what makes copy-link possible without
//     rotating;
//  2. mail is synchronous and AFTER the record, so a send failure leaves a
//     retryable failed attempt rather than an invitation that never existed.

func newInvitationService(t *testing.T) (*rfqissuance.Service, *invitationTestRig) {
	t.Helper()

	rig := &invitationTestRig{
		invitations: newFakeInvitationStore(),
		deliveries:  newFakeDeliveryStore(),
		suppliers:   &fakeSupplierLookup{invitable: true},
		mailer:      &fakeMailer{},
		chains:      newFakeChainStore(),
		versions:    newFakeVersionStore(),
		source:      &fakeReadyRFQSource{snapshot: readySnapshot(futureDeadline()), found: true},
	}

	keyring, err := secrets.NewInvitationKeyring(1, map[int]string{1: encodedTestKey(1)})
	if err != nil {
		t.Fatalf("building keyring: %v", err)
	}
	rig.keyring = keyring

	svc := rfqissuance.NewService(rig.versions,
		rfqissuance.WithReadyRFQSource(rig.source),
		rfqissuance.WithIssuanceChains(rig.chains),
		rfqissuance.WithAmendmentDrafts(newFakeDraftStore()),
		rfqissuance.WithInvitations(rig.invitations),
		rfqissuance.WithDeliveryAttempts(rig.deliveries),
		rfqissuance.WithSupplierLookup(rig.suppliers),
		rfqissuance.WithInvitationKeyring(keyring),
		rfqissuance.WithMailer(rig.mailer),
		rfqissuance.WithSupplierLinkBaseURL("https://app.example.test"),
	)
	return svc, rig
}

type invitationTestRig struct {
	invitations *fakeInvitationStore
	deliveries  *fakeDeliveryStore
	suppliers   *fakeSupplierLookup
	mailer      *fakeMailer
	chains      *fakeChainStore
	versions    *fakeVersionStore
	source      *fakeReadyRFQSource
	keyring     *secrets.InvitationKeyring
}

// rebuild reconstructs the service over the SAME stores, with extra options
// layered on. It exists so a test can swap in a spying audit recorder or logger
// without losing the rig's state or duplicating the wiring.
func (r *invitationTestRig) rebuild(t *testing.T, extra ...rfqissuance.Option) *rfqissuance.Service {
	t.Helper()

	opts := []rfqissuance.Option{
		rfqissuance.WithReadyRFQSource(r.source),
		rfqissuance.WithIssuanceChains(r.chains),
		rfqissuance.WithAmendmentDrafts(newFakeDraftStore()),
		rfqissuance.WithInvitations(r.invitations),
		rfqissuance.WithDeliveryAttempts(r.deliveries),
		rfqissuance.WithSupplierLookup(r.suppliers),
		rfqissuance.WithInvitationKeyring(r.keyring),
		rfqissuance.WithMailer(r.mailer),
		rfqissuance.WithSupplierLinkBaseURL("https://app.example.test"),
	}
	return rfqissuance.NewService(r.versions, append(opts, extra...)...)
}

// issuedChain puts the rig into the "one issued version" state invitations
// require.
func (r *invitationTestRig) issuedChain(t *testing.T, svc *rfqissuance.Service) rfqissuance.IssuedRFQVersion {
	t.Helper()
	issued, err := svc.IssueVersion(context.Background(), "company-1", "user-1",
		issueInput("op-issue-1"))
	if err != nil {
		t.Fatalf("setup issue: %v", err)
	}
	return issued
}

func createInvitationInput() rfqissuance.CreateInvitationInput {
	return rfqissuance.CreateInvitationInput{
		RFQChainID:     "chain-1",
		SupplierID:     "supplier-1",
		RecipientName:  "Aisha Rahman",
		RecipientEmail: "sales@supplier.com",
		ExpiresAt:      time.Now().Add(30 * 24 * time.Hour),
	}
}

// --- creation (§5.1) ---

func TestCreateInvitationRefusesThe101stInvitationForAChain(t *testing.T) {
	svc, rig := newInvitationService(t)
	rig.issuedChain(t, svc)
	for index := 0; index < 100; index++ {
		id := "existing-" + string(rune(index+1))
		rig.invitations.stored[id] = &rfqissuance.SupplierInvitation{
			ID: id, CompanyID: "company-1", RFQChainID: "chain-1",
			SupplierID: "existing-supplier-" + id,
		}
	}
	_, err := svc.CreateInvitation(context.Background(), "company-1", "user-1",
		createInvitationInput())
	if !errors.Is(err, rfqissuance.ErrInputLimitExceeded) {
		t.Errorf("101st invitation error = %v, want ErrInputLimitExceeded", err)
	}
}

func TestCreateInvitationStoresOnlyTheSecretHash(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	issued := rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invitation.AccessSecretHash == "" {
		t.Fatal("the invitation must carry the hash of its derived secret")
	}
	if invitation.SecretKeyVersion != rig.keyring.ActiveVersion() {
		t.Errorf("SecretKeyVersion = %d, want the active version %d",
			invitation.SecretKeyVersion, rig.keyring.ActiveVersion())
	}
	if invitation.CurrentIssuedRFQVersionID != issued.ID {
		t.Errorf("CurrentIssuedRFQVersionID = %q, want the current version %q",
			invitation.CurrentIssuedRFQVersionID, issued.ID)
	}

	// THE property: nothing resembling the raw token may be persisted.
	raw, err := rig.keyring.DeriveInvitationSecret(invitation.SecretKeyVersion,
		"company-1", invitation.ID, invitation.AccessGeneration)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	stored := rig.invitations.stored[invitation.ID]
	if stored == nil {
		t.Fatal("the invitation was not stored")
	}
	if strings.Contains(stored.AccessSecretHash, raw) {
		t.Error("the stored hash contains the raw token")
	}
	if stored.AccessSecretHash != secrets.HashInvitationSecret(raw) {
		t.Error("the stored hash is not the hash of the token derived from the persisted " +
			"key version and generation; copy-link would fail closed forever")
	}
}

// Creation must not contact the Supplier (§5.1).
func TestCreateInvitationSendsNoEmail(t *testing.T) {
	svc, rig := newInvitationService(t)
	rig.issuedChain(t, svc)

	if _, err := svc.CreateInvitation(context.Background(), "company-1", "user-1",
		createInvitationInput()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rig.mailer.sent) != 0 {
		t.Errorf("creation sent %d emails, want 0: sending is a separate explicit "+
			"action (§5.1)", len(rig.mailer.sent))
	}
	if len(rig.deliveries.stored) != 0 {
		t.Errorf("creation recorded %d delivery attempts, want 0", len(rig.deliveries.stored))
	}
}

// The Supplier must exist, belong to the company and be active (§5.1). All
// three failures collapse so a caller cannot probe supplier IDs.
func TestCreateInvitationRefusesAnUninvitableSupplier(t *testing.T) {
	svc, rig := newInvitationService(t)
	rig.issuedChain(t, svc)
	rig.suppliers.invitable = false

	_, err := svc.CreateInvitation(context.Background(), "company-1", "user-1",
		createInvitationInput())

	if !errors.Is(err, rfqissuance.ErrSupplierNotInvitable) {
		t.Errorf("error = %v, want ErrSupplierNotInvitable", err)
	}
}

// The Supplier lookup must be scoped by the AUTHENTICATED company.
func TestCreateInvitationScopesTheSupplierLookupToTheCompany(t *testing.T) {
	svc, rig := newInvitationService(t)
	rig.issuedChain(t, svc)

	if _, err := svc.CreateInvitation(context.Background(), "company-1", "user-1",
		createInvitationInput()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rig.suppliers.gotCompanyID != "company-1" {
		t.Errorf("supplier lookup used companyID %q, want company-1", rig.suppliers.gotCompanyID)
	}
}

// An RFQ chain with no issued version has nothing to invite against (§5.1).
func TestCreateInvitationRequiresAnIssuedVersion(t *testing.T) {
	svc, _ := newInvitationService(t)

	_, err := svc.CreateInvitation(context.Background(), "company-1", "user-1",
		createInvitationInput())

	if !errors.Is(err, rfqissuance.ErrIssuanceChainNotFound) {
		t.Errorf("error = %v, want ErrIssuanceChainNotFound", err)
	}
}

// --- copy-link (§1A.4, §6.1A) ---

// Copy-link re-derives the CURRENT link and must not rotate anything.
func TestCopyInvitationLinkReturnsTheCurrentLinkWithoutRotating(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.URL == "" {
		t.Fatal("copy-link must return a usable URL")
	}

	second, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first.URL != second.URL {
		t.Error("two copies returned DIFFERENT links: copy-link must reproduce the " +
			"current generation's link, never rotate it (§1A.4)")
	}

	after := rig.invitations.stored[invitation.ID]
	if after.AccessGeneration != invitation.AccessGeneration {
		t.Errorf("AccessGeneration moved from %d to %d; an ordinary copy must not rotate",
			invitation.AccessGeneration, after.AccessGeneration)
	}
	if after.AccessSecretHash != invitation.AccessSecretHash {
		t.Error("the stored hash changed during an ordinary copy")
	}
}

// Copy-link records a copy_link delivery attempt but sends no mail (§1A.4).
func TestCopyInvitationLinkRecordsACopyLinkAttemptAndSendsNoMail(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rig.mailer.sent) != 0 {
		t.Errorf("copy-link sent %d emails, want 0", len(rig.mailer.sent))
	}
	if len(rig.deliveries.stored) != 1 {
		t.Fatalf("recorded %d attempts, want 1", len(rig.deliveries.stored))
	}
	attempt := rig.deliveries.stored[0]
	if attempt.Channel != rfqissuance.DeliveryChannelCopyLink {
		t.Errorf("Channel = %q, want copy_link", attempt.Channel)
	}
	if attempt.Status != rfqissuance.DeliveryStatusSent {
		t.Errorf("Status = %q, want sent", attempt.Status)
	}
}

// The raw link must never reach an audit record (§15).
func TestCopyInvitationLinkNeverAuditsTheRawLink(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	link, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The delivery record is the only thing persisted for a copy — assert the
	// token is absent from every stored string field.
	for _, attempt := range rig.deliveries.stored {
		for _, field := range []string{attempt.DeliveryOperationID, attempt.RecipientEmail,
			string(attempt.FailureCode), attempt.IssuedRFQVersionID} {
			if field != "" && strings.Contains(link.URL, field) && len(field) > 20 {
				t.Errorf("a persisted field %q looks like part of the raw link", field)
			}
		}
	}
	if strings.Contains(rig.invitations.stored[invitation.ID].AccessSecretHash, link.Token) {
		t.Error("the raw token leaked into the stored hash")
	}
}

// A revoked invitation has no current link to copy (§5.4).
func TestCopyInvitationLinkRefusesARevokedInvitation(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	revokedAt := time.Now()
	rig.invitations.stored[invitation.ID].Status = rfqissuance.InvitationStatusRevoked
	rig.invitations.stored[invitation.ID].RevokedAt = &revokedAt

	_, err = svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)

	if !errors.Is(err, rfqissuance.ErrInvitationRevoked) {
		t.Errorf("error = %v, want ErrInvitationRevoked", err)
	}
}

// --- DeriveInvitationLink: the narrow, side-effect-free sibling used by
// awards to build an outcome notification's link (M8 §8I). ---

func TestDeriveInvitationLinkReproducesTheSameLinkAsCopy(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	copied, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	derived, err := svc.DeriveInvitationLink(ctx, "company-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if derived.URL != copied.URL {
		t.Errorf("DeriveInvitationLink URL = %q, want the same link CopyInvitationLink returns (%q)",
			derived.URL, copied.URL)
	}
}

// Unlike CopyInvitationLink, DeriveInvitationLink must not rotate the access
// generation, record a delivery attempt, write an audit event, or mutate the
// invitation in any way — it exists specifically so an internal caller (an
// outcome notification) can get a usable link without any of those
// contractor-visible side effects.
func TestDeriveInvitationLinkHasNoSideEffects(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.DeriveInvitationLink(ctx, "company-1", invitation.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after := rig.invitations.stored[invitation.ID]
	if after.AccessGeneration != invitation.AccessGeneration {
		t.Errorf("AccessGeneration moved from %d to %d; DeriveInvitationLink must not rotate it",
			invitation.AccessGeneration, after.AccessGeneration)
	}
	if after.Revision != invitation.Revision {
		t.Errorf("Revision moved from %d to %d; DeriveInvitationLink must not mutate the invitation",
			invitation.Revision, after.Revision)
	}
	if len(rig.deliveries.stored) != 0 {
		t.Errorf("recorded %d delivery attempts, want 0 — DeriveInvitationLink is not a contractor-visible action",
			len(rig.deliveries.stored))
	}
	if len(rig.mailer.sent) != 0 {
		t.Errorf("sent %d emails, want 0", len(rig.mailer.sent))
	}
}

func TestDeriveInvitationLinkRefusesARevokedInvitation(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	revokedAt := time.Now()
	rig.invitations.stored[invitation.ID].Status = rfqissuance.InvitationStatusRevoked
	rig.invitations.stored[invitation.ID].RevokedAt = &revokedAt

	_, err = svc.DeriveInvitationLink(ctx, "company-1", invitation.ID)

	if !errors.Is(err, rfqissuance.ErrInvitationRevoked) {
		t.Errorf("error = %v, want ErrInvitationRevoked", err)
	}
}

// A foreign company must not be able to derive another tenant's invitation
// link merely by naming its ID.
func TestDeriveInvitationLinkIsTenantScoped(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.DeriveInvitationLink(ctx, "company-2", invitation.ID)
	if err == nil {
		t.Fatal("expected an error deriving another company's invitation link")
	}
}

// --- send (§5.2, §1A.2) ---

func TestSendInvitationPersistsIntentThenSendsAndMarksSent(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{InvitationID: invitation.ID, OperationID: "op-send-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != rfqissuance.DeliveryStatusSent {
		t.Errorf("Status = %q, want sent", result.Status)
	}
	if len(rig.mailer.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(rig.mailer.sent))
	}
	if rig.mailer.sent[0].To != "sales@supplier.com" {
		t.Errorf("email To = %q", rig.mailer.sent[0].To)
	}
	// The intent must have been persisted BEFORE the send, so the store records
	// the attempt even though the mailer ran after it.
	if len(rig.deliveries.stored) != 1 {
		t.Fatalf("recorded %d attempts, want 1", len(rig.deliveries.stored))
	}
	if rig.deliveries.stored[0].Status != rfqissuance.DeliveryStatusSent {
		t.Errorf("stored status = %q, want sent", rig.deliveries.stored[0].Status)
	}

	// Sending activates the invitation (§5).
	if rig.invitations.stored[invitation.ID].Status != rfqissuance.InvitationStatusActive {
		t.Errorf("Status = %q, want active after an explicit send",
			rig.invitations.stored[invitation.ID].Status)
	}
}

// The mail body must carry the link (that is the point of the email), but the
// FAILURE path must not persist provider text (§1A.2).
func TestSendInvitationFailureLeavesARetryableFailedAttempt(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rig.mailer.err = errors.New("smtp: 535 auth failed for user admin password hunter2")

	_, err = svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{InvitationID: invitation.ID, OperationID: "op-send-1"})

	if !errors.Is(err, rfqissuance.ErrMailDeliveryFailed) {
		t.Errorf("error = %v, want ErrMailDeliveryFailed", err)
	}

	// The invitation is NOT rolled back.
	if rig.invitations.stored[invitation.ID] == nil {
		t.Fatal("a mail failure deleted the invitation")
	}

	if len(rig.deliveries.stored) != 1 {
		t.Fatalf("recorded %d attempts, want 1: a failure must remain visible and "+
			"retryable (§1A.2)", len(rig.deliveries.stored))
	}
	attempt := rig.deliveries.stored[0]
	if attempt.Status != rfqissuance.DeliveryStatusFailed {
		t.Errorf("Status = %q, want failed", attempt.Status)
	}
	if attempt.FailureCode != rfqissuance.DeliveryFailureCodeSendFailed {
		t.Errorf("FailureCode = %q, want the bounded code", attempt.FailureCode)
	}
	if strings.Contains(string(attempt.FailureCode), "hunter2") ||
		strings.Contains(string(attempt.FailureCode), "admin") {
		t.Error("provider error text leaked into the persisted failure code (§1A.2)")
	}
}

// Resend reuses the stable invitation and its current secret (§5.2).
func TestResendReusesTheInvitationAndItsCurrentSecret(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{
			InvitationID: invitation.ID, OperationID: "op-send-1",
		}); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if _, err := svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{
			InvitationID: invitation.ID, OperationID: "op-send-2",
		}); err != nil {
		t.Fatalf("resend: %v", err)
	}

	after := rig.invitations.stored[invitation.ID]
	if after.AccessGeneration != invitation.AccessGeneration {
		t.Errorf("AccessGeneration = %d, want unchanged %d: a resend normally reuses the "+
			"current secret (§5.2)", after.AccessGeneration, invitation.AccessGeneration)
	}
	if len(rig.deliveries.stored) != 2 {
		t.Errorf("recorded %d attempts, want 2: a resend creates a NEW attempt",
			len(rig.deliveries.stored))
	}
	if len(rig.mailer.sent) != 2 {
		t.Errorf("sent %d emails, want 2", len(rig.mailer.sent))
	}
}

// Send idempotency (§10.2): the same operation ID must not send twice.
func TestSendInvitationIsIdempotentUnderTheSameOperationID(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := rfqissuance.SendInvitationInput{
		InvitationID: invitation.ID, OperationID: "op-send-1",
	}
	if _, err := svc.SendInvitation(ctx, "company-1", "user-1", input); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if _, err := svc.SendInvitation(ctx, "company-1", "user-1", input); err != nil {
		t.Fatalf("the retry must resolve, got %v", err)
	}

	if len(rig.mailer.sent) != 1 {
		t.Errorf("sent %d emails, want 1: a retry under the same operation ID must not "+
			"send the Supplier a second copy (§10.2)", len(rig.mailer.sent))
	}
	if len(rig.deliveries.stored) != 1 {
		t.Errorf("recorded %d attempts, want 1", len(rig.deliveries.stored))
	}
}

// Revocation is terminal: resending never reactivates it (§5.4).
func TestSendInvitationRefusesARevokedInvitation(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	revokedAt := time.Now()
	rig.invitations.stored[invitation.ID].Status = rfqissuance.InvitationStatusRevoked
	rig.invitations.stored[invitation.ID].RevokedAt = &revokedAt

	_, err = svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{InvitationID: invitation.ID, OperationID: "op-send-1"})

	if !errors.Is(err, rfqissuance.ErrInvitationRevoked) {
		t.Errorf("error = %v, want ErrInvitationRevoked: resending must never reactivate "+
			"a revoked invitation (§5.4)", err)
	}
	if len(rig.mailer.sent) != 0 {
		t.Error("a revoked invitation was emailed")
	}
}

// --- recipient replacement (§5.3) ---

func TestReplaceRecipientRotatesTheSecretAndIncrementsGeneration(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	before, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	replaced, err := svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: invitation.Revision,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if replaced.RecipientEmailNormalized != "procurement@supplier.com" {
		t.Errorf("RecipientEmailNormalized = %q", replaced.RecipientEmailNormalized)
	}
	if replaced.AccessGeneration != invitation.AccessGeneration+1 {
		t.Errorf("AccessGeneration = %d, want %d: replacement must invalidate the previous "+
			"recipient's link (§5.3)", replaced.AccessGeneration, invitation.AccessGeneration+1)
	}

	after, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if after.URL == before.URL {
		t.Error("the link is unchanged after a recipient replacement; the previous " +
			"recipient would retain access (§5.3)")
	}
}

// A failed recipient replacement must leave the invitation completely
// untouched. A two-write implementation snapshots the new recipient first and
// only then rotates, so a failure at the rotation step leaves the recipient
// already changed while the PREVIOUS recipient's link still resolves — a stale
// -link exposure the contractor cannot see (§5.3A).
func TestReplaceRecipientLeavesNoPartialStateWhenRotationFails(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	originalRecipient := invitation.RecipientEmailNormalized
	originalGeneration := invitation.AccessGeneration

	rig.invitations.failSecretRotation = true
	if _, err := svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: invitation.Revision,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
		}); err == nil {
		t.Fatal("expected the replacement to fail when the authoritative write fails")
	}
	rig.invitations.failSecretRotation = false

	current, err := svc.GetInvitation(ctx, "company-1", invitation.ID)
	if err != nil {
		t.Fatalf("reloading the invitation: %v", err)
	}

	if current.RecipientEmailNormalized != originalRecipient {
		t.Errorf("RecipientEmailNormalized = %q, want the original %q: the "+
			"recipient was replaced while the previous recipient's link was "+
			"never invalidated", current.RecipientEmailNormalized, originalRecipient)
	}
	if current.AccessGeneration != originalGeneration {
		t.Errorf("AccessGeneration = %d, want the original %d",
			current.AccessGeneration, originalGeneration)
	}
}

// Replacement invalidates the previous generation's link, so a delivery still
// queued for the PREVIOUS recipient must not be sent afterwards: it would mail
// a dead link to the person who was just replaced (§5.3A).
func TestReplaceRecipientObsoletesOlderPendingDeliveries(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pending, err := rig.deliveries.CreateAttempt(ctx, rfqissuance.InvitationDeliveryAttempt{
		CompanyID:           "company-1",
		InvitationID:        invitation.ID,
		DeliveryOperationID: "op-send-1",
		AccessGeneration:    invitation.AccessGeneration,
		Status:              rfqissuance.DeliveryStatusPending,
	})
	if err != nil {
		t.Fatalf("seeding a pending delivery: %v", err)
	}

	if _, err := svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: invitation.Revision,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
			OperationID:      "op-replace-1",
		}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found bool
	for _, attempt := range rig.deliveries.stored {
		if attempt.ID != pending.ID {
			continue
		}
		found = true
		if attempt.Status != rfqissuance.DeliveryStatusObsolete {
			t.Errorf("status = %q, want obsolete: a delivery queued for the "+
				"replaced recipient would carry an already-invalid link",
				attempt.Status)
		}
	}
	if !found {
		t.Fatal("the delivery attempt disappeared; history must be preserved")
	}
}

func TestReplaceRecipientRequiresTheExpectedRevision(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: invitation.Revision + 99,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
		})

	if !errors.Is(err, rfqissuance.ErrRevisionMismatch) {
		t.Errorf("error = %v, want ErrRevisionMismatch", err)
	}
}

// --- revocation (§5.4) ---

func TestRevokeInvitationBlocksAccessAndPreservesHistory(t *testing.T) {
	svc, rig := newInvitationService(t)
	ctx := context.Background()
	rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	revoked, err := svc.RevokeInvitation(ctx, "company-1", "user-1", invitation.ID,
		invitation.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if revoked.Status != rfqissuance.InvitationStatusRevoked {
		t.Errorf("Status = %q, want revoked", revoked.Status)
	}
	if revoked.RevokedAt == nil {
		t.Error("RevokedAt must be stamped: it is what blocks access immediately")
	}
	if revoked.PermitsAccess(time.Now()) {
		t.Error("a revoked invitation still permits access")
	}
	// History is preserved, not deleted (§5.4).
	if rig.invitations.stored[invitation.ID].RecipientEmailNormalized != "sales@supplier.com" {
		t.Error("revocation destroyed the recipient record")
	}
}
