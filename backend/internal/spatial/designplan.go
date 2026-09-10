package spatial

import (
	"errors"
	"regexp"
	"strings"
)

// SectionMode is shared by the geometry/material/spatial sections of both
// a proposed delta and the cumulative working design (RP4E1 plan: "Each
// section has mode = preserve|replace|clear; only replace carries a spec.
// This makes 'keep it,' 'instead,' and 'return to the original
// shape/material/position' deterministic.").
type SectionMode string

const (
	SectionModePreserve SectionMode = "preserve"
	SectionModeReplace  SectionMode = "replace"
	SectionModeClear    SectionMode = "clear"
)

// ErrInvalidSectionChange is returned when a SectionChange's Mode/spec
// pairing violates the mode/spec consistency invariant: replace REQUIRES a
// spec, preserve/clear MUST NOT carry one. Go re-enforces this
// independently of Python's own model_validator — the proposal is
// untrusted output from the provider's perspective, not just structurally
// valid JSON.
var ErrInvalidSectionChange = errors.New("spatial: invalid design section change (mode/spec mismatch)")

// WorkingDesignGeometry mirrors Python's GeometrySpec — the cumulative
// visual-geometry description accepted so far in a session.
type WorkingDesignGeometry struct {
	Category                    string `bson:"category" json:"category"`
	ShapeDescription            string `bson:"shapeDescription" json:"shapeDescription"`
	PreserveCanonicalDimensions bool   `bson:"preserveCanonicalDimensions" json:"preserveCanonicalDimensions"`
}

// WorkingDesignMaterial mirrors Python's MaterialSpec.
type WorkingDesignMaterial struct {
	BaseColor      string `bson:"baseColor" json:"baseColor"`
	MaterialFamily string `bson:"materialFamily" json:"materialFamily"`
	Roughness      string `bson:"roughness" json:"roughness"`
	Metallic       bool   `bson:"metallic" json:"metallic"`
}

// hexColorPattern is the single canonical sRGB #RRGGBB pattern shared by
// every base-color validation site in this package (RP4E2/M8.5C amendment,
// plan repository-findings row 4: "the renderer needs deterministic color
// data" — a free-form name like "dark green" is never trusted, even though
// Python already enforces the same pattern independently).
var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ErrInvalidBaseColor is returned when a MaterialSpec's baseColor is not a
// canonical six-digit sRGB hex code.
var ErrInvalidBaseColor = errors.New("spatial: baseColor must be a canonical #RRGGBB hex color")

// ValidateBaseColor checks color against the canonical #RRGGBB pattern and
// returns it normalized to lowercase (RP4E2 plan: "normalized to lowercase
// before fingerprinting"). Go re-validates independently of Python's own
// schema pattern — a structurally-valid-but-adversarial provider response is
// never trusted twice by the same code, matching this package's existing
// re-validation convention (ProposedSceneEditDelta.Validate).
func ValidateBaseColor(color string) (string, error) {
	if !hexColorPattern.MatchString(color) {
		return "", ErrInvalidBaseColor
	}
	return strings.ToLower(color), nil
}

// WorkingDesign is the session's cumulative accepted design state (RP4E1
// plan "SpatialDesignSession.CurrentWorkingDesign") — distinct from any one
// turn's LOCAL delta. Only a successfully validated turn ever replaces it
// (Task 7's service orchestration); a failed/blocked turn leaves it
// untouched.
type WorkingDesign struct {
	Geometry                  *WorkingDesignGeometry     `bson:"geometry,omitempty" json:"geometry,omitempty"`
	Material                  *WorkingDesignMaterial     `bson:"material,omitempty" json:"material,omitempty"`
	ResolvedSpatialOperations []ResolvedSpatialOperation `bson:"resolvedSpatialOperations" json:"resolvedSpatialOperations"`
}

