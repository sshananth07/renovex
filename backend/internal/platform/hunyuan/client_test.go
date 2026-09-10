package hunyuan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_StartShapeGeneration_ReturnsEventID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gradio_api/call/generate_shape" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("expected bearer auth, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"event_id": "evt_abc"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token", 5*time.Second)
	ref, err := client.StartShapeGeneration(context.Background(), Source{URL: "https://example.com/img.jpg"}, 42)
	if err != nil {
		t.Fatalf("StartShapeGeneration: %v", err)
	}
	if ref.ID != "evt_abc" {
		t.Errorf("expected event id evt_abc, got %q", ref.ID)
	}
}

// TestClient_StartShapeGeneration_SendsSourceURLAndSeed asserts the exact
// sanitized JSON request shape StartShapeGeneration sends, including the
// image input's full Gradio FileData object — live-verified 2026-09-08
// against the real private renovex-hunyuan3d-runtime Space: a bare
// {"path": url} object is silently rejected (the Space returns an
// "error"/data:null SSE event without ever fetching the image); the
// input must carry "url" and "meta": {"_type": "gradio.FileData"} as
// well, matching Gradio's real client-side FileData serialization.
func TestClient_StartShapeGeneration_SendsSourceURLAndSeed(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		json.NewEncoder(w).Encode(map[string]string{"event_id": "evt_1"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok", 5*time.Second)
	_, err := client.StartShapeGeneration(context.Background(), Source{URL: "https://example.com/source.jpg"}, 99)
	if err != nil {
		t.Fatalf("StartShapeGeneration: %v", err)
	}
	data, ok := capturedBody["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("expected a 2-element data array, got %+v", capturedBody)
	}
	fileData, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("expected data[0] to be an object, got %+v", data[0])
	}
	if fileData["path"] != "https://example.com/source.jpg" {
		t.Errorf("expected data[0].path to be the source URL, got %+v", fileData["path"])
	}
	if fileData["url"] != "https://example.com/source.jpg" {
		t.Errorf("expected data[0].url to be the source URL, got %+v", fileData["url"])
	}
	meta, ok := fileData["meta"].(map[string]any)
	if !ok || meta["_type"] != "gradio.FileData" {
		t.Errorf(`expected data[0].meta to be {"_type":"gradio.FileData"}, got %+v`, fileData["meta"])
	}
	if seed, ok := data[1].(float64); !ok || seed != 99 {
		t.Errorf("expected data[1] to be the seed 99, got %+v", data[1])
	}
}

func TestClient_StartShapeGeneration_ProviderErrorReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok", 5*time.Second)
	if _, err := client.StartShapeGeneration(context.Background(), Source{URL: "https://example.com/img.jpg"}, 1); err == nil {
		t.Fatal("expected an error for a non-2xx response")
	}
}

func TestClient_ResumeShapeGeneration_ParsesCompletedSSEEvent(t *testing.T) {
	var glbBytes = []byte{'g', 'l', 'T', 'F', 1, 2, 3, 4}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/file/output.glb", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("expected bearer auth on the output download, got %q", got)
		}
		w.Write(glbBytes)
	})
	mux.HandleFunc("/gradio_api/call/generate_shape/evt_abc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Live-verified 2026-09-08: the real Space's "complete" event data
		// is a JSON ARRAY of output FileData objects (one per Gradio
		// output component); "path" is a server-local filesystem path
		// (not fetchable by us), "url" is the actual absolute HTTPS
		// download URL. Deliberately different from each other here to
		// prove the client downloads from "url", not "path".
		fmt.Fprintf(w, "event: complete\ndata: [{\"path\": \"/tmp/gradio/not-fetchable/output.glb\", \"url\": \"%s/file/output.glb\"}]\n\n", server.URL)
	})

	client := NewClient(server.URL, "test-token", 5*time.Second)
	result, err := client.ResumeShapeGeneration(context.Background(), Ref{ID: "evt_abc"})
	if err != nil {
		t.Fatalf("ResumeShapeGeneration: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %q", result.Status)
	}
	if result.Output == nil {
		t.Fatal("expected non-nil Output")
	}
	defer result.Output.Close()
	got, err := io.ReadAll(result.Output)
	if err != nil {
		t.Fatalf("reading Output: %v", err)
	}
	if string(got) != string(glbBytes) {
		t.Errorf("expected output bytes %v, got %v", glbBytes, got)
	}
}

