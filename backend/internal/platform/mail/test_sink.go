package mail

import "context"

// TestSinkSender wraps any EmailSender and rewrites ONLY the outbound copy
// of a message's recipient — the caller's own logical message.To (and
// every Mongo/domain record it was already derived from before calling
// Send) is never touched; only the copy actually handed to the wrapped
// sender's Send changes (M8.5C plan: "Resend test-sink changes only the
// outbound delivery address. Mongo and domain records retain the real
// logical recipient"). The real address survives as the
// X-Renovex-Intended-Recipient header and a "[TEST → real-address]"
// subject prefix, so a human reading the sink inbox can see who a message
// was actually meant for.
type TestSinkSender struct {
	inner       EmailSender
	sinkAddress string
}

// NewTestSinkSender wraps inner, redirecting every outbound copy to
// sinkAddress. Panics if sinkAddress is empty — a misconfigured test sink
// that silently fails to redirect would leak tester traffic to real
// recipients, which must never happen (this is caught earlier by config
// validation too; this panic is the last line of defense).
func NewTestSinkSender(inner EmailSender, sinkAddress string) *TestSinkSender {
	if sinkAddress == "" {
		panic("mail: TestSinkSender requires a non-empty sink address")
	}
	return &TestSinkSender{inner: inner, sinkAddress: sinkAddress}
}

const intendedRecipientHeader = "X-Renovex-Intended-Recipient"

func (s *TestSinkSender) Send(ctx context.Context, msg Message) error {
	realRecipient := msg.To

	headers := make(map[string]string, len(msg.Headers)+1)
	for k, v := range msg.Headers {
		headers[k] = v
	}
	headers[intendedRecipientHeader] = realRecipient

	redirected := Message{
		To:      s.sinkAddress,
		Subject: "[TEST → " + realRecipient + "] " + msg.Subject,
		Body:    msg.Body,
		Headers: headers,
	}
	return s.inner.Send(ctx, redirected)
}
