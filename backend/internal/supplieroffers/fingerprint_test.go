package supplieroffers

import (
	"testing"
	"time"
)

func fingerprintLine(t *testing.T) IssuedRFQLineSnapshot {
	t.Helper()
	due := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	return IssuedRFQLineSnapshot{
		ID:                          "issued-line-1",
		LineageID:                   "lineage-1",
		SourceMaterialRequirementID: materialRequirement("mr-1"),
		MaterialID:                  "material-1",
		MaterialName:                "Ceramic tile",
		Specification:               "300x300, matte finish",
		Quantity:                    qty(t, "10.500", "m2"),
		RequiredByDate:              &due,
		ProcurementNotes:            "deliver to loading dock B",
		SortOrder:                   3,
	}
}

// Fingerprints must be stable across everything that is NOT a commercial
// input: whitespace shape, line endings, Unicode normalization form, display
// order and database bookkeeping.
func TestCommercialFingerprintV1StableAcrossNonCommercialVariation(t *testing.T) {
	base := fingerprintLine(t)
	baseFingerprint := CommercialFingerprintV1(base)

	cases := map[string]IssuedRFQLineSnapshot{
		"surrounding whitespace": withSpecification(base, "  300x300, matte finish  "),
		"repeated internal whitespace": withSpecification(base,
			"300x300,   matte    finish"),
		"CRLF vs LF":         withSpecification(base, "300x300,\r\nmatte finish"),
		"sort order differs": withSortOrder(base, 99),
	}

	for name, variant := range cases {
		t.Run(name, func(t *testing.T) {
			got := CommercialFingerprintV1(variant)
			if got != baseFingerprint {
				t.Errorf("fingerprint changed for %s: got %q, want %q (base)",
					name, got, baseFingerprint)
			}
		})
	}
}

func withSpecification(line IssuedRFQLineSnapshot, spec string) IssuedRFQLineSnapshot {
	line.Specification = spec
	return line
}

func withSortOrder(line IssuedRFQLineSnapshot, order int) IssuedRFQLineSnapshot {
	line.SortOrder = order
	return line
}

// Fingerprints must differ whenever a genuinely commercial input differs.
func TestCommercialFingerprintV1DiffersOnCommercialChange(t *testing.T) {
	base := fingerprintLine(t)
	baseFingerprint := CommercialFingerprintV1(base)

	laterDue := base.RequiredByDate.Add(24 * time.Hour)
	otherMaterial := materialRequirement("mr-2")

	cases := map[string]IssuedRFQLineSnapshot{
		"different specification": withSpecification(base, "600x600, gloss finish"),
		"different quantity value": func() IssuedRFQLineSnapshot {
			l := base
			l.Quantity = qty(t, "11.000", "m2")
			return l
		}(),
		"different unit": func() IssuedRFQLineSnapshot {
			l := base
			l.Quantity = qty(t, "10.500", "ea")
			return l
		}(),
		"different material name": func() IssuedRFQLineSnapshot {
			l := base
			l.MaterialName = "Porcelain tile"
			return l
		}(),
		"different required-by date": func() IssuedRFQLineSnapshot {
			l := base
			l.RequiredByDate = &laterDue
			return l
		}(),
		"different procurement notes (delivery constraint)": func() IssuedRFQLineSnapshot {
			l := base
			l.ProcurementNotes = "deliver to loading dock C"
			return l
		}(),
		"different source material requirement association": func() IssuedRFQLineSnapshot {
			l := base
			l.SourceMaterialRequirementID = otherMaterial
			return l
		}(),
	}

	for name, variant := range cases {
		t.Run(name, func(t *testing.T) {
			got := CommercialFingerprintV1(variant)
			if got == baseFingerprint {
				t.Errorf("fingerprint unchanged for %s, want it to differ", name)
			}
		})
	}
}

// Case and punctuation are preserved verbatim, never normalized away.
func TestCommercialFingerprintV1PreservesCaseAndPunctuation(t *testing.T) {
	lower := withSpecification(fingerprintLine(t), "matte finish, grade a")
	upper := withSpecification(fingerprintLine(t), "MATTE FINISH, GRADE A")
	if CommercialFingerprintV1(lower) == CommercialFingerprintV1(upper) {
		t.Error("fingerprint ignored case; case must be preserved")
	}

	withComma := withSpecification(fingerprintLine(t), "300x300, matte finish")
	withoutComma := withSpecification(fingerprintLine(t), "300x300 matte finish")
	if CommercialFingerprintV1(withComma) == CommercialFingerprintV1(withoutComma) {
		t.Error("fingerprint ignored punctuation; punctuation must be preserved")
	}
}

// Absent and empty optional associations must encode deterministically and
// distinctly from each other, never colliding by accident.
func TestCommercialFingerprintV1EncodesOptionalAssociationsDeterministically(t *testing.T) {
	absent := fingerprintLine(t)
	absent.SourceMaterialRequirementID = nil
	absent.RequiredByDate = nil

	empty := fingerprintLine(t)
	emptyID := ""
	empty.SourceMaterialRequirementID = &emptyID

	if CommercialFingerprintV1(absent) == CommercialFingerprintV1(empty) {
		t.Error("absent association must not collide with an empty-string association")
	}

	// Determinism: computing twice from the same absent-association input
	// yields the same digest.
	if CommercialFingerprintV1(absent) != CommercialFingerprintV1(absent) {
		t.Error("fingerprint is not deterministic for identical input")
	}
}

// The fingerprint is versioned and uses exact-decimal quantity encoding, not
// float conversion.
func TestCommercialFingerprintV1VersionPrefixAndExactDecimal(t *testing.T) {
	line := fingerprintLine(t)
	got := CommercialFingerprintV1(line)
	const prefix = "v1:"
	if len(got) <= len(prefix) || got[:len(prefix)] != prefix {
		t.Fatalf("fingerprint = %q, want v1: prefix", got)
	}

	// A quantity value that would lose precision under float64 conversion
	// (e.g. 0.1 + 0.2 style drift) must still round-trip exactly and produce a
	// stable, reproducible fingerprint distinct from a naively-rounded value.
	precise := fingerprintLine(t)
	precise.Quantity = qty(t, "10.10", "m2")
	roundedDifferently := fingerprintLine(t)
	roundedDifferently.Quantity = qty(t, "10.1", "m2")

	// "10.10" and "10.1" are the exact same decimal value; the canonical
	// exact-decimal encoding must treat them identically (decimal-equal, not
	// string-equal), so the fingerprint must match.
	if CommercialFingerprintV1(precise) != CommercialFingerprintV1(roundedDifferently) {
		t.Error("fingerprint must compare quantity by exact decimal value, not raw string")
	}
}
