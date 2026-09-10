package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// fingerprintVersionPrefix marks every M8.5B-A input fingerprint. A future
// change to canonicalization or input scope must bump this rather than
// silently changing what an existing fingerprint means (design doc §7.2,
// "renovex-ai-input-v1").
const fingerprintVersionPrefix = "renovex-ai-input-v1:"

// FingerprintSpaceRef, FingerprintWorkItemRef, FingerprintMaterialRef, and
// FingerprintRequirementRef are the canonical, minimal identity+content
// projections a fingerprint is computed over — never the full domain
// struct, so unrelated field additions to Space/WorkItem/Material don't
// silently change every existing fingerprint.
type FingerprintSpaceRef struct {
	ID   string
	Name string
	Type string
}

type FingerprintWorkItemRef struct {
	ID          string
	SpaceID     string
	Description string
	WorkType    string
}

type FingerprintMaterialRef struct {
	ID   string
	Name string
}

type FingerprintRequirementRef struct {
	WorkItemID   string
	ResourceType string
	Name         string
}

// SpaceFingerprintInput is the authoritative input to Space generation
// (design doc §7.2): the project scope brief plus existing Space identities.
type SpaceFingerprintInput struct {
	ScopeBrief     string
	ExistingSpaces []FingerprintSpaceRef
}

// SpaceFingerprint computes the deterministic SHA-256 fingerprint of in,
// with ExistingSpaces canonically sorted by ID so caller-supplied slice
// order never affects the digest.
func SpaceFingerprint(in SpaceFingerprintInput) string {
	spaces := append([]FingerprintSpaceRef(nil), in.ExistingSpaces...)
	sort.Slice(spaces, func(i, j int) bool { return spaces[i].ID < spaces[j].ID })

	return fingerprint(struct {
		ScopeBrief     string
		ExistingSpaces []FingerprintSpaceRef
	}{ScopeBrief: in.ScopeBrief, ExistingSpaces: spaces})
}

// WorkFingerprintInput is the authoritative input to Work Item generation
// (design doc §7.2): scope brief, confirmed Space identities, and existing
// Work Items.
type WorkFingerprintInput struct {
	ScopeBrief        string
	ConfirmedSpaces   []FingerprintSpaceRef
	ExistingWorkItems []FingerprintWorkItemRef
}

// WorkFingerprint computes the deterministic SHA-256 fingerprint of in, with
// both slices canonically sorted by ID.
func WorkFingerprint(in WorkFingerprintInput) string {
	spaces := append([]FingerprintSpaceRef(nil), in.ConfirmedSpaces...)
	sort.Slice(spaces, func(i, j int) bool { return spaces[i].ID < spaces[j].ID })

	items := append([]FingerprintWorkItemRef(nil), in.ExistingWorkItems...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	return fingerprint(struct {
		ScopeBrief        string
		ConfirmedSpaces   []FingerprintSpaceRef
		ExistingWorkItems []FingerprintWorkItemRef
	}{ScopeBrief: in.ScopeBrief, ConfirmedSpaces: spaces, ExistingWorkItems: items})
}

// ResourceFingerprintInput is the authoritative input to Resource
// generation (design doc §7.2): confirmed Work Items, existing
// WorkResourceRequirements, and the supplied Material catalog candidate set.
type ResourceFingerprintInput struct {
	ConfirmedWorkItems   []FingerprintWorkItemRef
	ExistingRequirements []FingerprintRequirementRef
	MaterialCandidates   []FingerprintMaterialRef
}

// ResourceFingerprint computes the deterministic SHA-256 fingerprint of in,
// with all three slices canonically sorted by stable ID.
func ResourceFingerprint(in ResourceFingerprintInput) string {
	items := append([]FingerprintWorkItemRef(nil), in.ConfirmedWorkItems...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	reqs := append([]FingerprintRequirementRef(nil), in.ExistingRequirements...)
	sort.Slice(reqs, func(i, j int) bool {
		if reqs[i].WorkItemID != reqs[j].WorkItemID {
			return reqs[i].WorkItemID < reqs[j].WorkItemID
		}
		if reqs[i].ResourceType != reqs[j].ResourceType {
			return reqs[i].ResourceType < reqs[j].ResourceType
		}
		return reqs[i].Name < reqs[j].Name
	})

	candidates := append([]FingerprintMaterialRef(nil), in.MaterialCandidates...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

	return fingerprint(struct {
		ConfirmedWorkItems   []FingerprintWorkItemRef
		ExistingRequirements []FingerprintRequirementRef
		MaterialCandidates   []FingerprintMaterialRef
	}{ConfirmedWorkItems: items, ExistingRequirements: reqs, MaterialCandidates: candidates})
}

// fingerprint marshals canonical (a fixed-shape, already-sorted struct) as
// JSON and returns its versioned SHA-256 hex digest. Struct field
// declaration order fixes the JSON key order, so this never depends on map
// iteration order.
func fingerprint(canonical any) string {
	payload, err := json.Marshal(canonical)
	if err != nil {
		// The canonical shapes hold only JSON-infallible primitives/slices,
		// so this can only mean a shape itself was broken by an edit.
		panic("ai: fingerprint encoding failed: " + err.Error())
	}
	sum := sha256.Sum256(payload)
	return fingerprintVersionPrefix + hex.EncodeToString(sum[:])
}
