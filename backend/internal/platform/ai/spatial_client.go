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

	"github.com/rs/zerolog"
)

// spatialErrorResponseBody mirrors errorResponseBody's role in client.go
// but is DisallowUnknownFields-safe against Python's actual
// AIServiceError handler shape ({"code": ..., "detail": ...}) — the shared
// errorResponseBody type only declares Code, so decoding an error body
// through it with DisallowUnknownFields would reject "detail" as unknown.
type spatialErrorResponseBody struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// maxSpatialResponseBytes bounds every response body read from the Python
// spatial-reasoning route — a strict, separate bound from Copilot's
// unbounded io.ReadAll(client.go's post), matching RP4E1 plan Task 4 Step
// 3's explicit requirement for a bounded decoder distinct from Copilot's
// existing decoding path.
const maxSpatialResponseBytes = 1 << 20 // 1 MiB

// SpatialClient is a SEPARATE Go->Python HTTP client from Client
// (client.go), used only for
// POST /internal/v1/spatial/element-proposals/reason. It never modifies
// Copilot's existing Client.post behavior. Unlike Client, it disallows
// unknown response fields and never follows redirects — GLM/Python's
// contract for this route never legitimately redirects, so a 3xx response
// is treated as invalid output rather than followed.
type SpatialClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     zerolog.Logger
}

// NewSpatialClient constructs a SpatialClient against baseURL, bounding
// every call by timeout and refusing to follow redirects
// (http.ErrUseLastResponse, mirroring the plan's "no redirects" requirement
// exactly like SpatialProvider's own transport-level policy).
func NewSpatialClient(baseURL, token string, timeout time.Duration) *SpatialClient {
	return newSpatialClient(baseURL, token, timeout, zerolog.Nop())
}

// NewSpatialClientWithLogger constructs a SpatialClient with safe outbound
// request-boundary diagnostics. The logger never receives credentials,
// authorization headers, prompts, or RoomDraft payloads.
func NewSpatialClientWithLogger(baseURL, token string, timeout time.Duration, logger zerolog.Logger) *SpatialClient {
	return newSpatialClient(baseURL, token, timeout, logger)
}

func newSpatialClient(baseURL, token string, timeout time.Duration, logger zerolog.Logger) *SpatialClient {
	return &SpatialClient{
		baseURL: baseURL,
		token:   token,
		logger:  logger,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ReasonElement makes exactly one HTTP request to Python's spatial
// reasoning route. Callers (spatial's design service) are themselves
// responsible for never invoking this more than once per durable turn —
// this client has no retry logic of its own.
func (c *SpatialClient) ReasonElement(ctx context.Context, req SpatialReasoningRequest) (SpatialReasoningResponse, error) {
	var resp SpatialReasoningResponse
	startedAt := time.Now()

	payload, err := json.Marshal(req)
	if err != nil {
		c.logFailure(req, startedAt, "request_encode_failed", "service_unavailable")
		return SpatialReasoningResponse{}, fmt.Errorf("ai: encoding spatial reasoning request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/spatial/element-proposals/reason", bytes.NewReader(payload))
	if err != nil {
		c.logFailure(req, startedAt, "request_build_failed", "service_unavailable")
		return SpatialReasoningResponse{}, fmt.Errorf("ai: building spatial reasoning request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	c.logger.Info().
		Str("turn_id", req.TurnID).
		Str("room_draft_id", req.RoomDraftID).
		Int64("room_draft_revision", req.RoomDraftRevision).
		Str("destination_host", httpReq.URL.Host).
		Str("destination_path", httpReq.URL.Path).
		Msg("spatial reasoning request started")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeoutErr(err) {
			c.logFailure(req, startedAt, "transport_timeout", "provider_timeout")
			return SpatialReasoningResponse{}, ErrProviderTimeout
		}
		c.logFailure(req, startedAt, "transport_failure", "service_unavailable")
		return SpatialReasoningResponse{}, ErrServiceUnavailable
	}
	defer httpResp.Body.Close()
	c.logger.Info().
		Str("turn_id", req.TurnID).
		Int("http_status", httpResp.StatusCode).
		Dur("duration", time.Since(startedAt)).
		Msg("spatial reasoning response received")

	// CheckRedirect above prevents http.Client from following a 3xx
	// automatically; a redirect status here means it was returned directly
	// (net/http still surfaces 3xx as a normal response when
	// CheckRedirect returns ErrUseLastResponse) — treated as invalid
	// output, never followed.
	if httpResp.StatusCode >= 300 && httpResp.StatusCode < 400 {
		c.logFailure(req, startedAt, "unexpected_redirect", "invalid_service_response")
		return SpatialReasoningResponse{}, ErrInvalidServiceResponse
	}

	// Bounded read: +1 so a body of EXACTLY the limit is distinguishable
	// from one that was truncated by the limiter (an oversized body reads
	// maxSpatialResponseBytes+1 bytes and is rejected below).
	bounded := io.LimitReader(httpResp.Body, maxSpatialResponseBytes+1)
	rawBody, err := io.ReadAll(bounded)
	if err != nil {
		c.logFailure(req, startedAt, "response_read_failed", "invalid_service_response")
		return SpatialReasoningResponse{}, ErrInvalidServiceResponse
	}
	if len(rawBody) > maxSpatialResponseBytes {
		c.logFailure(req, startedAt, "response_too_large", "invalid_service_response")
		return SpatialReasoningResponse{}, ErrInvalidServiceResponse
	}

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		decoder := json.NewDecoder(bytes.NewReader(rawBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&resp); err != nil {
			c.logFailure(req, startedAt, "response_decode_failed", "invalid_service_response")
			return SpatialReasoningResponse{}, ErrInvalidServiceResponse
		}
		// A well-formed single JSON value must consume the entire body —
		// trailing content (a second JSON value, garbage) is rejected
		// rather than silently ignored.
		if decoder.More() {
			c.logFailure(req, startedAt, "response_trailing_content", "invalid_service_response")
			return SpatialReasoningResponse{}, ErrInvalidServiceResponse
		}
		return resp, nil
	}

	// spatialErrorResponseBody (not the shared errorResponseBody) since
	// Python's AIServiceError handler body also carries "detail" — an
	// unknown field to errorResponseBody's stricter shape, but expected
	// here.
	var errBody spatialErrorResponseBody
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&errBody); err != nil {
		c.logFailure(req, startedAt, "error_response_decode_failed", "invalid_service_response")
		return SpatialReasoningResponse{}, ErrInvalidServiceResponse
	}
	if sentinel, ok := errorCodeSentinels[errBody.Code]; ok {
		c.logger.Warn().
			Str("turn_id", req.TurnID).
			Int("http_status", httpResp.StatusCode).
			Str("safe_application_error_code", errBody.Code).
			Dur("duration", time.Since(startedAt)).
			Msg("spatial reasoning service rejected request")
		return SpatialReasoningResponse{}, sentinel
	}
	c.logFailure(req, startedAt, "unrecognized_service_error", "service_unavailable")
	return SpatialReasoningResponse{}, ErrServiceUnavailable
}

func (c *SpatialClient) logFailure(req SpatialReasoningRequest, startedAt time.Time, failureClass, safeErrorCode string) {
	c.logger.Warn().
		Str("turn_id", req.TurnID).
		Str("room_draft_id", req.RoomDraftID).
		Int64("room_draft_revision", req.RoomDraftRevision).
		Str("failure_class", failureClass).
		Str("safe_error_code", safeErrorCode).
		Dur("duration", time.Since(startedAt)).
		Msg("spatial reasoning request failed")
}
