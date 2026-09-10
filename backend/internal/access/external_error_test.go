package access

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestExternalUnknownErrorsBecomeBoundedNoEcho503(t *testing.T) {
	canary := "mongo topology failed at secret-host:27017 with token=raw-secret"
	mapped := mapExternalError(errors.New(canary))

	var status huma.StatusError
	if !errors.As(mapped, &status) {
		t.Fatalf("mapped error %v is not an HTTP status error", mapped)
	}
	if status.GetStatus() != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status.GetStatus())
	}
	message := status.Error()
	if len(message) > 200 {
		t.Errorf("external error is unbounded: %d bytes", len(message))
	}
	for _, forbidden := range []string{canary, "mongo", "secret-host", "raw-secret"} {
		if strings.Contains(message, forbidden) {
			t.Errorf("external error %q echoes private input %q", message, forbidden)
		}
	}
}
