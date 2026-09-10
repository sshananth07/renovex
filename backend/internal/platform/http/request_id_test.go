package http_test

import (
	"strings"
	"testing"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func TestValidRequestIDAcceptsPrintableASCII(t *testing.T) {
	if !platformhttp.IsValidRequestID("abc-123_XYZ.456") {
		t.Fatal("expected a printable ASCII request ID to be valid")
	}
}

func TestValidRequestIDAccepts1Byte(t *testing.T) {
	if !platformhttp.IsValidRequestID("a") {
		t.Fatal("expected a 1-byte request ID to be valid")
	}
}

func TestValidRequestIDAccepts128Bytes(t *testing.T) {
	if !platformhttp.IsValidRequestID(strings.Repeat("a", 128)) {
		t.Fatal("expected a 128-byte request ID to be valid")
	}
}

func TestValidRequestIDRejectsEmpty(t *testing.T) {
	if platformhttp.IsValidRequestID("") {
		t.Fatal("expected an empty request ID to be invalid")
	}
}

func TestValidRequestIDRejectsOver128Bytes(t *testing.T) {
	if platformhttp.IsValidRequestID(strings.Repeat("a", 129)) {
		t.Fatal("expected a 129-byte request ID to be invalid")
	}
}

func TestValidRequestIDRejectsControlCharacters(t *testing.T) {
	if platformhttp.IsValidRequestID("abc\ndef") {
		t.Fatal("expected a request ID containing a newline to be invalid")
	}
	if platformhttp.IsValidRequestID("abc\x00def") {
		t.Fatal("expected a request ID containing a NUL byte to be invalid")
	}
	if platformhttp.IsValidRequestID("abc\tdef") {
		t.Fatal("expected a request ID containing a tab to be invalid")
	}
}

func TestValidRequestIDRejectsWhitespaceOnly(t *testing.T) {
	if platformhttp.IsValidRequestID("   ") {
		t.Fatal("expected a whitespace-only request ID to be invalid")
	}
}

func TestValidRequestIDRejectsNonASCII(t *testing.T) {
	if platformhttp.IsValidRequestID("café") {
		t.Fatal("expected a request ID containing non-ASCII bytes to be invalid")
	}
}

func TestResolveRequestIDReturnsSuppliedValidID(t *testing.T) {
	id := platformhttp.ResolveRequestID("client-supplied-id-123")
	if id != "client-supplied-id-123" {
		t.Fatalf("expected the supplied valid ID to be preserved, got %q", id)
	}
}

func TestResolveRequestIDGeneratesForInvalidID(t *testing.T) {
	id := platformhttp.ResolveRequestID("bad\nid")
	if id == "bad\nid" {
		t.Fatal("expected an invalid supplied ID to never be echoed back")
	}
	if !platformhttp.IsValidRequestID(id) {
		t.Fatalf("expected the generated replacement ID to itself be valid, got %q", id)
	}
}

func TestResolveRequestIDGeneratesForOversizedID(t *testing.T) {
	oversized := strings.Repeat("a", 200)
	id := platformhttp.ResolveRequestID(oversized)
	if id == oversized {
		t.Fatal("expected an oversized supplied ID to never be echoed back")
	}
	if !platformhttp.IsValidRequestID(id) {
		t.Fatalf("expected the generated replacement ID to itself be valid, got %q", id)
	}
}

func TestResolveRequestIDGeneratesForEmptyID(t *testing.T) {
	id := platformhttp.ResolveRequestID("")
	if id == "" {
		t.Fatal("expected an empty supplied ID to be replaced with a generated one")
	}
	if !platformhttp.IsValidRequestID(id) {
		t.Fatalf("expected the generated replacement ID to itself be valid, got %q", id)
	}
}

func TestResolveRequestIDGeneratesDifferentValuesEachCall(t *testing.T) {
	first := platformhttp.ResolveRequestID("")
	second := platformhttp.ResolveRequestID("")
	if first == second {
		t.Fatal("expected two generated request IDs to differ")
	}
}
