package composition

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// spatialGenerationWakeResponseMaxBytes bounds the enqueue bridge's
// response body — it only ever returns a tiny JSON acknowledgement, never a
// payload whose size depends on caller input.
const spatialGenerationWakeResponseMaxBytes = 4096

// spatialGenerationWakeClient implements spatial.GenerationWakePublisher by
// calling the Web project's shared-secret-protected enqueue bridge
// (RP4E2 Gate 4: POST {kind, id} to
// /api/internal/spatial-generation/enqueue, which publishes to the
// matching Vercel Queue topic with @vercel/queue). This is the ONLY place
// internal/spatial's GenerationWakeKind is translated to the Web route's
// wire contract, matching referenceImageAdapter's "thin translation layer"
// precedent — internal/spatial never imports net/http itself.
type spatialGenerationWakeClient struct {
	url        string
	token      string
	httpClient *http.Client
}

// NewSpatialGenerationWakeClient constructs a client against url (the full
// enqueue route, e.g. SPATIAL_QUEUE_ENQUEUE_URL), bounding every call by
// timeout and refusing to follow redirects — same transport discipline as
// platformai.ReferenceImageClient.
func NewSpatialGenerationWakeClient(url, token string, timeout time.Duration) *spatialGenerationWakeClient {
	return &spatialGenerationWakeClient{
		url:   url,
		token: token,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

type spatialGenerationWakeRequestBody struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// PublishGenerationWake makes exactly one HTTP request. Callers
// (spatial.Service.notifyGenerationWake) already treat any error as
// best-effort and swallow it, so this never retries internally.
func (c *spatialGenerationWakeClient) PublishGenerationWake(ctx context.Context, kind spatial.GenerationWakeKind, id string) error {
	payload, err := json.Marshal(spatialGenerationWakeRequestBody{Kind: string(kind), ID: id})
	if err != nil {
		return fmt.Errorf("composition: encoding generation wake request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("composition: building generation wake request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Spatial-Queue-Enqueue-Token", c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("composition: calling generation wake enqueue route: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, spatialGenerationWakeResponseMaxBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("composition: generation wake enqueue route returned status %d", resp.StatusCode)
	}
	return nil
}