// TestClient_ResumeShapeGeneration_IgnoresHeartbeatEvents reflects an
// actual observed live sequence (2026-09-08): the real Space emits
// "event: heartbeat\ndata: null" one or more times while a generation is
// in progress, before the terminal "complete" event. These must be
// skipped, not misclassified as an error or a completion.
func TestClient_ResumeShapeGeneration_IgnoresHeartbeatEvents(t *testing.T) {
	var glbBytes = []byte{'g', 'l', 'T', 'F', 9, 9, 9}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/file/output.glb", func(w http.ResponseWriter, r *http.Request) {
		w.Write(glbBytes)
	})
	mux.HandleFunc("/gradio_api/call/generate_shape/evt_hb", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: heartbeat\ndata: null\n\n")
		fmt.Fprint(w, "event: heartbeat\ndata: null\n\n")
		fmt.Fprintf(w, "event: complete\ndata: [{\"path\": \"/tmp/gradio/not-fetchable/output.glb\", \"url\": \"%s/file/output.glb\"}]\n\n", server.URL)
	})

	client := NewClient(server.URL, "tok", 5*time.Second)
	result, err := client.ResumeShapeGeneration(context.Background(), Ref{ID: "evt_hb"})
	if err != nil {
		t.Fatalf("ResumeShapeGeneration: %v", err)
	}
	if result.Status != StatusCompleted {
		t.Fatalf("expected heartbeats to be skipped and StatusCompleted reached, got %q", result.Status)
	}
}

func TestClient_ResumeShapeGeneration_ParsesQuotaBlockedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: error\ndata: \"ZeroGPU quota exceeded, please retry later\"\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok", 5*time.Second)
	result, err := client.ResumeShapeGeneration(context.Background(), Ref{ID: "evt_1"})
	if err != nil {
		t.Fatalf("ResumeShapeGeneration: %v", err)
	}
	if result.Status != StatusQuotaBlocked {
		t.Errorf("expected StatusQuotaBlocked, got %q", result.Status)
	}
}

func TestClient_ResumeShapeGeneration_ParsesRejectedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: error\ndata: \"invalid input image\"\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok", 5*time.Second)
	result, err := client.ResumeShapeGeneration(context.Background(), Ref{ID: "evt_1"})
	if err != nil {
		t.Fatalf("ResumeShapeGeneration: %v", err)
	}
	if result.Status != StatusRejected {
		t.Errorf("expected StatusRejected, got %q", result.Status)
	}
}

// TestClient_ResumeShapeGeneration_ParsesNullErrorData reflects an actual
// observed response from the live renovex-hunyuan3d-runtime Space
// (2026-09-08 live verification): its "error" SSE event carries a bare
// JSON `null` data payload, not a quoted message string. This must
// classify as StatusRejected (never crash, never silently misclassify as
// StatusPending) with an empty/best-effort FailureMessage — the quota
// heuristic simply cannot fire when there is no message text to inspect.
func TestClient_ResumeShapeGeneration_ParsesNullErrorData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: error\ndata: null\n\n")
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok", 5*time.Second)
	result, err := client.ResumeShapeGeneration(context.Background(), Ref{ID: "evt_1"})
	if err != nil {
		t.Fatalf("ResumeShapeGeneration: %v", err)
	}
	if result.Status != StatusRejected {
		t.Errorf("expected StatusRejected for a null error payload, got %q", result.Status)
	}
}

func TestClient_ResumeShapeGeneration_NoTerminalEventReturnsPending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// No terminal event at all — connection just ends (e.g. still processing).
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok", 5*time.Second)
	result, err := client.ResumeShapeGeneration(context.Background(), Ref{ID: "evt_1"})
	if err != nil {
		t.Fatalf("ResumeShapeGeneration: %v", err)
	}
	if result.Status != StatusPending {
		t.Errorf("expected StatusPending when no terminal event is seen, got %q", result.Status)
	}
}
