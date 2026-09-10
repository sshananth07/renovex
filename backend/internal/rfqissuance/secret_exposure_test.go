package rfqissuance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/logging"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Secret non-exposure across EVERY sink (design spec §6.1A, §15).
//
// The audit interface's signature already makes a secret unpassable, but a
// signature cannot prove that application logging, delivery records or any
// other persisted string is clean. These tests take the raw token actually
// returned to the contractor and search every sink for it.
//
// Why the token is searched rather than a fixed fixture: an assertion against a
// hardcoded string would silently stop testing anything the moment derivation
// changed.

// secretSinks captures everything the module writes anywhere except its own
// return value.
type secretSinks struct {
	logs  *bytes.Buffer
	audit *recordingIssuanceAudit
	rig   *invitationTestRig
}

// assertAbsent fails if any needle appears in any sink. Both the raw token and
// the full URL are checked: a URL-encoded token in a log line is exactly as
// dangerous as the bare token.
func (s secretSinks) assertAbsent(t *testing.T, needles map[string]string) {
	t.Helper()

	haystacks := map[string]string{
		"application logs": s.logs.String(),
		"audit records":    s.auditJSON(t),
		"persisted state":  s.persistedJSON(t),
	}

	for sinkName, haystack := range haystacks {
		for needleName, needle := range needles {
			if needle == "" {
				t.Fatalf("the %s needle is empty; the test would pass vacuously", needleName)
			}
			if strings.Contains(haystack, needle) {
				t.Errorf("the %s appeared in %s. A secret must exist only in the response "+
					"and the outbound mail, never in a log line, an audit record or the "+
					"database (§6.1A, §15)", needleName, sinkName)
			}
		}
	}
}

// auditJSON renders every recorded audit call so the search covers all fields,
// not just the ones a test remembered to name.
func (s secretSinks) auditJSON(t *testing.T) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"created":           s.audit.invitationCreated,
		"sent":              s.audit.invitationSent,
		"deliveryFailed":    s.audit.invitationDeliveryFailed,
		"linkCopied":        s.audit.invitationLinkCopied,
		"recipientReplaced": s.audit.invitationRecipientReplaced,
		"secretRotated":     s.audit.invitationSecretRotated,
		"revoked":           s.audit.invitationRevoked,
		"reactivated":       s.audit.invitationReactivated,
		"expiryChanged":     s.audit.invitationExpiryChanged,
		"advanced":          s.audit.invitationAdvanced,
	})
	if err != nil {
		t.Fatalf("encoding audit calls: %v", err)
	}
	// The audit call struct has unexported fields, so JSON alone would be
	// empty. Append a formatted rendering so nothing escapes the search.
	return string(encoded) + renderAuditCalls(s.audit)
}

// persistedJSON renders every stored invitation and delivery attempt.
func (s secretSinks) persistedJSON(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	for _, invitation := range s.rig.invitations.stored {
		b.WriteString(invitation.ID)
		b.WriteString(invitation.AccessSecretHash)
		b.WriteString(invitation.RecipientEmail)
		b.WriteString(invitation.RecipientEmailNormalized)
		b.WriteString(invitation.RecipientName)
		b.WriteString(string(invitation.Status))
	}
	for _, attempt := range s.rig.deliveries.stored {
		b.WriteString(attempt.ID)
		b.WriteString(attempt.DeliveryOperationID)
		b.WriteString(attempt.RecipientEmail)
		b.WriteString(attempt.IssuedRFQVersionID)
		b.WriteString(string(attempt.Status))
		b.WriteString(string(attempt.FailureCode))
		b.WriteString(string(attempt.Channel))
	}
	return b.String()
}

func renderAuditCalls(a *recordingIssuanceAudit) string {
	var b strings.Builder
	groups := [][]invitationAuditCall{
		a.invitationCreated, a.invitationSent, a.invitationDeliveryFailed,
		a.invitationLinkCopied, a.invitationRecipientReplaced,
		a.invitationSecretRotated, a.invitationRevoked,
		a.invitationReactivated,
		a.invitationExpiryChanged, a.invitationAdvanced,
	}
	for _, group := range groups {
		for _, call := range group {
			b.WriteString(call.companyID)
			b.WriteString(call.actorUserID)
			b.WriteString(call.rfqChainID)
			b.WriteString(call.invitationID)
			b.WriteString(call.supplierID)
			b.WriteString(call.attemptID)
			b.WriteString(call.channel)
			b.WriteString(call.failureCode)
		}
	}
	return b.String()
}

// newSpiedInvitationService wires the service with a capturing logger and a
// recording audit recorder.
func newSpiedInvitationService(t *testing.T) (*rfqissuance.Service, secretSinks) {
	t.Helper()

	svc, rig := newInvitationService(t)

	logs := &bytes.Buffer{}
	audit := &recordingIssuanceAudit{}
	logger := logging.New(logs, "debug")

	svc = rig.rebuild(t,
		rfqissuance.WithAuditRecorder(audit),
		rfqissuance.WithLogger(&logger),
	)

	return svc, secretSinks{logs: logs, audit: audit, rig: rig}
}

