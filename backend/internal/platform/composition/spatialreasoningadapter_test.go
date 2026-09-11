package composition

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func testReasoningContext() spatial.DesignReasoningContext {
	return spatial.DesignReasoningContext{
		TurnID: "turn_server_123", RoomDraftID: "roomdraft_server_456", RoomDraftRevision: 17,
		Target: spatial.AuthorizedDesignTarget{
			Kind: spatial.DesignTargetKindObject, ID: "object_sofa_123", Category: "sofa",
			Transform:  spatial.RoomLocalTransform{Position: spatial.RoomLocalPoint{X: 1.2, Y: 0, Z: 2.4}, Rotation: spatial.RoomLocalQuaternion{W: 1}},
			Dimensions: &spatial.RoomLocalPoint{X: 2.0, Y: 0.85, Z: 0.95},
		},
		Walls:                []spatial.DesignContextWall{{ID: "wall_1", Start: spatial.RoomLocalPoint{}, End: spatial.RoomLocalPoint{X: 4}}},
		Neighbors:            []spatial.DesignContextNeighbor{},
		CurrentWorkingDesign: spatial.WorkingDesign{},
		Instruction:          "Actually make it beige.",
	}
}

func TestSpatialReasoningAdapter_TranslatesContextAndMapsResponse(t *testing.T) {
	var gotBody platformai.SpatialReasoningRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		resp := platformai.SpatialReasoningResponse{
			Delta: platformai.SpatialProposedSceneEditDelta{
				SchemaVersion: 1, Target: platformai.SpatialTargetRef{Kind: "object", ID: "object_sofa_123"},
				Intent: "material_appearance", Summary: []string{"Change the sofa upholstery to beige."},
				Geometry: platformai.SpatialGeometryChange{Mode: "preserve"},
				Material: platformai.SpatialMaterialChange{Mode: "replace", Spec: &platformai.SpatialWorkingDesignMaterial{
					BaseColor: "beige", MaterialFamily: "fabric", Roughness: "matte",
				}},
				Spatial:  platformai.SpatialSpatialChange{Mode: "preserve"},
				Blockers: []platformai.SpatialProposedBlocker{}, Assumptions: []string{}, ReviewNotes: []string{},
				Confidence: 0.9,
			},
			Provider: "mock", Model: "spatial-mock-v1", PromptVersion: "v1", SchemaVersion: 1,
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer server.Close()

	client := platformai.NewSpatialClient(server.URL, "test-token", 5*time.Second)
	adapter := NewSpatialReasoningAdapter(client)

	delta, err := adapter.ReasonElement(context.Background(), testReasoningContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if delta.Target.ID != "object_sofa_123" {
		t.Fatalf("unexpected target: %+v", delta.Target)
	}
	if delta.Material.Mode != spatial.SectionModeReplace || delta.Material.MaterialSpec == nil || delta.Material.MaterialSpec.BaseColor != "beige" {
		t.Fatalf("expected material replaced with beige, got %+v", delta.Material)
	}
	if delta.Intent != spatial.DesignIntentMaterialAppearance {
		t.Fatalf("unexpected intent: %s", delta.Intent)
	}

	if gotBody.Instruction != "Actually make it beige." {
		t.Fatalf("expected instruction forwarded, got %q", gotBody.Instruction)
	}
	if gotBody.SelectedElement.ID != "object_sofa_123" {
		t.Fatalf("expected target forwarded, got %+v", gotBody.SelectedElement)
	}
}

func TestSpatialReasoningAdapter_ForwardsServerAuthoritativeReasoningIdentifiers(t *testing.T) {
	// A regression here recreates the production bug: Python rejects blank
	// turnId/roomDraftId before selecting a provider, so GLM is never called.
	request := toSpatialReasoningRequest(testReasoningContext())

	if request.TurnID != "turn_server_123" {
		t.Fatalf("expected server-authoritative turnId, got %q", request.TurnID)
	}
	if request.RoomDraftID != "roomdraft_server_456" {
		t.Fatalf("expected server-authoritative roomDraftId, got %q", request.RoomDraftID)
	}
	if request.RoomDraftRevision != 17 {
		t.Fatalf("expected authoritative roomDraftRevision 17, got %d", request.RoomDraftRevision)
	}
}

func TestSpatialReasoningAdapter_MapsPlatformErrorsToSpatialSentinels(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		code       string
		wantErr    error
	}{
		{"provider unavailable", http.StatusServiceUnavailable, "PROVIDER_UNAVAILABLE", spatial.ErrDesignReasoningProviderUnavailable},
		{"provider rate limited", http.StatusServiceUnavailable, "PROVIDER_RATE_LIMITED", spatial.ErrDesignReasoningProviderRejected},
		{"provider timeout", http.StatusServiceUnavailable, "PROVIDER_TIMEOUT", spatial.ErrDesignReasoningTimeout},
		{"invalid provider response", http.StatusBadGateway, "INVALID_PROVIDER_RESPONSE", spatial.ErrDesignReasoningInvalidOutput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				body, _ := json.Marshal(map[string]string{"code": tc.code, "detail": "fixed safe text"})
				w.Write(body)
			}))
			defer server.Close()

			client := platformai.NewSpatialClient(server.URL, "test-token", 5*time.Second)
			adapter := NewSpatialReasoningAdapter(client)

			_, err := adapter.ReasonElement(context.Background(), testReasoningContext())
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSpatialReasoningAdapter_TargetMismatchMapsToTargetMismatchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := platformai.SpatialReasoningResponse{
			Delta: platformai.SpatialProposedSceneEditDelta{
				SchemaVersion: 1, Target: platformai.SpatialTargetRef{Kind: "object", ID: "object_other_999"},
				Intent: "material_appearance", Summary: []string{"x"},
				Geometry: platformai.SpatialGeometryChange{Mode: "preserve"},
				Material: platformai.SpatialMaterialChange{Mode: "replace", Spec: &platformai.SpatialWorkingDesignMaterial{
					BaseColor: "beige", MaterialFamily: "fabric", Roughness: "matte",
				}},
				Spatial:     platformai.SpatialSpatialChange{Mode: "preserve"},
				Blockers:    []platformai.SpatialProposedBlocker{},
				Assumptions: []string{}, ReviewNotes: []string{}, Confidence: 0.9,
			},
			Provider: "mock", Model: "m", PromptVersion: "v", SchemaVersion: 1,
		}
		body, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer server.Close()

	client := platformai.NewSpatialClient(server.URL, "test-token", 5*time.Second)
	adapter := NewSpatialReasoningAdapter(client)

	_, err := adapter.ReasonElement(context.Background(), testReasoningContext())
	if err != spatial.ErrDesignPlanTargetMismatch {
		t.Fatalf("expected ErrDesignPlanTargetMismatch, got %v", err)
	}
}
