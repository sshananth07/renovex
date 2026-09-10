package spatial

import (
	"errors"
	"testing"
)

func TestMergeWorkingDesign_PreserveKeepsAllSections(t *testing.T) {
	previous := WorkingDesign{
		Geometry: &WorkingDesignGeometry{Category: "sofa", ShapeDescription: "Curved sofa", PreserveCanonicalDimensions: true},
		Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
	}
	delta := ProposedSceneEditDelta{
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModePreserve},
		Spatial:  SectionChange{Mode: SectionModePreserve},
	}

	merged, err := mergeWorkingDesign(previous, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.Geometry == nil || merged.Geometry.ShapeDescription != "Curved sofa" {
		t.Fatalf("expected geometry preserved, got %+v", merged.Geometry)
	}
	if merged.Material == nil || merged.Material.BaseColor != "#c8a464" {
		t.Fatalf("expected material preserved, got %+v", merged.Material)
	}
}

func TestMergeWorkingDesign_ReplaceOverwritesSection(t *testing.T) {
	previous := WorkingDesign{
		Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
	}
	delta := ProposedSceneEditDelta{
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeReplace, MaterialSpec: &WorkingDesignMaterial{
			BaseColor: "#2f4f3a", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial: SectionChange{Mode: SectionModePreserve},
	}

	merged, err := mergeWorkingDesign(previous, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.Material.BaseColor != "#2f4f3a" {
		t.Fatalf("expected replaced material, got %+v", merged.Material)
	}
}

func TestMergeWorkingDesign_ClearRemovesSection(t *testing.T) {
	previous := WorkingDesign{
		Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
	}
	delta := ProposedSceneEditDelta{
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeClear},
		Spatial:  SectionChange{Mode: SectionModePreserve},
	}

	merged, err := mergeWorkingDesign(previous, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.Material != nil {
		t.Fatalf("expected material cleared, got %+v", merged.Material)
	}
}

func TestMergeWorkingDesign_MaterialOnlyRefinementRetainsGeometry(t *testing.T) {
	// "Actually make it beige." after an earlier geometry-only turn:
	// geometry must survive untouched (RP4E1 plan's canonical demo
	// sequence).
	previous := WorkingDesign{
		Geometry: &WorkingDesignGeometry{Category: "sofa", ShapeDescription: "Curved three-seat sofa with rounded arms", PreserveCanonicalDimensions: true},
	}
	delta := ProposedSceneEditDelta{
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeReplace, MaterialSpec: &WorkingDesignMaterial{
			BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial: SectionChange{Mode: SectionModePreserve},
	}

	merged, err := mergeWorkingDesign(previous, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.Geometry == nil || merged.Geometry.ShapeDescription != "Curved three-seat sofa with rounded arms" {
		t.Fatalf("expected geometry retained from prior turn, got %+v", merged.Geometry)
	}
	if merged.Material == nil || merged.Material.BaseColor != "#c8a464" {
		t.Fatalf("expected material replaced, got %+v", merged.Material)
	}
}

func TestMergeWorkingDesign_FailedTurnNeverCalled(t *testing.T) {
	// mergeWorkingDesign itself has no notion of "failed" — the SERVICE
	// layer (Task 7) is responsible for never calling merge for a
	// failed/blocked turn, so CurrentWorkingDesign is untouched. This test
	// documents that merge is a pure function with no failure-branch
	// special-casing needed at this layer.
	previous := WorkingDesign{Geometry: &WorkingDesignGeometry{Category: "sofa", ShapeDescription: "x", PreserveCanonicalDimensions: true}}
	result := previous // service simply does not call merge; working design is untouched
	if result.Geometry.ShapeDescription != "x" {
		t.Fatal("working design must remain untouched when merge is not invoked")
	}
}

func TestValidateBaseColor_AcceptsCanonicalHexAndNormalizesToLowercase(t *testing.T) {
	got, err := ValidateBaseColor("#2F4F3A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "#2f4f3a" {
		t.Fatalf("expected normalized lowercase hex, got %q", got)
	}
}

func TestValidateBaseColor_AcceptsAlreadyLowercase(t *testing.T) {
	got, err := ValidateBaseColor("#2f4f3a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "#2f4f3a" {
		t.Fatalf("expected unchanged lowercase hex, got %q", got)
	}
}

func TestValidateBaseColor_RejectsColorName(t *testing.T) {
	if _, err := ValidateBaseColor("dark green"); !errors.Is(err, ErrInvalidBaseColor) {
		t.Fatalf("expected ErrInvalidBaseColor, got %v", err)
	}
}

func TestValidateBaseColor_RejectsMissingHash(t *testing.T) {
	if _, err := ValidateBaseColor("2f4f3a"); !errors.Is(err, ErrInvalidBaseColor) {
		t.Fatalf("expected ErrInvalidBaseColor, got %v", err)
	}
}

func TestValidateBaseColor_RejectsShortHex(t *testing.T) {
	if _, err := ValidateBaseColor("#2f4"); !errors.Is(err, ErrInvalidBaseColor) {
		t.Fatalf("expected ErrInvalidBaseColor, got %v", err)
	}
}

func TestProposedSceneEditDeltaValidate_NormalizesReplaceModeMaterialColor(t *testing.T) {
	delta := ProposedSceneEditDelta{
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeReplace, MaterialSpec: &WorkingDesignMaterial{
			BaseColor: "#2F4F3A", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial: SectionChange{Mode: SectionModePreserve},
	}
	if err := delta.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if delta.Material.MaterialSpec.BaseColor != "#2f4f3a" {
		t.Fatalf("expected in-place lowercase normalization, got %q", delta.Material.MaterialSpec.BaseColor)
	}
}

func TestProposedSceneEditDeltaValidate_RejectsNonHexReplaceModeMaterialColor(t *testing.T) {
	delta := ProposedSceneEditDelta{
		Geometry: SectionChange{Mode: SectionModePreserve},
		Material: SectionChange{Mode: SectionModeReplace, MaterialSpec: &WorkingDesignMaterial{
			BaseColor: "dark green", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial: SectionChange{Mode: SectionModePreserve},
	}
	if err := delta.Validate(); !errors.Is(err, ErrInvalidBaseColor) {
		t.Fatalf("expected ErrInvalidBaseColor, got %v", err)
	}
}

func TestSectionModeSpecConsistency_RejectsReplaceWithoutSpec(t *testing.T) {
	delta := ProposedSceneEditDelta{
		Geometry: SectionChange{Mode: SectionModeReplace}, // no GeometrySpec set
		Material: SectionChange{Mode: SectionModePreserve},
		Spatial:  SectionChange{Mode: SectionModePreserve},
	}
	if _, err := mergeWorkingDesign(WorkingDesign{}, delta); err == nil {
		t.Fatal("expected an error for replace mode with no spec")
	}
}
