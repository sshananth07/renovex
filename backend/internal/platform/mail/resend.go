package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const resendDefaultBaseURL = "https://api.resend.com"

// ResendSender sends email via Resend's HTTP API — the tester deployment's
// EMAIL_PROVIDER=resend implementation (M8.5C plan). Implements the same
// EmailSender interface every existing company/access-grant/RFQ/supplier-
// verification/award caller already depends on, so no calling code changes.
type ResendSender struct {
	apiKey     string
	from       string
	baseURL    string
	httpClient *http.Client
}

// NewResendSender constructs a ResendSender using Resend's real API. apiKey
// is the RESEND_API_KEY secret; from is the RESEND_FROM address (e.g.
// "Renovex <onboarding@resend.dev>").
func NewResendSender(apiKey, from string) *ResendSender {
	return &ResendSender{apiKey: apiKey, from: from, baseURL: resendDefaultBaseURL, httpClient: http.DefaultClient}
}

// NewResendSenderForTest constructs a ResendSender against an injectable
// baseURL/httpClient — test-only, matching
// ComputeOperationFingerprintForTest's "exported test-only helper"
// convention so external (mail_test) test files can inject a
// httptest.Server URL without resend.com ever being contacted.
func NewResendSenderForTest(apiKey, from, baseURL string, httpClient *http.Client) *ResendSender {
	return &ResendSender{apiKey: apiKey, from: from, baseURL: baseURL, httpClient: httpClient}
}

type resendSendRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	HTML    string            `json:"html,omitempty"`
	Text    string            `json:"text,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (s *ResendSender) Send(ctx context.Context, msg Message) error {
	if msg.To == "" {
		return fmt.Errorf("mail: recipient (To) is required")
	}

	payload, err := json.Marshal(resendSendRequest{
		From: s.from, To: []string{msg.To}, Subject: msg.Subject, Text: msg.Body, Headers: msg.Headers,
	})
	if err != nil {
		return fmt.Errorf("mail: encoding resend request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/emails", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("mail: building resend request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mail: resend request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("mail: resend returned status %d: %s", resp.StatusCode, body)
	}
	return nil
}
