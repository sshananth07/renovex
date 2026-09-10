package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func TestSpatialGenerationWakeClient_SendsKindAndIDWithToken(t *testing.T) {
	var gotBody spatialGenerationWakeRequestBody
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Spatial-Queue-Enqueue-Token")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewSpatialGenerationWakeClient(server.URL, "the-enqueue-token", 5*time.Second)
	if err := client.PublishGenerationWake(context.Background(), spatial.GenerationWakeKindDesignAttempt, "attempt_123"); err != nil {
		t.Fatalf("PublishGenerationWake: %v", err)
	}

	if gotToken != "the-enqueue-token" {
		t.Fatalf("expected token header forwarded, got %q", gotToken)
	}
	if gotBody.Kind != "design_attempt" || gotBody.ID != "attempt_123" {
		t.Fatalf("expected {design_attempt, attempt_123}, got %+v", gotBody)
	}
}

func TestSpatialGenerationWakeClient_NonSuccessStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewSpatialGenerationWakeClient(server.URL, "wrong-token", 5*time.Second)
	if err := client.PublishGenerationWake(context.Background(), spatial.GenerationWakeKindAssetJob, "job_1"); err == nil {
		t.Fatal("expected an error for a non-2xx response")
	}
}
