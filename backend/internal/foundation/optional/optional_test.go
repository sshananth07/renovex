package optional_test

import (
	"encoding/json"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/optional"
)

type wrapper struct {
	Field optional.NullableString `json:"field"`
}

func TestNullableStringOmittedKeyLeavesUnset(t *testing.T) {
	var w wrapper
	if err := json.Unmarshal([]byte(`{}`), &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Field.Present {
		t.Fatal("expected Present=false when the key is absent")
	}
	if w.Field.Value != nil {
		t.Fatal("expected Value=nil when the key is absent")
	}
}

func TestNullableStringExplicitNullSetsPresentWithNilValue(t *testing.T) {
	var w wrapper
	if err := json.Unmarshal([]byte(`{"field": null}`), &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !w.Field.Present {
		t.Fatal("expected Present=true when the key is explicitly null")
	}
	if w.Field.Value != nil {
		t.Fatal("expected Value=nil for an explicit null")
	}
}

func TestNullableStringExplicitValueSetsPresentWithValue(t *testing.T) {
	var w wrapper
	if err := json.Unmarshal([]byte(`{"field": "hello"}`), &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !w.Field.Present {
		t.Fatal("expected Present=true when the key has a value")
	}
	if w.Field.Value == nil || *w.Field.Value != "hello" {
		t.Fatalf("expected Value=hello, got %v", w.Field.Value)
	}
}

func TestNullableStringExplicitEmptyStringSetsPresentWithEmptyValue(t *testing.T) {
	var w wrapper
	if err := json.Unmarshal([]byte(`{"field": ""}`), &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !w.Field.Present {
		t.Fatal("expected Present=true for an explicit empty string")
	}
	if w.Field.Value == nil || *w.Field.Value != "" {
		t.Fatalf("expected Value=\"\", got %v", w.Field.Value)
	}
}

func TestNullableStringRejectsNonStringNonNullValue(t *testing.T) {
	var w wrapper
	if err := json.Unmarshal([]byte(`{"field": 123}`), &w); err == nil {
		t.Fatal("expected an error for a non-string, non-null value")
	}
}

func TestNullableStringSchemaIsStringOrNull(t *testing.T) {
	schema := (&optional.NullableString{}).Schema(nil)
	if schema == nil {
		t.Fatal("expected a non-nil schema")
	}
	if schema.Type != "string" {
		t.Fatalf("Type = %q, want string", schema.Type)
	}
	if !schema.Nullable {
		t.Fatal("expected Nullable=true")
	}
}