// Creation returns a link once. It must appear in NO sink.
func TestCreationNeverExposesTheRawSecret(t *testing.T) {
	svc, sinks := newSpiedInvitationService(t)
	ctx := context.Background()
	sinks.rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Recover the token the contractor would receive.
	link, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sinks.assertAbsent(t, map[string]string{
		"raw token":         link.Token,
		"full URL":          link.URL,
		"URL-encoded token": url.QueryEscape(link.Token),
	})
}

// Copy-link is the operation most likely to leak: it exists to hand the raw
// link to a human.
func TestCopyLinkNeverExposesTheRawSecret(t *testing.T) {
	svc, sinks := newSpiedInvitationService(t)
	ctx := context.Background()
	sinks.rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	link, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sinks.assertAbsent(t, map[string]string{
		"raw token":         link.Token,
		"full URL":          link.URL,
		"URL-encoded token": url.QueryEscape(link.Token),
	})
}

// A send puts the link in the outbound MAIL, which is legitimate. It must still
// reach no other sink.
func TestSendNeverExposesTheRawSecretOutsideTheMail(t *testing.T) {
	svc, sinks := newSpiedInvitationService(t)
	ctx := context.Background()
	sinks.rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{
			InvitationID: invitation.ID, OperationID: "op-send-1",
		}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	link, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The mail body SHOULD carry it — that is the point of sending.
	if len(sinks.rig.mailer.sent) == 0 {
		t.Fatal("no mail was sent")
	}
	if !strings.Contains(sinks.rig.mailer.sent[0].Body, link.Token) {
		t.Error("the invitation email does not contain the link; the Supplier cannot open it")
	}

	sinks.assertAbsent(t, map[string]string{
		"raw token":         link.Token,
		"full URL":          link.URL,
		"URL-encoded token": url.QueryEscape(link.Token),
	})
}

// A FAILED send is the highest-risk path: an error handler is exactly where a
// developer reaches for "log everything we know".
func TestFailedSendNeverExposesTheRawSecretOrProviderText(t *testing.T) {
	svc, sinks := newSpiedInvitationService(t)
	ctx := context.Background()
	sinks.rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	link, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sinks.rig.mailer.err = &stubError{"smtp: 535 auth failed for user admin password hunter2"}

	if _, err := svc.SendInvitation(ctx, "company-1", "user-1",
		rfqissuance.SendInvitationInput{
			InvitationID: invitation.ID, OperationID: "op-send-1",
		}); err == nil {
		t.Fatal("expected the send to fail")
	}

	sinks.assertAbsent(t, map[string]string{
		"raw token":           link.Token,
		"full URL":            link.URL,
		"URL-encoded token":   url.QueryEscape(link.Token),
		"provider credential": "hunter2",
		"provider account":    "admin password",
	})
}

// Rotation and recipient replacement both mint a new link.
func TestRotationAndReplacementNeverExposeTheRawSecret(t *testing.T) {
	svc, sinks := newSpiedInvitationService(t)
	ctx := context.Background()
	sinks.rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1", createInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rotated, err := svc.RotateInvitationSecret(ctx, "company-1", "user-1", invitation.ID,
		invitation.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	afterRotation, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	replaced, err := svc.ReplaceRecipient(ctx, "company-1", "user-1",
		rfqissuance.ReplaceRecipientInput{
			InvitationID:     invitation.ID,
			ExpectedRevision: rotated.Revision,
			RecipientName:    "Bala Krishnan",
			RecipientEmail:   "procurement@supplier.com",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	afterReplacement, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", replaced.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sinks.assertAbsent(t, map[string]string{
		"post-rotation token":    afterRotation.Token,
		"post-rotation URL":      afterRotation.URL,
		"post-replacement token": afterReplacement.Token,
		"post-replacement URL":   afterReplacement.URL,
	})
}

// Reactivation mints a fresh generation under the active key. It expands the
// secret-bearing surface and therefore belongs in the same sink spy as create,
// copy, send, rotation, and replacement.
func TestReactivationNeverExposesTheRawSecret(t *testing.T) {
	svc, sinks := newSpiedInvitationService(t)
	ctx := context.Background()
	sinks.rig.issuedChain(t, svc)

	invitation, err := svc.CreateInvitation(ctx, "company-1", "user-1",
		createInvitationInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	revoked, err := svc.RevokeInvitation(ctx, "company-1", "user-1",
		invitation.ID, invitation.Revision)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.ReactivateInvitation(ctx, "company-1", "user-1",
		rfqissuance.ReactivateInvitationInput{
			InvitationID: invitation.ID, ExpectedRevision: revoked.Revision,
			ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
			OperationID: "op-reactivate-secret-spy",
		}); err != nil {
		t.Fatalf("reactivate: %v", err)
	}

	link, err := svc.CopyInvitationLink(ctx, "company-1", "user-1", invitation.ID)
	if err != nil {
		t.Fatalf("copy reactivated link: %v", err)
	}
	sinks.assertAbsent(t, map[string]string{
		"reactivated raw token":         link.Token,
		"reactivated full URL":          link.URL,
		"reactivated URL-encoded token": url.QueryEscape(link.Token),
	})
}
