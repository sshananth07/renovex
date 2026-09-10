// Package hunyuan is the authenticated Go->Hugging Face Gradio HTTP
// client for the private renovex-hunyuan3d-runtime Space (RP4E0). It
// implements internal/spatial.AssetGenerationProvider's two-phase
// contract against Gradio's real HTTP protocol: POST returns an
// event_id immediately; GET .../{event_id} streams SSE terminating in a
// "complete" event carrying the output GLB's URL. This package has no
// domain knowledge — internal/platform/composition wires it into
// spatial.
//
// LIVE VERIFIED 2026-09-08/09 against the real private
// admamzr/renovex-hunyuan3d-runtime Space (Gradio 5.33.0, protocol
// sse_v3), through this package's actual StartShapeGeneration/
// ResumeShapeGeneration codepath end to end (not a bypass/diagnostic
// tool): submit -> StartShapeGeneration -> ResumeShapeGeneration ->
// real GLB download -> RP4E0 geometry validation -> PublishVisualAssetVersion
// all completed successfully against a real Hunyuan3D generation, twice.
//
// CONFIRMED live:
//   - Both endpoints live under Gradio 5.x's versioned "/gradio_api"
//     prefix: "/gradio_api/call/generate_shape" (POST, returns
//     {"event_id":"..."}) and "/gradio_api/call/generate_shape/{event_id}"
//     (GET, SSE).
//   - The image input must be a full Gradio FileData object —
//     {"path":url,"url":url,"meta":{"_type":"gradio.FileData"}} — not a
//     bare {"path":url}; the latter is silently rejected (an
//     "error"/data:null SSE event, the Space never even fetching the
//     image).
//   - "heartbeat" SSE events (data: null) are emitted one or more times
//     during processing before the terminal event, and are correctly
//     ignored (fall through handleSSEEvent's default case).
//   - The "complete" event's data is a JSON ARRAY of FileData objects —
//     [{"path":...,"url":...,...}] — and the actual fetchable bytes live
//     at "url", not "path" (which is a server-local filesystem path, not
//     fetchable by this client).
//   - The "error" event's data payload can be a bare JSON `null` (not a
//     quoted message string) — handleSSEEvent's json.Unmarshal into a
//     string degrades this safely to an empty FailureMessage and
//     StatusRejected, never a crash or a silent misclassification as
//     Pending.
//   - A Gradio SSE session (event_id) can expire ("404: Session not
//     found") if ResumeShapeGeneration is not called promptly after
//     StartShapeGeneration — consistent with (not contradicting) this
//     package's existing two-phase design.
//
// REMAINING OPEN ITEM: no live ZeroGPU quota-exhaustion event was
// observed (deliberately not provoked, since doing so would require
// actually exhausting shared GPU quota) — whether/how this Space
// signals quota exhaustion via the SSE error payload (message text, a
// distinct event name, or an HTTP-level signal) remains unconfirmed.
// The existing quota/zerogpu substring heuristic is retained as
// best-effort but cannot be verified until a real quota-exhaustion
// event is observed.
package hunyuan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type Source struct{ URL string }

type Ref struct{ ID string }

type Status string

const (
	StatusPending      Status = "pending"
	StatusCompleted    Status = "completed"
	StatusQuotaBlocked Status = "quota_blocked"
	StatusRejected     Status = "rejected"
)

type Result struct {
	Status         Status
	Output         io.ReadCloser
	FailureMessage string
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{baseURL: baseURL, token: token, httpClient: &http.Client{Timeout: timeout}}
}

func (c *Client) StartShapeGeneration(ctx context.Context, source Source, seed int64) (Ref, error) {
	// Live-verified 2026-09-08: the real Space silently rejects a bare
	// {"path": url} image input (an "error"/data:null SSE event, never
	// even fetching the image) — Gradio's actual FileData wire shape
	// requires "url" and the "meta": {"_type": "gradio.FileData"} marker
	// as well.
	fileData := map[string]any{
		"path": source.URL,
		"url":  source.URL,
		"meta": map[string]string{"_type": "gradio.FileData"},
	}
	body, _ := json.Marshal(map[string]any{"data": []any{fileData, seed}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/gradio_api/call/generate_shape", bytes.NewReader(body))
	if err != nil {
		return Ref{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Ref{}, ErrProviderUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Ref{}, ErrProviderUnavailable
	}
	var parsed struct {
		EventID string `json:"event_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || parsed.EventID == "" {
		return Ref{}, ErrInvalidProviderResponse
	}
	return Ref{ID: parsed.EventID}, nil
}

func (c *Client) ResumeShapeGeneration(ctx context.Context, ref Ref) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/gradio_api/call/generate_shape/"+ref.ID, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, ErrProviderUnavailable
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var eventType, eventData string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			eventData = strings.TrimPrefix(line, "data: ")
		case line == "" && eventType != "":
			result, done, err := c.handleSSEEvent(ctx, eventType, eventData)
			if done {
				return result, err
			}
			eventType, eventData = "", ""
		}
	}
	return Result{Status: StatusPending}, nil
}

func (c *Client) handleSSEEvent(ctx context.Context, eventType, data string) (Result, bool, error) {
	switch eventType {
	case "complete":
		// Live-verified 2026-09-08: the real Space's "complete" event data
		// is a JSON ARRAY of output FileData objects (one per Gradio
		// output component) — [{"path": ..., "url": ..., ...}] — not a
		// bare object.
		var outputs []struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(data), &outputs); err != nil || len(outputs) == 0 || outputs[0].URL == "" {
			return Result{}, true, ErrInvalidProviderResponse
		}
		output, err := c.downloadOutput(ctx, outputs[0].URL)
		if err != nil {
			return Result{}, true, err
		}
		return Result{Status: StatusCompleted, Output: output}, true, nil
	case "error":
		var message string
		if err := json.Unmarshal([]byte(data), &message); err != nil {
			message = data // fall back to the raw string if it wasn't JSON-quoted
		}
		if strings.Contains(strings.ToLower(message), "quota") || strings.Contains(strings.ToLower(message), "zerogpu") {
			return Result{Status: StatusQuotaBlocked, FailureMessage: message}, true, nil
		}
		return Result{Status: StatusRejected, FailureMessage: message}, true, nil
	default:
		return Result{}, false, nil
	}
}

func (c *Client) downloadOutput(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, ErrProviderUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, ErrProviderUnavailable
	}
	return resp.Body, nil
}
