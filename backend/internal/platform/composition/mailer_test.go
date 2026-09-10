package composition

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

func TestSelectMailer_DefaultsToSMTP(t *testing.T) {
	m := selectMailer(config.Config{SMTPHost: "localhost", SMTPPort: "1025", SMTPFrom: "no-reply@test.local"})
	if _, ok := m.(*mail.SMTPSender); !ok {
		t.Fatalf("expected *mail.SMTPSender, got %T", m)
	}
}

func TestSelectMailer_ResendProviderSelectsResendSender(t *testing.T) {
	m := selectMailer(config.Config{EmailProvider: "resend", ResendAPIKey: "key", ResendFrom: "Renovex <no-reply@renovex.example>"})
	if _, ok := m.(*mail.ResendSender); !ok {
		t.Fatalf("expected *mail.ResendSender, got %T", m)
	}
}

func TestSelectMailer_TestSinkWrapsWhicheverProviderIsSelected(t *testing.T) {
	m := selectMailer(config.Config{
		EmailProvider: "resend", ResendAPIKey: "key", ResendFrom: "from@example.com",
		EmailDeliveryMode: "test_sink", EmailTestSinkAddress: "sink@example.com",
	})
	if _, ok := m.(*mail.TestSinkSender); !ok {
		t.Fatalf("expected *mail.TestSinkSender wrapping the selected provider, got %T", m)
	}
}

func TestSelectMailer_DirectModeNeverWraps(t *testing.T) {
	m := selectMailer(config.Config{EmailProvider: "smtp", EmailDeliveryMode: "direct", SMTPHost: "localhost", SMTPPort: "1025"})
	if _, ok := m.(*mail.TestSinkSender); ok {
		t.Fatal("expected no TestSinkSender wrapping in direct mode")
	}
}
