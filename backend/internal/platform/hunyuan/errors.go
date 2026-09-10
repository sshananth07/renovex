package hunyuan

import "errors"

var (
	ErrProviderUnavailable     = errors.New("hunyuan: provider unavailable")
	ErrProviderTimeout         = errors.New("hunyuan: provider request timed out")
	ErrInvalidProviderResponse = errors.New("hunyuan: invalid provider response")
)
