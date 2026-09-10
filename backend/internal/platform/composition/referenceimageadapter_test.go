package composition

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func testReferenceImageRequest() spatial.ReferenceImageGenerationRequest {
	return spatial.ReferenceImageGenerationRequest{
		DesignSessionID: "ds_1", TurnID: "turn_1", PlanFingerprint: "sha256:abc123",
		Target: spatial.ReferenceImageTarget{
			Kind: spatial.DesignTargetKindObject, ID: "object_sofa_123", Category: "sofa",
			Dimensions: &spatial.RoomLocalPoint{X: 2.1, Y: 0.85, Z: 0.9},
		},
		Geometry:      spatial.WorkingDesignGeometry{Category: "sofa", ShapeDescription: "Curved three-seat sofa", PreserveCanonicalDimensions: true},
		Appearance:    &spatial.VisualAppearance{BaseColor: "#315c45", MaterialFamily: spatial.MaterialFamilyFabric, Roughness: spatial.RoughnessMatte, Metallic: false},
		PromptVersion: "reference-v1", Seed: 4815162342,
	}
}

func TestReferenceImageAdapter_TranslatesRequestAndDecodesImage(t *testing.T) {
	var gotBody platformai.ReferenceImageRequest
	imageBytes := []byte("fake-jpeg-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		resp := platformai.ReferenceImageResponse{
			SchemaVersion: "1", ImageBase64: base64.StdEncoding.EncodeToString(imageBytes), ContentType: "image/jpeg",
			Width: 1024, Height: 1024, Provider: "cloudflare_flux", Model: "@cf/black-forest-labs/flux-1-schnell",
			ProviderRequestID: "req_1", Seed: 4815162342, PromptVersion: "reference-v1",
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer server.Close()

	client := platformai.NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	adapter := NewReferenceImageAdapter(client)

	result, err := adapter.GenerateReference(context.Background(), testReferenceImageRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotBody.DesignSessionID != "ds_1" || gotBody.TurnID != "turn_1" || gotBody.PlanFingerprint != "sha256:abc123" {
		t.Fatalf("expected session/turn/fingerprint forwarded, got %+v", gotBody)
	}
	if gotBody.Target.Kind != "object" || gotBody.Target.ID != "object_sofa_123" {
		t.Fatalf("expected target forwarded, got %+v", gotBody.Target)
	}
	if gotBody.MaterialAppearance == nil || gotBody.MaterialAppearance.BaseColor != "#315c45" {
		t.Fatalf("expected appearance forwarded, got %+v", gotBody.MaterialAppearance)
	}
	if gotBody.RenderBrief.View != "three_quarter_front" || !gotBody.RenderBrief.Isolated {
		t.Fatalf("expected fixed render brief composition, got %+v", gotBody.RenderBrief)
	}
	if gotBody.Seed != 4815162342 {
		t.Fatalf("expected seed forwarded, got %d", gotBody.Seed)
	}

	if string(result.ImageBytes) != string(imageBytes) {
		t.Fatalf("expected decoded image bytes to match, got %q", result.ImageBytes)
	}
	if result.ContentType != "image/jpeg" {
		t.Fatalf("expected content type forwarded, got %s", result.ContentType)
	}
	if result.Provider != "cloudflare_flux" {
		t.Fatalf("expected provider forwarded, got %s", result.Provider)
	}
}

func TestReferenceImageAdapter_MapsProviderErrorsToDesignReasoningSentinels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"code":"PROVIDER_TIMEOUT","detail":"timed out"}`))
	}))
	defer server.Close()

	client := platformai.NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	adapter := NewReferenceImageAdapter(client)

	_, err := adapter.GenerateReference(context.Background(), testReferenceImageRequest())
	if err != spatial.ErrDesignReasoningTimeout {
		t.Fatalf("expected spatial.ErrDesignReasoningTimeout, got %v", err)
	}
}

func TestReferenceImageAdapter_OmitsAppearanceWhenNil(t *testing.T) {
	var gotBody platformai.ReferenceImageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		resp := platformai.ReferenceImageResponse{
			SchemaVersion: "1", ImageBase64: base64.StdEncoding.EncodeToString([]byte("x")), ContentType: "image/jpeg",
			Width: 1, Height: 1, Provider: "mock", Model: "m", Seed: 1, PromptVersion: "v1",
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer server.Close()

	client := platformai.NewReferenceImageClient(server.URL, "test-token", 8*1024*1024, 5*time.Second)
	adapter := NewReferenceImageAdapter(client)

	req := testReferenceImageRequest()
	req.Appearance = nil
	if _, err := adapter.GenerateReference(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody.MaterialAppearance != nil {
		t.Fatalf("expected nil appearance to omit materialAppearance, got %+v", gotBody.MaterialAppearance)
	}
}