// SpatialOperationKind is RP4E1's closed set of supported spatial
// operations — resize_axis and move_relative_to_nearest_wall are the only
// two the plan defines; anything else is a validated blocker, never a
// fabricated operation.
type SpatialOperationKind string

const (
	SpatialOpMoveRelativeToNearestWall SpatialOperationKind = "move_relative_to_nearest_wall"
	SpatialOpResizeAxis                SpatialOperationKind = "resize_axis"
)

// SpatialRelationship is move_relative_to_nearest_wall's direction.
type SpatialRelationship string

const (
	SpatialRelationshipAwayFrom SpatialRelationship = "away_from"
	SpatialRelationshipToward   SpatialRelationship = "toward"
)

// SpatialAxis is resize_axis's target axis.
type SpatialAxis string

const (
	SpatialAxisX SpatialAxis = "x"
	SpatialAxisY SpatialAxis = "y"
	SpatialAxisZ SpatialAxis = "z"
)

// ProposedSpatialSpec is the untrusted, Python-proposed spatial operation
// BEFORE Go resolves it to absolute coordinates. Exactly one of the two
// operation shapes is populated, discriminated by Kind — mirrors Python's
// closed discriminated union (MoveRelativeToNearestWallSpec |
// ResizeAxisSpec).
type ProposedSpatialSpec struct {
	Kind SpatialOperationKind

	// move_relative_to_nearest_wall fields.
	Relationship   SpatialRelationship
	DistanceMeters float64

	// resize_axis fields — exactly one of DeltaMeters/TargetMeters is set
	// (HasDelta/HasTarget discriminate, since 0.0 is a legal DeltaMeters).
	Axis         SpatialAxis
	DeltaMeters  float64
	HasDelta     bool
	TargetMeters float64
	HasTarget    bool
}

// ResolvedSpatialOperation is the Go-computed ABSOLUTE canonical operation
// payload (RP4E1 plan: "All resolved operations store absolute payloads
// matching existing Go types") — never applied to the RoomDraft in RP4E1,
// only stored on the cumulative WorkingDesign and referenced by the public
// plan DTO. Kind matches one of editoperation.go's existing
// EditOperationKind constants (move_object/move_fixture/resize_object/
// resize_fixture) so a FUTURE execution slice can apply it through the
// EXISTING EditOperation pipeline with no new operation vocabulary.
type ResolvedSpatialOperation struct {
	Kind    EditOperationKind `bson:"kind" json:"kind"`
	Payload map[string]any    `bson:"payload" json:"payload"`
}

// SectionChange mirrors one section (geometry|material|spatial) of
// Python's ProposedSceneEditDelta — the untrusted proposal Go revalidates
// before ever merging it into a WorkingDesign.
type SectionChange struct {
	Mode SectionMode

	GeometrySpec *WorkingDesignGeometry
	MaterialSpec *WorkingDesignMaterial
	SpatialSpec  *ProposedSpatialSpec
}

// Validate enforces the mode/spec consistency invariant independently of
// Python's own validation — replace requires exactly one non-nil spec
// field (matching this section's own kind); preserve/clear forbid any.
func (s SectionChange) validate(specPresent bool) error {
	switch s.Mode {
	case SectionModeReplace:
		if !specPresent {
			return ErrInvalidSectionChange
		}
	case SectionModePreserve, SectionModeClear:
		if specPresent {
			return ErrInvalidSectionChange
		}
	default:
		return ErrInvalidSectionChange
	}
	return nil
}

// ProposedBlocker mirrors Python's ProposedBlocker.
type ProposedBlocker struct {
	Code    string `bson:"code" json:"code"`
	Message string `bson:"message" json:"message"`
}

// DesignIntent mirrors Python's intent discriminator.
type DesignIntent string

const (
	DesignIntentVisualGeometry     DesignIntent = "visual_geometry"
	DesignIntentMaterialAppearance DesignIntent = "material_appearance"
	DesignIntentSpatialDomain      DesignIntent = "spatial_domain"
	DesignIntentMixed              DesignIntent = "mixed"
)

