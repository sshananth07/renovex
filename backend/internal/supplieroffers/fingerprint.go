package supplieroffers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// CommercialFingerprintV1 computes the M8.1 amendment's exact, versioned
// commercial identity of one immutable RFQ line snapshot (spec §7.1
// amendment). Copy-forward treats two snapshots as commercially identical
// only when their fingerprints match; the canonicalization below intentionally
// discards everything that is NOT a commercial input — whitespace shape, line
// endings, Unicode normalization form, display order, timestamps and database
// revision metadata — while preserving case, punctuation and exact decimal
// values.
//
// Changing these inputs or the canonicalization rules requires a new version
// rather than silently altering V1's digest for existing callers.
func CommercialFingerprintV1(line IssuedRFQLineSnapshot) string {
	canonical := canonicalFingerprintLine{
		Version:                     "rfq-line-commercial-v1",
		MaterialID:                  canonicalText(line.MaterialID),
		MaterialName:                canonicalText(line.MaterialName),
		Specification:               canonicalText(line.Specification),
		QuantityValue:               line.Quantity.Value.String(),
		QuantityUnit:                canonicalText(line.Quantity.Unit),
		SourceMaterialRequirementID: canonicalOptionalString(line.SourceMaterialRequirementID),
		RequiredByDate:              canonicalOptionalTime(line.RequiredByDate),
		ProcurementNotes:            canonicalText(line.ProcurementNotes),
	}

	payload, err := json.Marshal(canonical)
	if err != nil {
		// The canonical shape holds only JSON-infallible primitives, so this
		// can only mean the shape itself was broken by an edit.
		panic("supplieroffers: commercial fingerprint v1 encoding failed: " + err.Error())
	}
	sum := sha256.Sum256(payload)
	return "v1:" + hex.EncodeToString(sum[:])
}

// canonicalFingerprintLine is the fixed-field-order, JSON-serialized shape the
// digest is computed over. Field order in the struct IS the serialization
// order: Go's encoding/json marshals struct fields in declaration order.
type canonicalFingerprintLine struct {
	Version                     string  `json:"version"`
	MaterialID                  string  `json:"materialId"`
	MaterialName                string  `json:"materialName"`
	Specification               string  `json:"specification"`
	QuantityValue               string  `json:"quantityValue"`
	QuantityUnit                string  `json:"quantityUnit"`
	SourceMaterialRequirementID *string `json:"sourceMaterialRequirementId"`
	RequiredByDate              *string `json:"requiredByDate"`
	ProcurementNotes            string  `json:"procurementNotes"`
}

// canonicalText applies the shared text canonicalization: NFC Unicode
// normalization, LF line endings, trimmed outer whitespace and collapsed
// internal whitespace. Case and punctuation are deliberately left untouched.
func canonicalText(value string) string {
	normalized := norm.NFC.String(value)
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	fields := strings.Fields(normalized)
	return strings.Join(fields, " ")
}

// canonicalOptionalString encodes an absent association distinctly from an
// empty one: nil becomes a nil JSON field, "" becomes an explicit empty
// string, so the two can never collide in the digest.
func canonicalOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	canonical := canonicalText(*value)
	return &canonical
}

// canonicalOptionalTime discards timezone-presentation and sub-second
// formatting differences that are not commercial content, while still
// distinguishing absent from any concrete instant.
func canonicalOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	canonical := value.UTC().Format(time.RFC3339)
	return &canonical
}
