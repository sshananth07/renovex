package ai

import "errors"

// These typed sentinels mirror the Python ai-service's internal error codes
// (M8.5B-A design doc §22) one-to-one. Go callers switch on these, never on
// parsed English text from the Python response body.
var (
	ErrInvalidRequest          = errors.New("ai: invalid AI request")
	ErrProviderUnavailable     = errors.New("ai: provider unavailable")
	ErrProviderRateLimited     = errors.New("ai: provider rate limited")
	ErrProviderTimeout         = errors.New("ai: provider request timed out")
	ErrInvalidProviderResponse = errors.New("ai: invalid provider response")
	ErrServiceUnavailable      = errors.New("ai: service unavailable")
	// ErrInvalidServiceResponse is a Go-side sentinel (not one of Python's
	// error codes) for a response that isn't valid JSON, or valid JSON with
	// none of the recognized error codes.
	ErrInvalidServiceResponse = errors.New("ai: invalid or unrecognized service response")
)

// errorCodeSentinels maps the Python internal error code strings (design
// doc §22) to Go sentinels. Any code that fails to match here still maps to
// ErrServiceUnavailable in decodeErrorResponse's default branch, so an
// unrecognized/future code degrades safely rather than surfacing the raw
// string.
var errorCodeSentinels = map[string]error{
	"INVALID_AI_REQUEST":        ErrInvalidRequest,
	"PROVIDER_UNAVAILABLE":      ErrProviderUnavailable,
	"PROVIDER_RATE_LIMITED":     ErrProviderRateLimited,
	"PROVIDER_TIMEOUT":          ErrProviderTimeout,
	"INVALID_PROVIDER_RESPONSE": ErrInvalidProviderResponse,
	"AI_SERVICE_UNAVAILABLE":    ErrServiceUnavailable,
}
