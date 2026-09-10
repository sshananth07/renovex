package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/ai"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*ai.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := ai.NewClient(server.URL, "test-token", 5*time.Second)
	return client, server
}

func TestSuggestSpacesSendsCorrectPathMethodAuthAndBody(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody []byte
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = readAll(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"provider":"mock","model":"mock-v1","promptVersion":"spaces-v1","schemaVersion":1,"suggestions":[]}`))
	})

	_, err := client.SuggestSpaces(context.Background(), ai.SpaceSuggestionRequest{
		OperationID: "op_1",
		Project:     ai.ProjectContext{ID: "p1", ScopeBrief: "brief"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/internal/v1/spaces/suggest" {
		t.Fatalf("path = %q, want /internal/v1/spaces/suggest", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if !strings.Contains(string(gotBody), "op_1") {
		t.Fatalf("expected request body to contain operationId, got %s", gotBody)
	}
}

func TestSuggestSpacesDecodesResponseMetadata(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"provider":"mock","model":"mock-v1","promptVersion":"spaces-v1","schemaVersion":1,
			"suggestions":[{"name":"Kitchen","spaceType":"kitchen","rationale":"x","confidence":0.9}]
		}`))
	})

	resp, err := client.SuggestSpaces(context.Background(), ai.SpaceSuggestionRequest{
		OperationID: "op_1", Project: ai.ProjectContext{ID: "p1", ScopeBrief: "brief"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "mock" || resp.PromptVersion != "spaces-v1" {
		t.Fatalf("unexpected metadata: %+v", resp)
	}
	if len(resp.Suggestions) != 1 || resp.Suggestions[0].Name != "Kitchen" {
		t.Fatalf("unexpected suggestions: %+v", resp.Suggestions)
	}
}

func TestSuggestWorkItemsAndResourcesUseCorrectPaths(t *testing.T) {
	var paths []string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/internal/v1/work-items/suggest":
			_, _ = w.Write([]byte(`{"provider":"mock","model":"mock-v1","promptVersion":"work-items-v1","schemaVersion":1,"suggestions":[]}`))
		case "/internal/v1/resources/suggest":
			_, _ = w.Write([]byte(`{"provider":"mock","model":"mock-v1","promptVersion":"resources-v1","schemaVersion":1,"suggestions":[]}`))
		}
	})

	if _, err := client.SuggestWorkItems(context.Background(), ai.WorkItemSuggestionRequest{OperationID: "op_2"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := client.SuggestResources(context.Background(), ai.ResourceSuggestionRequest{OperationID: "op_3"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(paths) != 2 || paths[0] != "/internal/v1/work-items/suggest" || paths[1] != "/internal/v1/resources/suggest" {
		t.Fatalf("unexpected paths called: %v", paths)
	}
}

func TestClientTimeoutReturnsTypedServiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := ai.NewClient(server.URL, "test-token", 10*time.Millisecond)

	_, err := client.SuggestSpaces(context.Background(), ai.SpaceSuggestionRequest{OperationID: "op_1"})
	if err != ai.ErrProviderTimeout {
		t.Fatalf("expected ErrProviderTimeout, got %v", err)
	}
}

func TestClientMapsInternalErrorCodes(t *testing.T) {
	cases := []struct {
		httpStatus int
		body       string
		wantErr    error
	}{
		{http.StatusUnprocessableEntity, `{"code":"INVALID_AI_REQUEST","detail":"bad"}`, ai.ErrInvalidRequest},
		{http.StatusTooManyRequests, `{"code":"PROVIDER_RATE_LIMITED","detail":"bad"}`, ai.ErrProviderRateLimited},
		{http.StatusBadGateway, `{"code":"INVALID_PROVIDER_RESPONSE","detail":"bad"}`, ai.ErrInvalidProviderResponse},
		{http.StatusServiceUnavailable, `{"code":"PROVIDER_UNAVAILABLE","detail":"bad"}`, ai.ErrProviderUnavailable},
		{http.StatusServiceUnavailable, `{"code":"AI_SERVICE_UNAVAILABLE","detail":"bad"}`, ai.ErrServiceUnavailable},
	}
	for _, c := range cases {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(c.httpStatus)
			_, _ = w.Write([]byte(c.body))
		})
		_, err := client.SuggestSpaces(context.Background(), ai.SpaceSuggestionRequest{OperationID: "op_1"})
		if err != c.wantErr {
			t.Fatalf("status %d body %s: got %v, want %v", c.httpStatus, c.body, err, c.wantErr)
		}
	}
}

func TestClientNonJSONResponseBecomesInvalidServiceResponse(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})

	_, err := client.SuggestSpaces(context.Background(), ai.SpaceSuggestionRequest{OperationID: "op_1"})
	if err != ai.ErrInvalidServiceResponse {
		t.Fatalf("expected ErrInvalidServiceResponse for non-JSON body, got %v", err)
	}
}

func TestClientErrorDoesNotPropagateRawBodyOrToken(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"PROVIDER_UNAVAILABLE","detail":"raw provider secret leak test-token GEMINI_KEY_XYZ"}`))
	})

	_, err := client.SuggestSpaces(context.Background(), ai.SpaceSuggestionRequest{OperationID: "op_1"})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if strings.Contains(msg, "test-token") || strings.Contains(msg, "GEMINI_KEY_XYZ") || strings.Contains(msg, "raw provider secret") {
		t.Fatalf("error message must not propagate the raw Python response body or token, got %q", msg)
	}
}

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	var buf []byte
	dec := json.NewDecoder(r.Body)
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	buf, err := json.Marshal(raw)
	return buf, err
}
