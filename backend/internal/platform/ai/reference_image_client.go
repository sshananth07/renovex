package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// referenceImageResponseOverheadBytes bounds the NON-image portion of a
// response body (JSON structure, provider/model strings) on top of the
// caller-supplied max image byte budget — the base64-encoded image itself
// can be up to ~4/3 of the raw byte budget, so the total response bound
// must accommodate that encoding overhead, not just the raw budget.
const referenceImageResponseOverheadBytes = 1 << 16 // 64 KiB

// ReferenceImageClient is a SEPARATE Go->Python HTTP client from Client and
// SpatialClient, used only for
// POST /internal/v1/spatial/reference-images/generate. Same "disallow
// unknown fields, never follow redirects, bounded read" discipline as
// SpatialClient.
type ReferenceImageClient struct {
	baseURL    string
	token      string
	maxBytes   int64
	httpClient *http.Client
}

// NewReferenceImageClient constructs a ReferenceImageClient against
// baseURL, bounding every call by timeout and refusing to follow
// redirects. maxImageBytes is the caller's configured
// REFERENCE_IMAGE_MAX_BYTES — the response-body read bound is derived from
// it (base64 encoding overhead plus a fixed JSON-structure allowance), so
// this client never needs a second independently-tuned constant.
func NewReferenceImageClient(baseURL, token string, maxImageBytes int64, timeout time.Duration) *ReferenceImageClient {
	return &ReferenceImageClient{
		baseURL:  baseURL,
		token:    token,
		maxBytes: (maxImageBytes*4)/3 + referenceImageResponseOverheadBytes,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// GenerateReference makes exactly one HTTP request to Python's
// reference-image generation route. Callers (spatial's design generation
// service) are themselves responsible for the at-most-once dispatch
// guarantee per attempt — this client has no retry logic of its own.
func (c *ReferenceImageClient) GenerateReference(ctx context.Context, req ReferenceImageRequest) (ReferenceImageResponse, error) {
	var resp ReferenceImageResponse

	payload, err := json.Marshal(req)
	if err != nil {
		return ReferenceImageResponse{}, fmt.Errorf("ai: encoding reference image request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/spatial/reference-images/generate", bytes.NewReader(payload))
	if err != nil {
		return ReferenceImageResponse{}, fmt.Errorf("ai: building reference image request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeoutErr(err) {
			return ReferenceImageResponse{}, ErrProviderTimeout
		}
		return ReferenceImageResponse{}, ErrServiceUnavailable
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 300 && httpResp.StatusCode < 400 {
		return ReferenceImageResponse{}, ErrInvalidServiceResponse
	}

	bounded := io.LimitReader(httpResp.Body, c.maxBytes+1)
	rawBody, err := io.ReadAll(bounded)
	if err != nil {
		return ReferenceImageResponse{}, ErrInvalidServiceResponse
	}
	if int64(len(rawBody)) > c.maxBytes {
		return ReferenceImageResponse{}, ErrInvalidServiceResponse
	}

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		decoder := json.NewDecoder(bytes.NewReader(rawBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&resp); err != nil {
			return ReferenceImageResponse{}, ErrInvalidServiceResponse
		}
		if decoder.More() {
			return ReferenceImageResponse{}, ErrInvalidServiceResponse
		}
		return resp, nil
	}

	var errBody spatialErrorResponseBody
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&errBody); err != nil {
		return ReferenceImageResponse{}, ErrInvalidServiceResponse
	}
	if sentinel, ok := errorCodeSentinels[errBody.Code]; ok {
		return ReferenceImageResponse{}, sentinel
	}
	return ReferenceImageResponse{}, ErrServiceUnavailable
}
