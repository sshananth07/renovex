package mail

import (
	"context"
	"fmt"
	"net/smtp"
)

// SMTPSender sends email via a plain SMTP relay (e.g. Mailpit for local
// development; a real relay in production).
type SMTPSender struct {
	host string
	port string
	from string
}

// NewSMTPSender constructs an SMTPSender targeting host:port, sending as from.
func NewSMTPSender(host, port, from string) *SMTPSender {
	return &SMTPSender{host: host, port: port, from: from}
}

func (s *SMTPSender) Send(_ context.Context, msg Message) error {
	if msg.To == "" {
		return fmt.Errorf("mail: recipient (To) is required")
	}

	addr := fmt.Sprintf("%s:%s", s.host, s.port)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		s.from, msg.To, msg.Subject, msg.Body)

	// Mailpit accepts unauthenticated SMTP, so no auth is configured here.
	return smtp.SendMail(addr, nil, s.from, []string{msg.To}, []byte(body))
}
