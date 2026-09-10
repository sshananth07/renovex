package materialrequirements_test

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// row builds a SourceRow for tests. Quantities are given as decimal strings so
// the tests exercise exactly what the production path receives.
func row(t *testing.T, costItemID, qty string) mr.SourceRow {
	t.Helper()
	d, err := decimal.NewFromString(qty)
	if err != nil {
		t.Fatalf("bad decimal %q: %v", qty, err)
	}
	return mr.SourceRow{CostItemID: costItemID, Quantity: d}
}

// --- Version tags (design spec §3.3, §5.2) ---

// The version tag must be the FIRST field of each canonical encoding, so a
// future encoding change can never be silently compared against v1 output.
func TestEncodingVersionTagsAreDistinctConstants(t *testing.T) {
	if mr.SourceFingerprintVersionTag != "mrq-source-fingerprint-v1" {
		t.Errorf("fingerprint version tag = %q, want %q",
			mr.SourceFingerprintVersionTag, "mrq-source-fingerprint-v1")
	}
	if mr.SourceAggregationKeyVersionTag != "mrq-source-key-v1" {
		t.Errorf("aggregation key version tag = %q, want %q",
			mr.SourceAggregationKeyVersionTag, "mrq-source-key-v1")
	}
	if mr.SourceFingerprintVersionTag == mr.SourceAggregationKeyVersionTag {
		t.Fatal("the two encodings must not share a version tag")
	}
}

// A fingerprint and an aggregation key built over the same identity must never
// collide, because the version tag differs.
func TestFingerprintAndAggregationKeyNeverCollide(t *testing.T) {
	fp := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", nil)
	key := mr.ComputeSourceAggregationKey("c1", "p1", mr.WorkItemRef("w1"), "m1", "bag")
	if fp == key {
		t.Fatal("fingerprint and aggregation key produced the same hash")
	}
}

// Both encodings must be hex-encoded SHA-256: 64 lowercase hex characters.
func TestEncodingsAreHexSHA256(t *testing.T) {
	const hexDigits = "0123456789abcdef"
	for name, got := range map[string]string{
		"fingerprint":    mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "5")}),
		"aggregationKey": mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "m1", "bag"),
	} {
		if len(got) != 64 {
			t.Errorf("%s: length = %d, want 64 (%q)", name, len(got), got)
		}
		if strings.ContainsFunc(got, func(r rune) bool { return !strings.ContainsRune(hexDigits, r) }) {
			t.Errorf("%s: %q contains non-lowercase-hex characters", name, got)
		}
	}
}

// --- Golden hashes: the regression lock (design spec §3.3, §5.2) ---

// These constants pin the exact v1 output. A silent encoding change — reordered
// fields, a dropped length prefix, an altered version tag — fails HERE rather
// than silently re-baselining every stored fingerprint in the database.
//
// If one of these fails, do NOT update the constant to match. Either revert the
// encoding change, or bump the version tag deliberately and re-pin (which is a
// documented, visible re-baseline per design spec §5.2).
// Pinned against the v1 encoding as of A1, which includes the explicit
// WorkItem presence marker (design spec §2.2). The tag remains v1 because this
// baseline predates any persisted data — no stored fingerprint exists to
// re-baseline. Once M7 ships, changing the encoding requires a tag bump.
const (
	goldenEmptyAggregateFingerprint = "53c58ae88ea6b55a1af354984488cf417d2afddcaf1b35580d43d2a45ef12057"
	goldenTwoRowFingerprint         = "cc7aa67ef4cf1f24f4a89c0523ffabb38e0f3ea929981a5e1d08243ce7bb29be"
	goldenAggregationKey            = "666b1ed536f8a16f79d34cce23bac46a3ade4fde161a0a7de628540532f3507d"
)

