// Package ai is the authenticated Go->Python AI service HTTP client
// (M8.5B-A design doc §13, §21.1). It knows only Python's stable internal
// error codes, never provider details — Gemini/Pydantic/FastAPI internals
// stop at the Python service boundary. This package has no domain
// knowledge; internal/ai and internal/aiintegration compose it.
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

// Client is the Go->Python AI service HTTP client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient constructs a Client against baseURL, authenticating every
// request with Authorization: Bearer token, and bounding every call by
// timeout.
func NewClient(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) SuggestSpaces(ctx context.Context, req SpaceSuggestionRequest) (SpaceSuggestionResponse, error) {
	var resp SpaceSuggestionResponse
	err := c.post(ctx, "/internal/v1/spaces/suggest", req, &resp)
	return resp, err
}

func (c *Client) SuggestWorkItems(ctx context.Context, req WorkItemSuggestionRequest) (WorkItemSuggestionResponse, error) {
	var resp WorkItemSuggestionResponse
	err := c.post(ctx, "/internal/v1/work-items/suggest", req, &resp)
	return resp, err
}

func (c *Client) SuggestResources(ctx context.Context, req ResourceSuggestionRequest) (ResourceSuggestionResponse, error) {
	var resp ResourceSuggestionResponse
	err := c.post(ctx, "/internal/v1/resources/suggest", req, &resp)
	return resp, err
}

// errorResponseBody is the shape errors.py's exception handler writes
// (ai-service/app/main.py: {"code": ..., "detail": ...}). detail is a
// contractor-safe fixed message from Python, never a raw provider body —
// but this Go client still never echoes it in its own error text, keeping
// one layer of defense even if that ever regressed.
type errorResponseBody struct {
	Code string `json:"code"`
}

// post is the one shared HTTP execution method every Suggest* call uses.
// No provider knowledge lives here — only Python's stable internal error
// codes.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ai: encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("ai: building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeoutErr(err) {
			return ErrProviderTimeout
		}
		return ErrServiceUnavailable
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return ErrInvalidServiceResponse
	}

	if httpResp.StatusCode >= 200 && httpResp.StatusCode < 300 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return ErrInvalidServiceResponse
		}
		return nil
	}

	var errBody errorResponseBody
	if err := json.Unmarshal(respBody, &errBody); err != nil {
		return ErrInvalidServiceResponse
	}
	if sentinel, ok := errorCodeSentinels[errBody.Code]; ok {
		return sentinel
	}
	return ErrServiceUnavailable
}

// isTimeoutErr reports whether err (typically from http.Client.Do) is a
// client-side timeout, covering both context deadline expiry and the
// net.Error Timeout() interface that http.Client wraps transport timeouts
// in.
func isTimeoutErr(err error) bool {
	type timeouter interface{ Timeout() bool }
	var t timeouter
	if errors.As(err, &t) {
		return t.Timeout()
	}
	return false
}
