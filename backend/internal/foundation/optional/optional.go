// Package optional provides a tri-state JSON body field: a PATCH request
// needs to distinguish "the caller omitted this key" (leave unchanged) from
// "the caller explicitly sent null" (clear it) from "the caller sent a
// value" (set it) — three states a plain *string cannot represent, since
// encoding/json collapses "absent" and "null" to the same nil pointer.
package optional

import (
	"bytes"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
)

// NullableString is a JSON body field with three states:
//   - key absent from the JSON object: Present=false, Value=nil
//   - key present with value null: Present=true, Value=nil
//   - key present with a string value (including ""): Present=true, Value=&s
//
// encoding/json only invokes UnmarshalJSON when the key is present in the
// source object, so an absent key leaves NullableString at its zero value
// (Present=false) without any call into this type at all — that omission is
// the actual signal, not something this type has to detect itself.
type NullableString struct {
	Present bool
	Value   *string
}

var jsonNull = []byte("null")

// UnmarshalJSON implements json.Unmarshaler. Called only when the field's
// JSON key is present in the source object.
func (n *NullableString) UnmarshalJSON(data []byte) error {
	n.Present = true
	if bytes.Equal(bytes.TrimSpace(data), jsonNull) {
		n.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	n.Value = &s
	return nil
}

// MarshalJSON implements json.Marshaler, for symmetry (this type is not
// currently used in any response DTO, but round-tripping matters if that
// changes).
func (n NullableString) MarshalJSON() ([]byte, error) {
	if !n.Present || n.Value == nil {
		return jsonNull, nil
	}
	return json.Marshal(*n.Value)
}

// Schema implements huma.SchemaProvider so the OpenAPI document shows a
// plain nullable string for this field, not the {Present, Value} wrapper
// struct's own shape.
func (n *NullableString) Schema(_ huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Nullable: true}
}
