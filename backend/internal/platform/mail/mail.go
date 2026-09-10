// Package mail defines the EmailSender abstraction used to deliver
// transactional email (Client Quotation notifications, Supplier RFQ
// Invitations). Phase 1 uses a local SMTP implementation pointed at
// Mailpit for development; a future managed email provider can satisfy
// the same interface without changes to calling domain modules.
package mail

import "context"

// Message is a plain-text/HTML email to send. Headers carries optional
// extra outbound headers (e.g. TestSinkSender's X-Renovex-Intended-Recipient)
// — nil/empty for every existing caller, so this is a purely additive field.
type Message struct {
	To      string
	Subject string
	Body    string
	Headers map[string]string
}

// EmailSender sends a Message.
type EmailSender interface {
	Send(ctx context.Context, msg Message) error
}
