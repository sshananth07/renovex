package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func validSpatialRequest() SpatialReasoningRequest {
	return SpatialReasoningRequest{
		SchemaVersion:     1,
		TurnID:            "turn_002",
		RoomDraftID:       "roomdraft_001",
		RoomDraftRevision: 17,
		SelectedElement: SpatialSelectedElement{
			Kind:     "object",
			ID:       "object_sofa_123",
			Category: "sofa",
			Transform: SpatialRoomLocalTransform{
				Position: SpatialRoomLocalPoint{X: 1.2, Y: 0, Z: 2.4},
				Rotation: SpatialRoomLocalQuaternion{X: 0, Y: 0, Z: 0, W: 1},
			},
			VisualAssetBound: false,
		},
		Context: SpatialReasoningNeighborhood{
			Walls:     []SpatialContextWall{},
			Openings:  []map[string]any{},
			Neighbors: []map[string]any{},
		},
		CurrentWorkingDesign: SpatialWorkingDesign{
			ResolvedSpatialOperations: []map[string]any{},
		},
		LastSuccessfulPlanSummary: []string{},
		Instruction:               "Actually make it beige.",
		AllowedSpatialOperations:  []string{"move_relative_to_nearest_wall", "resize_axis"},
		MaterialFamilyEnum:        []string{"fabric", "leather", "wood", "metal", "stone", "other"},
		RoughnessEnum:             []string{"matte", "satin", "glossy"},
		PreservationDefaults:      SpatialPreservationDefaults{Geometry: true, Material: true, Spatial: true},
	}
}

func validSpatialResponseBody() []byte {
	body, _ := json.Marshal(SpatialReasoningResponse{
		Delta: SpatialProposedSceneEditDelta{
			SchemaVersion: 1,
			Target:        SpatialTargetRef{Kind: "object", ID: "object_sofa_123"},
			Intent:        "material_appearance",
			Summary:       []string{"Change the sofa upholstery to beige."},
			Geometry:      SpatialGeometryChange{Mode: "preserve"},
			Material: SpatialMaterialChange{
				Mode: "replace",
				Spec: &SpatialWorkingDesignMaterial{BaseColor: "beige", MaterialFamily: "fabric", Roughness: "matte", Metallic: false},
			},
			Spatial:     SpatialSpatialChange{Mode: "preserve"},
			Blockers:    []SpatialProposedBlocker{},
			Assumptions: []string{},
			ReviewNotes: []string{},
			Confidence:  0.91,
		},
		Provider:      "mock",
		Model:         "spatial-mock-v1",
		PromptVersion: "spatial-reasoning-mock-v1",
		SchemaVersion: 1,
	})
	return body
}

func TestSpatialClient_ExactPathAuthBody(t *testing.T) {
	var gotPath, gotAuth, gotMethod string
	var gotBody SpatialReasoningRequest
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
		w.Write(validSpatialResponseBody())
	}))
	defer server.Close()

	client := NewSpatialClient(server.URL, "test-token", 5*time.Second)
	resp, err := client.ReasonElement(context.Background(), validSpatialRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 request, got %d", callCount)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/internal/v1/spatial/element-proposals/reason" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("unexpected Authorization header: %s", gotAuth)
	}
	if gotBody.Instruction != "Actually make it beige." {
		t.Fatalf("request body not forwarded correctly: %+v", gotBody)
	}
	if resp.Delta.Target.ID != "object_sofa_123" {
		t.Fatalf("unexpected response target: %+v", resp)
	}
}

func TestSpatialClient_NoRedirectsFollowed(t *testing.T) {
	var callCount int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("redirect target must never be reached")
	}))
	defer target.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()

	client := NewSpatialClient(server.URL, "test-token", 5*time.Second)
	_, err := client.ReasonElement(context.Background(), validSpatialRequest())
	if err == nil {
		t.Fatal("expected an error for an unexpected redirect response")
	}
	if callCount != 1 {
		t.Fatalf("expected exactly 1 request to the primary server, got %d", callCount)
	}
}

func TestSpatialClient_ContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write(validSpatialResponseBody())
	}))
	defer server.Close()

	client := NewSpatialClient(server.URL, "test-token", 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := client.ReasonElement(ctx, validSpatialRequest())
	if err != ErrProviderTimeout {
		t.Fatalf("expected ErrProviderTimeout, got %v", err)
	}
}

func TestSpatialClient_ClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write(validSpatialResponseBody())
	}))
	defer server.Close()

	client := NewSpatialClient(server.URL, "test-token", 5*time.Millisecond)
	_, err := client.ReasonElement(context.Background(), validSpatialRequest())
	if err != ErrProviderTimeout {
		t.Fatalf("expected ErrProviderTimeout, got %v", err)
	}
}

func TestSpatialClient_BoundedSuccessBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Oversized body: must be rejected rather than fully buffered.
		huge := make([]byte, 5*1024*1024)
		for i := range huge {
			huge[i] = ' '
		}
		w.Write([]byte(`{"delta":`))
		w.Write(huge)
	}))
	defer server.Close()

	client := NewSpatialClient(server.URL, "test-token", 5*time.Second)
	_, err := client.ReasonElement(context.Background(), validSpatialRequest())
	if err != ErrInvalidServiceResponse {
		t.Fatalf("expected ErrInvalidServiceResponse for oversized body, got %v", err)
	}
}

func TestSpatialClient_UnknownFieldRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"delta":{}, "provider":"mock", "model":"m", "promptVersion":"v", "schemaVersion":1, "unexpectedField":"nope"}`))
	}))
	defer server.Close()

	client := NewSpatialClient(server.URL, "test-token", 5*time.Second)
	_, err := client.ReasonElement(context.Background(), validSpatialRequest())
	if err != ErrInvalidServiceResponse {
		t.Fatalf("expected ErrInvalidServiceResponse for unknown field, got %v", err)
	}
}

func TestSpatialClient_ErrorCodeMapping(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		code       string
		wantErr    error
	}{
		{"invalid request", http.StatusUnprocessableEntity, "INVALID_AI_REQUEST", ErrInvalidRequest},
		{"provider unavailable", http.StatusServiceUnavailable, "PROVIDER_UNAVAILABLE", ErrProviderUnavailable},
		{"provider rate limited", http.StatusServiceUnavailable, "PROVIDER_RATE_LIMITED", ErrProviderRateLimited},
		{"provider timeout", http.StatusServiceUnavailable, "PROVIDER_TIMEOUT", ErrProviderTimeout},
		{"invalid provider response", http.StatusBadGateway, "INVALID_PROVIDER_RESPONSE", ErrInvalidProviderResponse},
		{"unrecognized code", http.StatusServiceUnavailable, "SOMETHING_NEW", ErrServiceUnavailable},
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

			client := NewSpatialClient(server.URL, "test-token", 5*time.Second)
			_, err := client.ReasonElement(context.Background(), validSpatialRequest())
			if err != tc.wantErr {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}
