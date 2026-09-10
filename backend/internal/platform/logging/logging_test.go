package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewWritesJSONWithLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, "info")

	logger.Info().Str("component", "test").Msg("hello world")

	output := buf.String()
	if !strings.Contains(output, `"message":"hello world"`) {
		t.Fatalf("expected message field in output, got: %s", output)
	}
	if !strings.Contains(output, `"component":"test"`) {
		t.Fatalf("expected component field in output, got: %s", output)
	}
}
