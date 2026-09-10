package mail_test

import (
	"context"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

// fakeSender records every message it was actually asked to send —
// TestSinkSender's caller (test) inspects what the WRAPPED sender received,
// never what the original caller passed to TestSinkSender.Send.
type fakeSender struct {
	sent []mail.Message
}

func (f *fakeSender) Send(_ context.Context, msg mail.Message) error {
	f.sent = append(f.sent, msg)
	return nil
}

func TestTestSinkSender_RedirectsOutboundToOnly(t *testing.T) {
	inner := &fakeSender{}
	sink := mail.NewTestSinkSender(inner, "tester-inbox@resend.dev")

	err := sink.Send(context.Background(), mail.Message{To: "real-client@example.com", Subject: "Your quotation", Body: "..."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inner.sent) != 1 {
		t.Fatalf("expected exactly one send, got %d", len(inner.sent))
	}
	if inner.sent[0].To != "tester-inbox@resend.dev" {
		t.Fatalf("expected outbound To redirected to sink, got %q", inner.sent[0].To)
	}
}

func TestTestSinkSender_PreservesIntendedRecipientHeader(t *testing.T) {
	inner := &fakeSender{}
	sink := mail.NewTestSinkSender(inner, "tester-inbox@resend.dev")

	if err := sink.Send(context.Background(), mail.Message{To: "real-client@example.com", Subject: "Test", Body: "..."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := inner.sent[0].Headers["X-Renovex-Intended-Recipient"]
	if got != "real-client@example.com" {
		t.Fatalf("expected intended-recipient header preserved, got %q", got)
	}
}

func TestTestSinkSender_PrefixesSubjectWithRealAddress(t *testing.T) {
	inner := &fakeSender{}
	sink := mail.NewTestSinkSender(inner, "tester-inbox@resend.dev")

	if err := sink.Send(context.Background(), mail.Message{To: "real-client@example.com", Subject: "Your quotation", Body: "..."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	subject := inner.sent[0].Subject
	if !strings.Contains(subject, "real-client@example.com") || !strings.Contains(subject, "Your quotation") {
		t.Fatalf("expected subject to carry real address and original subject, got %q", subject)
	}
}

func TestTestSinkSender_PreservesOtherHeaders(t *testing.T) {
	inner := &fakeSender{}
	sink := mail.NewTestSinkSender(inner, "tester-inbox@resend.dev")

	if err := sink.Send(context.Background(), mail.Message{
		To: "real-client@example.com", Subject: "Test", Body: "...",
		Headers: map[string]string{"X-Custom": "value"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inner.sent[0].Headers["X-Custom"] != "value" {
		t.Fatalf("expected pre-existing header preserved, got %+v", inner.sent[0].Headers)
	}
}

func TestTestSinkSender_DoesNotMutateCallersOriginalMessage(t *testing.T) {
	inner := &fakeSender{}
	sink := mail.NewTestSinkSender(inner, "tester-inbox@resend.dev")

	original := mail.Message{To: "real-client@example.com", Subject: "Test", Body: "..."}
	originalTo := original.To
	if err := sink.Send(context.Background(), original); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if original.To != originalTo {
		t.Fatalf("expected caller's own Message struct never mutated, got To=%q", original.To)
	}
}

func TestNewTestSinkSender_PanicsOnEmptySinkAddress(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty sink address")
		}
	}()
	mail.NewTestSinkSender(&fakeSender{}, "")
}
