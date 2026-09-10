package mail_test

import (
	"context"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

func TestSMTPSenderConstructsWithoutError(t *testing.T) {
	sender := mail.NewSMTPSender("localhost", "1025", "no-reply@renovation-platform.local")
	if sender == nil {
		t.Fatal("expected non-nil sender")
	}
}

// TestSMTPSenderSendRequiresRecipient verifies input validation happens
// before any network call is attempted, so this test has no dependency on
// Mailpit actually running.
func TestSMTPSenderSendRequiresRecipient(t *testing.T) {
	sender := mail.NewSMTPSender("localhost", "1025", "no-reply@renovation-platform.local")

	err := sender.Send(context.Background(), mail.Message{
		To:      "",
		Subject: "Test",
		Body:    "Body",
	})
	if err == nil {
		t.Fatal("expected error for empty recipient, got nil")
	}
}