// Golden values for the ABSENT-WorkItem encodings, so a regression in the
// presence marker itself is caught and not merely the present-path hashes.
const (
	goldenAbsentWorkItemFingerprint    = "d93bda4981b9c06ab04548918ab52f6464a9874559dce070ae9bb809432c8159"
	goldenAbsentWorkItemAggregationKey = "f325f85fb3e1dc4460aa266cd92d0065e0e22badb4e7b7ddc91382d619475ecc"
)

func TestEncodingGoldenHashes(t *testing.T) {
	if got := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", nil); got != goldenEmptyAggregateFingerprint {
		t.Errorf("empty-aggregate fingerprint changed:\n got  %s\n want %s\n"+
			"Do not update this constant to match — see the comment above.", got, goldenEmptyAggregateFingerprint)
	}
	rows := []mr.SourceRow{row(t, "c1", "33.5"), row(t, "c2", "8")}
	if got := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", rows); got != goldenTwoRowFingerprint {
		t.Errorf("two-row fingerprint changed:\n got  %s\n want %s\n"+
			"Do not update this constant to match — see the comment above.", got, goldenTwoRowFingerprint)
	}
	if got := mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "m1", "bag"); got != goldenAggregationKey {
		t.Errorf("aggregation key changed:\n got  %s\n want %s\n"+
			"Do not update this constant to match — see the comment above.", got, goldenAggregationKey)
	}
	if got := mr.ComputeSourceFingerprint("p1", mr.NoWorkItemRef(), "m1", "bag", nil); got != goldenAbsentWorkItemFingerprint {
		t.Errorf("absent-WorkItem fingerprint changed:\n got  %s\n want %s\n"+
			"Do not update this constant to match — see the comment above.", got, goldenAbsentWorkItemFingerprint)
	}
	if got := mr.ComputeSourceAggregationKey("co1", "p1", mr.NoWorkItemRef(), "m1", "bag"); got != goldenAbsentWorkItemAggregationKey {
		t.Errorf("absent-WorkItem aggregation key changed:\n got  %s\n want %s\n"+
			"Do not update this constant to match — see the comment above.", got, goldenAbsentWorkItemAggregationKey)
	}
}

// --- Determinism and normalization (design spec §5.2) ---

func TestFingerprintIsDeterministic(t *testing.T) {
	rows := []mr.SourceRow{row(t, "c1", "33.5"), row(t, "c2", "8")}
	first := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "m2", rows)
	second := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "m2", rows)
	if first != second {
		t.Fatalf("not deterministic: %q vs %q", first, second)
	}
}

// decimal.String() normalizes equivalents, so 60 and 60.00 must hash alike.
func TestFingerprintNormalizesEquivalentDecimals(t *testing.T) {
	plain := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "60")})
	trailing := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "60.00")})
	if plain != trailing {
		t.Fatalf("60 and 60.00 must hash alike: %q vs %q", plain, trailing)
	}
}

// Rows are sorted ascending by CostItemID before encoding, so input order is
// irrelevant.
func TestFingerprintIsIndependentOfRowOrder(t *testing.T) {
	ascending := []mr.SourceRow{row(t, "c1", "10"), row(t, "c2", "20"), row(t, "c3", "30")}
	shuffled := []mr.SourceRow{row(t, "c3", "30"), row(t, "c1", "10"), row(t, "c2", "20")}
	if got, want := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", shuffled),
		mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", ascending); got != want {
		t.Fatalf("row order changed the fingerprint: %q vs %q", got, want)
	}
}

// An empty aggregate has a well-defined fingerprint distinct from any populated
// one. §3.4 and §5.3 depend on this: keep_current on source_removed accepts the
// empty-aggregate fingerprint and must then compare clean forever.
func TestEmptyAggregateFingerprintIsDefinedAndDistinct(t *testing.T) {
	empty := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", nil)
	emptySlice := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{})
	if empty == "" {
		t.Fatal("empty aggregate must still produce a fingerprint")
	}
	if empty != emptySlice {
		t.Errorf("nil and empty slice must hash alike: %q vs %q", empty, emptySlice)
	}
	populated := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "0")})
	if empty == populated {
		t.Fatal("empty aggregate must differ from a one-row aggregate of quantity 0")
	}
}