// ProposedSceneEditDelta is the Go-side re-validated mirror of Python's
// ProposedSceneEditDelta — untrusted provider output, never returned to a
// client and never merged without passing Validate() below.
type ProposedSceneEditDelta struct {
	Target      SpatialDesignTarget
	Intent      DesignIntent
	Summary     []string
	Geometry    SectionChange
	Material    SectionChange
	Spatial     SectionChange
	Blockers    []ProposedBlocker
	Assumptions []string
	ReviewNotes []string
	Confidence  float64
}

// Validate re-enforces the delta's own internal consistency (mode/spec
// pairing per section, intent/section-replacement consistency) — the SAME
// invariants Python's schema enforces, checked again in Go since a
// provider's structurally-valid-but-adversarial-shaped output is not
// trusted twice by the same code.
func (d ProposedSceneEditDelta) Validate() error {
	if err := d.Geometry.validate(d.Geometry.GeometrySpec != nil); err != nil {
		return err
	}
	if err := d.Material.validate(d.Material.MaterialSpec != nil); err != nil {
		return err
	}
	if d.Material.Mode == SectionModeReplace && d.Material.MaterialSpec != nil {
		normalized, err := ValidateBaseColor(d.Material.MaterialSpec.BaseColor)
		if err != nil {
			return err
		}
		d.Material.MaterialSpec.BaseColor = normalized
	}
	if err := d.Spatial.validate(d.Spatial.SpatialSpec != nil); err != nil {
		return err
	}

	replaced := map[string]bool{
		"geometry": d.Geometry.Mode == SectionModeReplace,
		"material": d.Material.Mode == SectionModeReplace,
		"spatial":  d.Spatial.Mode == SectionModeReplace,
	}
	singleSection := map[DesignIntent]string{
		DesignIntentVisualGeometry:     "geometry",
		DesignIntentMaterialAppearance: "material",
		DesignIntentSpatialDomain:      "spatial",
	}
	if only, ok := singleSection[d.Intent]; ok {
		for section, isReplaced := range replaced {
			if section != only && isReplaced {
				return ErrInvalidSectionChange
			}
		}
	}
	return nil
}

// mergeWorkingDesign applies delta's three sections onto previous,
// returning the new cumulative WorkingDesign. delta is validated first
// (ErrInvalidSectionChange on a mode/spec mismatch) — mergeWorkingDesign
// never partially applies an invalid delta.
//
// This function has no notion of "failed turn" — the caller (Task 7's
// design service) is responsible for only invoking this for a
// successfully-validated, non-blocked delta; a failed/blocked turn simply
// never calls merge, leaving the session's stored WorkingDesign untouched.
func mergeWorkingDesign(previous WorkingDesign, delta ProposedSceneEditDelta) (WorkingDesign, error) {
	if err := delta.Validate(); err != nil {
		return WorkingDesign{}, err
	}

	merged := WorkingDesign{
		Geometry:                  previous.Geometry,
		Material:                  previous.Material,
		ResolvedSpatialOperations: previous.ResolvedSpatialOperations,
	}

	switch delta.Geometry.Mode {
	case SectionModeReplace:
		merged.Geometry = delta.Geometry.GeometrySpec
	case SectionModeClear:
		merged.Geometry = nil
	}

	switch delta.Material.Mode {
	case SectionModeReplace:
		merged.Material = delta.Material.MaterialSpec
	case SectionModeClear:
		merged.Material = nil
	}

	// Spatial resolution (turning delta.Spatial's semantic spec into an
	// absolute ResolvedSpatialOperation) is designgeometry.go's
	// resolveSpatialChanges, not this function — mergeWorkingDesign only
	// merges the OTHER two sections' cumulative state and leaves
	// ResolvedSpatialOperations as the caller supplies it (the caller
	// calls resolveSpatialChanges separately and assigns the result before
	// persisting, per Task 7's documented service ordering).

	return merged, nil
}
