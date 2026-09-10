package materialrequirements

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/shopspring/decimal"
)

// Version tags. Each canonical encoding writes its tag as the FIRST field, so a
// future encoding change produces wholly different hashes and a v1 value can
// never be silently compared against a v2 one and read as "clean" (design spec
// §3.3, §5.2).
//
// Changing either constant is a deliberate, visible re-baseline: every stored
// value mismatches at once and surfaces as a discrepancy for contractor
// resolution. That is the intended behaviour — never edit a tag to make a
// comparison pass.
const (
	SourceFingerprintVersionTag    = "mrq-source-fingerprint-v1"
	SourceAggregationKeyVersionTag = "mrq-source-key-v1"
)

// SourceRow is one eligible material CostItem contributing to an aggregate.
// It is the unit the costs capability yields (design spec §1.2) and the unit
// the fingerprint encodes.
type SourceRow struct {
	CostItemID string
	Quantity   decimal.Decimal
}

// Presence markers for the optional WorkItem field. They are single bytes that
// cannot appear in a length prefix position for any real ID, and they are
// encoded as their OWN length-prefixed field, so no WorkItemID value can forge
// one (design spec §2.2).
const (
	workItemAbsentMarker  = "0"
	workItemPresentMarker = "1"
)

// WorkItemIDRef is an explicitly-present-or-absent WorkItem reference.
//
// MaterialRequirement.WorkItemID is a *string: required for a cost_item anchor,
// nil for manual project-level demand (design spec §2.2). Passing a bare string
// would make an absent WorkItem indistinguishable from a present one whose ID is
// "" — two different aggregation identities collapsing onto one unique-index
// key. This type keeps presence explicit at every call site.
type WorkItemIDRef struct {
	id      string
	present bool
}

// WorkItemRef returns a reference to a PRESENT WorkItem with the given ID,
// including when id is the empty string.
func WorkItemRef(id string) WorkItemIDRef {
	return WorkItemIDRef{id: id, present: true}
}

// NoWorkItemRef returns a reference denoting an ABSENT WorkItem — project-level
// demand carrying no WorkItem at all.
func NoWorkItemRef() WorkItemIDRef {
	return WorkItemIDRef{}
}

// WorkItemRefFromPointer converts the domain model's *string directly, so
// callers never hand-roll the presence distinction.
func WorkItemRefFromPointer(id *string) WorkItemIDRef {
	if id == nil {
		return NoWorkItemRef()
	}
	return WorkItemRef(*id)
}

// Present reports whether a WorkItem is attached.
func (r WorkItemIDRef) Present() bool { return r.present }

// ID returns the WorkItem ID and whether one is present.
func (r WorkItemIDRef) ID() (string, bool) { return r.id, r.present }

// writeWorkItemRef encodes the reference as a presence marker field followed,
// only when present, by the ID field. Absent encodes as one field; present
// encodes as two. Since each is independently length-prefixed, no ID value can
// produce the absent encoding.
func (e *canonicalEncoder) writeWorkItemRef(r WorkItemIDRef) {
	if !r.present {
		e.writeField(workItemAbsentMarker)
		return
	}
	e.writeField(workItemPresentMarker)
	e.writeField(r.id)
}

// canonicalEncoder builds a length-prefixed byte stream. Each field is written
// as its byte length (uvarint) followed by its raw bytes, so no field value can
// be mistaken for a different field split. Delimiter concatenation would be
// ambiguous here: a unit string may legitimately contain '|' or a newline
// (design spec §5.2).
type canonicalEncoder struct {
	buf []byte
}

// writeField appends one length-prefixed field.
func (e *canonicalEncoder) writeField(s string) {
	e.buf = binary.AppendUvarint(e.buf, uint64(len(s)))
	e.buf = append(e.buf, s...)
}

// sum returns the hex-encoded SHA-256 of everything written so far.
func (e *canonicalEncoder) sum() string {
	digest := sha256.Sum256(e.buf)
	return hex.EncodeToString(digest[:])
}

// ComputeSourceFingerprint hashes the accepted-source identity plus its
// contributing rows (design spec §5.2). Field order is fixed:
//
//	version tag, projectID, workItemID, materialID, aggregationUnit,
//	row count, then per row (sorted ascending by CostItemID):
//	costItemID, canonical decimal quantity
//
// Rows are sorted before encoding, so caller order is irrelevant.
// decimal.String() normalizes equivalents, so "60" and "60.00" hash alike.
//
// A nil or empty rows slice yields a well-defined empty-aggregate fingerprint
// (row count 0), which §5.3 relies on: once keep_current accepts it, later
// detection runs compare clean instead of warning forever.
func ComputeSourceFingerprint(projectID string, workItem WorkItemIDRef, materialID, aggregationUnit string, rows []SourceRow) string {
	e := &canonicalEncoder{}
	e.writeField(SourceFingerprintVersionTag)
	e.writeField(projectID)
	e.writeWorkItemRef(workItem)
	e.writeField(materialID)
	e.writeField(aggregationUnit)

	// Copy before sorting: rows is caller-owned, and reordering a generation
	// batch's working slice as a side effect of hashing it would be a silent
	// mutation of data the caller still relies on.
	sorted := make([]SourceRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CostItemID < sorted[j].CostItemID })

	e.writeField(strconv.Itoa(len(sorted)))
	for _, r := range sorted {
		e.writeField(r.CostItemID)
		e.writeField(r.Quantity.String())
	}
	return e.sum()
}

// ComputeSourceAggregationKey hashes the immutable source aggregation identity
// (design spec §3.3). It deliberately does NOT include the contractor-editable
// RequiredQuantity.Unit — only aggregationUnit, the CostItem quantity unit
// captured at generation time.
//
// companyID participates because this key backs a unique index; without it a
// cross-tenant collision would be a tenant-isolation failure.
func ComputeSourceAggregationKey(companyID, projectID string, workItem WorkItemIDRef, materialID, aggregationUnit string) string {
	e := &canonicalEncoder{}
	e.writeField(SourceAggregationKeyVersionTag)
	e.writeField(companyID)
	e.writeField(projectID)
	e.writeWorkItemRef(workItem)
	e.writeField(materialID)
	e.writeField(aggregationUnit)
	return e.sum()
}
