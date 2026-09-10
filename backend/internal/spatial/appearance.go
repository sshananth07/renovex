package spatial

import "errors"

// MaterialFamily is VisualAppearance's closed material-family vocabulary
// (RP4E2/M8.5C plan: "Add optional VisualAppearance to objects and fixtures
// plus set_visual_appearance and clear_visual_appearance operations").
type MaterialFamily string

const (
	MaterialFamilyFabric  MaterialFamily = "fabric"
	MaterialFamilyLeather MaterialFamily = "leather"
	MaterialFamilyWood    MaterialFamily = "wood"
	MaterialFamilyMetal   MaterialFamily = "metal"
	MaterialFamilyStone   MaterialFamily = "stone"
	MaterialFamilyOther   MaterialFamily = "other"
)

func isValidMaterialFamily(f MaterialFamily) bool {
	switch f {
	case MaterialFamilyFabric, MaterialFamilyLeather, MaterialFamilyWood, MaterialFamilyMetal, MaterialFamilyStone, MaterialFamilyOther:
		return true
	default:
		return false
	}
}

// Roughness is VisualAppearance's closed renderer-facing roughness
// vocabulary — the Web renderer maps these to concrete roughness scalars
// (matte->0.82, satin->0.48, glossy->0.18 per the RP4E3 renderer
// integration plan); this package stores only the semantic label.
type Roughness string

const (
	RoughnessMatte  Roughness = "matte"
	RoughnessSatin  Roughness = "satin"
	RoughnessGlossy Roughness = "glossy"
)

func isValidRoughness(r Roughness) bool {
	switch r {
	case RoughnessMatte, RoughnessSatin, RoughnessGlossy:
		return true
	default:
		return false
	}
}

// ErrInvalidVisualAppearance is returned when a VisualAppearance fails its
// closed validation (non-canonical color, unknown family, unknown
// roughness).
var ErrInvalidVisualAppearance = errors.New("spatial: invalid visual appearance")

// VisualAppearance is a fixture/object's optional material-only design
// override (RP4E2/M8.5C amendment) — persisted directly on RoomDraftObject/
// RoomDraftFixture, never in a separate materials collection or PBR asset
// pipeline (plan's explicit scope guard). Validation is closed and shared
// by both canonical operations (set_visual_appearance) and design
// confirmation (Gate 0/1's WorkingDesignMaterial normalization uses the
// same ValidateBaseColor) — the same invariant enforced at every entry
// point, never reimplemented per-caller.
type VisualAppearance struct {
	BaseColor      string         `bson:"baseColor" json:"baseColor"`
	MaterialFamily MaterialFamily `bson:"materialFamily" json:"materialFamily"`
	Roughness      Roughness      `bson:"roughness" json:"roughness"`
	Metallic       bool           `bson:"metallic" json:"metallic"`
}

// Validate enforces VisualAppearance's closed shape and normalizes
// BaseColor to lowercase in place — mirrors AssignVisualAssetOperation's
// "local/structural validation only" convention; VisualAppearance never
// references external state, so there is no separate authorization check.
func (a *VisualAppearance) Validate() error {
	normalized, err := ValidateBaseColor(a.BaseColor)
	if err != nil {
		return ErrInvalidVisualAppearance
	}
	if !isValidMaterialFamily(a.MaterialFamily) {
		return ErrInvalidVisualAppearance
	}
	if !isValidRoughness(a.Roughness) {
		return ErrInvalidVisualAppearance
	}
	a.BaseColor = normalized
	return nil
}
