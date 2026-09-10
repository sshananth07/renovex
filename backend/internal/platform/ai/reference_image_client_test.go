package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func validReferenceImageRequest() ReferenceImageRequest {
	return ReferenceImageRequest{
		SchemaVersion:   "1",
		DesignSessionID: "ds_1",
		TurnID:          "turn_1",
		PlanFingerprint: "sha256:abc123",
		Target: ReferenceTarget{
			Kind: "object", ID: "object_sofa_123", Category: "sofa",
			DimensionsMeters: &ReferenceTargetDimensions{Width: 2.1, Height: 0.85, Depth: 0.9},
		},
		AssetGenerationSpec: ReferenceAssetGenerationSpec{
			Category: "sofa", ShapeDescription: "Curved three-seat sofa with rounded back", PreserveCanonicalDimensions: true,
		},
		MaterialAppearance: &ReferenceMaterialAppearance{BaseColor: "#315c45", MaterialFamily: "fabric", Roughness: "matte", Metallic: false},
		RenderBrief: ReferenceRenderBrief{
			View: "three_quarter_front", Isolated: true, FullObjectVisible: true, Background: "plain_warm_white",
			NoText: true, NoPeople: true, NoRoom: true,
		},
		PromptVersion: "reference-v1",
		Seed:          4815162342,
	}
}

func validReferenceImageResponseBody() []byte {
	body, _ := json.Marshal(ReferenceImageResponse{
		SchemaVersion: "1", ImageBase64: "abc123==", ContentType: "image/jpeg",
		Width: 1024, Height: 1024, Provider: "mock", Model: "reference-mock-v1",
		ProviderRequestID: "req_1", Seed: 4815162342, PromptVersion: "reference-v1",
	})
	return body
}

func TestReferenceImageClient_ExactPathAuthBody(t *testing.T) {
	var gotPath, gotAuth, gotMethod string
	var gotBody ReferenceImageRequest
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(validReferenceImageResponseBody())
	}))
	defer server.Close()

	client := NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	resp, err := client.GenerateReference(context.Background(), validReferenceImageRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected exactly one call, got %d", callCount)
	}
	if gotPath != "/internal/v1/spatial/reference-images/generate" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("unexpected auth header: %s", gotAuth)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("unexpected method: %s", gotMethod)
	}
	if gotBody.Seed != 4815162342 {
		t.Fatalf("expected seed forwarded, got %d", gotBody.Seed)
	}
	if resp.Provider != "mock" {
		t.Fatalf("expected provider forwarded, got %s", resp.Provider)
	}
}

func TestReferenceImageClient_ErrorCodeMapsToSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"code":"PROVIDER_TIMEOUT","detail":"reference image provider request timed out"}`))
	}))
	defer server.Close()

	client := NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	_, err := client.GenerateReference(context.Background(), validReferenceImageRequest())
	if !errors.Is(err, ErrProviderTimeout) {
		t.Fatalf("expected ErrProviderTimeout, got %v", err)
	}
}

func TestReferenceImageClient_UnknownErrorCodeMapsToServiceUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"code":"SOME_FUTURE_CODE","detail":"unknown"}`))
	}))
	defer server.Close()

	client := NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	_, err := client.GenerateReference(context.Background(), validReferenceImageRequest())
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("expected ErrServiceUnavailable, got %v", err)
	}
}

func TestReferenceImageClient_RedirectTreatedAsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://evil.example/")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	client := NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	_, err := client.GenerateReference(context.Background(), validReferenceImageRequest())
	if !errors.Is(err, ErrInvalidServiceResponse) {
		t.Fatalf("expected ErrInvalidServiceResponse, got %v", err)
	}
}

func TestReferenceImageClient_UnknownResponseFieldRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"schemaVersion":"1","imageBase64":"a","contentType":"image/jpeg","width":1,"height":1,"provider":"mock","model":"m","seed":1,"promptVersion":"v1","unexpectedField":"nope"}`))
	}))
	defer server.Close()

	client := NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	_, err := client.GenerateReference(context.Background(), validReferenceImageRequest())
	if !errors.Is(err, ErrInvalidServiceResponse) {
		t.Fatalf("expected ErrInvalidServiceResponse for unknown field, got %v", err)
	}
}

func TestReferenceImageClient_OversizedResponseRejected(t *testing.T) {
	// maxImageBytes=100 derives a bound of (100*4)/3 + 64KiB overhead — a
	// response body well past that (1MiB of base64 payload) must be
	// rejected, proving the bound is actually enforced rather than
	// effectively unbounded.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"schemaVersion":"1","imageBase64":"` + strings.Repeat("A", 1<<20) + `","contentType":"image/jpeg","width":1,"height":1,"provider":"mock","model":"m","seed":1,"promptVersion":"v1"}`))
	}))
	defer server.Close()

	client := NewReferenceImageClient(server.URL, "test-token", 100, 5*time.Second)
	_, err := client.GenerateReference(context.Background(), validReferenceImageRequest())
	if !errors.Is(err, ErrInvalidServiceResponse) {
		t.Fatalf("expected ErrInvalidServiceResponse for oversized response, got %v", err)
	}
}

func TestReferenceImageClient_TimeoutMapsToProviderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Millisecond)
	_, err := client.GenerateReference(context.Background(), validReferenceImageRequest())
	if !errors.Is(err, ErrProviderTimeout) {
		t.Fatalf("expected ErrProviderTimeout, got %v", err)
	}
}
