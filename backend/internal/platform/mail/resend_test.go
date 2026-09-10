package mail_test

// resend_test.go tests use net/http/httptest against ResendSender's real
// HTTP call — no external network access, following ai/spatial_client_test.go's
// exact httptest.Server convention.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

func TestResendSender_SendsExpectedRequest(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"email_123"}`))
	}))
	defer server.Close()

	sender := mail.NewResendSenderForTest("test-api-key", "Renovex <onboarding@resend.dev>", server.URL, server.Client())
	err := sender.Send(context.Background(), mail.Message{To: "client@example.com", Subject: "Test", Body: "Body text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer test-api-key" {
		t.Fatalf("expected bearer auth header, got %q", gotAuth)
	}
	if gotPath != "/emails" {
		t.Fatalf("expected /emails path, got %q", gotPath)
	}
	toList, _ := gotBody["to"].([]any)
	if len(toList) != 1 || toList[0] != "client@example.com" {
		t.Fatalf("expected to=[client@example.com], got %+v", gotBody["to"])
	}
}

func TestResendSender_RequiresRecipient(t *testing.T) {
	sender := mail.NewResendSenderForTest("test-api-key", "from@example.com", "http://unused", http.DefaultClient)
	err := sender.Send(context.Background(), mail.Message{To: "", Subject: "Test", Body: "Body"})
	if err == nil {
		t.Fatal("expected error for empty recipient, got nil")
	}
}

func TestResendSender_NonSuccessStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"invalid API key"}`))
	}))
	defer server.Close()

	sender := mail.NewResendSenderForTest("bad-key", "from@example.com", server.URL, server.Client())
	err := sender.Send(context.Background(), mail.Message{To: "client@example.com", Subject: "Test", Body: "Body"})
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
}

func TestResendSender_ForwardsHeaders(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"email_1"}`))
	}))
	defer server.Close()

	sender := mail.NewResendSenderForTest("k", "from@example.com", server.URL, server.Client())
	err := sender.Send(context.Background(), mail.Message{
		To: "client@example.com", Subject: "Test", Body: "Body",
		Headers: map[string]string{"X-Renovex-Intended-Recipient": "real@example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	headers, _ := gotBody["headers"].(map[string]any)
	if headers["X-Renovex-Intended-Recipient"] != "real@example.com" {
		t.Fatalf("expected header forwarded, got %+v", headers)
	}
}