// --- Caller-owned data is never mutated ---

// Fingerprinting sorts rows internally. It must copy first: mutating a
// caller-owned slice would silently reorder a generation batch's working data.
func TestComputeSourceFingerprintDoesNotMutateCallerRows(t *testing.T) {
	rows := []mr.SourceRow{row(t, "c3", "30"), row(t, "c1", "10"), row(t, "c2", "20")}
	before := make([]string, len(rows))
	for i, r := range rows {
		before[i] = r.CostItemID
	}

	mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", rows)

	for i, r := range rows {
		if r.CostItemID != before[i] {
			t.Fatalf("caller slice was reordered: index %d is %q, was %q (want the input order preserved)",
				i, r.CostItemID, before[i])
		}
	}
}

// --- WorkItem presence is explicit (design spec §2.2) ---

// MaterialRequirement.WorkItemID is *string: nil for manual project-level
// demand. An absent Work Item must therefore encode differently from a present
// one whose ID happens to be the empty string, or the two aggregation
// identities would collide on one unique-index key.
func TestWorkItemAbsentDiffersFromPresentEmpty(t *testing.T) {
	absentFP := mr.ComputeSourceFingerprint("p1", mr.NoWorkItemRef(), "m1", "bag", nil)
	presentEmptyFP := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef(""), "m1", "bag", nil)
	if absentFP == presentEmptyFP {
		t.Error("fingerprint: absent WorkItem collided with present-but-empty WorkItem")
	}

	absentKey := mr.ComputeSourceAggregationKey("co1", "p1", mr.NoWorkItemRef(), "m1", "bag")
	presentEmptyKey := mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef(""), "m1", "bag")
	if absentKey == presentEmptyKey {
		t.Error("aggregation key: absent WorkItem collided with present-but-empty WorkItem")
	}
}

// A present Work Item must not collide with an absent one either, and the
// presence marker must not be forgeable from ID content alone.
func TestWorkItemPresenceMarkerIsNotForgeable(t *testing.T) {
	absent := mr.ComputeSourceAggregationKey("co1", "p1", mr.NoWorkItemRef(), "m1", "bag")
	for _, id := range []string{"", "0", "1", "nil", "null", "absent", "present"} {
		if got := mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef(id), "m1", "bag"); got == absent {
			t.Errorf("present WorkItemID %q collided with the absent marker", id)
		}
	}
}

// WorkItemRef round-trips through a *string, which is what the domain model
// holds, so callers never hand-roll the presence marker.
func TestWorkItemRefFromPointer(t *testing.T) {
	id := "w1"
	fromPtr := mr.WorkItemRefFromPointer(&id)
	if fromPtr != mr.WorkItemRef("w1") {
		t.Error("WorkItemRefFromPointer(&\"w1\") must equal WorkItemRef(\"w1\")")
	}
	if mr.WorkItemRefFromPointer(nil) != mr.NoWorkItemRef() {
		t.Error("WorkItemRefFromPointer(nil) must equal NoWorkItemRef()")
	}
	empty := ""
	if mr.WorkItemRefFromPointer(&empty) == mr.NoWorkItemRef() {
		t.Error("WorkItemRefFromPointer(&\"\") must NOT equal NoWorkItemRef()")
	}
}

// --- Injectivity: length-prefixed encoding is unambiguous (design spec §5.2) ---

// A unit string may legitimately contain a delimiter character. Length-prefixed
// encoding must keep such values from colliding with a different field split.
func TestFingerprintUnitContainingDelimitersDoesNotCollide(t *testing.T) {
	for _, tc := range []struct{ name, unitA, unitB string }{
		{"pipe", "bag|x", "bag"},
		{"newline", "bag\nx", "bag"},
		{"nul", "bag\x00x", "bag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", tc.unitA, nil)
			b := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", tc.unitB, nil)
			if a == b {
				t.Fatalf("units %q and %q collided", tc.unitA, tc.unitB)
			}
		})
	}
}

// Shifting a character across an adjacent field boundary must change the hash.
// Plain concatenation would make these pairs identical.
func TestFingerprintFieldBoundariesAreUnambiguous(t *testing.T) {
	a := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", nil)
	b := mr.ComputeSourceFingerprint("p", mr.WorkItemRef("1w1"), "m1", "bag", nil)
	if a == b {
		t.Fatal("field boundary shift produced an identical fingerprint")
	}
	c := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m", "1bag", nil)
	if a == c {
		t.Fatal("field boundary shift across materialID/unit produced an identical fingerprint")
	}
}

// Every identity field must participate: changing any one changes the hash.
func TestFingerprintDependsOnEveryIdentityField(t *testing.T) {
	base := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "5")})
	for name, got := range map[string]string{
		"projectID":  mr.ComputeSourceFingerprint("pX", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "5")}),
		"workItemID": mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("wX"), "m1", "bag", []mr.SourceRow{row(t, "c1", "5")}),
		"materialID": mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "mX", "bag", []mr.SourceRow{row(t, "c1", "5")}),
		"unit":       mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "kg", []mr.SourceRow{row(t, "c1", "5")}),
		"costItemID": mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "cX", "5")}),
		"quantity":   mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "6")}),
	} {
		if got == base {
			t.Errorf("changing %s did not change the fingerprint", name)
		}
	}
}

// Row count is encoded, so a repeated CostItemID cannot masquerade as one row.
func TestFingerprintEncodesRowCount(t *testing.T) {
	one := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag", []mr.SourceRow{row(t, "c1", "5")})
	two := mr.ComputeSourceFingerprint("p1", mr.WorkItemRef("w1"), "m1", "bag",
		[]mr.SourceRow{row(t, "c1", "5"), row(t, "c1", "5")})
	if one == two {
		t.Fatal("row count must participate in the fingerprint")
	}
}

// --- Aggregation key (design spec §3.3) ---

func TestAggregationKeyIsDeterministicAndTenantScoped(t *testing.T) {
	first := mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "m1", "bag")
	if second := mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "m1", "bag"); first != second {
		t.Fatalf("not deterministic: %q vs %q", first, second)
	}
	// companyID participates: this key backs a unique index, so a cross-tenant
	// collision would be a tenant-isolation failure.
	if other := mr.ComputeSourceAggregationKey("co2", "p1", mr.WorkItemRef("w1"), "m1", "bag"); first == other {
		t.Fatal("companyID must participate in the aggregation key")
	}
}

func TestAggregationKeyDependsOnEveryField(t *testing.T) {
	base := mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "m1", "bag")
	for name, got := range map[string]string{
		"companyID":  mr.ComputeSourceAggregationKey("coX", "p1", mr.WorkItemRef("w1"), "m1", "bag"),
		"projectID":  mr.ComputeSourceAggregationKey("co1", "pX", mr.WorkItemRef("w1"), "m1", "bag"),
		"workItemID": mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("wX"), "m1", "bag"),
		"materialID": mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "mX", "bag"),
		"unit":       mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "m1", "kg"),
	} {
		if got == base {
			t.Errorf("changing %s did not change the aggregation key", name)
		}
	}
}

func TestAggregationKeyFieldBoundariesAreUnambiguous(t *testing.T) {
	a := mr.ComputeSourceAggregationKey("co1", "p1", mr.WorkItemRef("w1"), "m1", "bag")
	b := mr.ComputeSourceAggregationKey("co", "1p1", mr.WorkItemRef("w1"), "m1", "bag")
	if a == b {
		t.Fatal("field boundary shift produced an identical aggregation key")
	}
}
