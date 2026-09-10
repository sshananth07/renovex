package spatial

import (
	"errors"
	"testing"
)

func validAppearance() VisualAppearance {
	return VisualAppearance{BaseColor: "#2F4F3A", MaterialFamily: MaterialFamilyFabric, Roughness: RoughnessMatte, Metallic: false}
}

func TestVisualAppearanceValidate_AcceptsValidAndNormalizesColor(t *testing.T) {
	a := validAppearance()
	if err := a.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.BaseColor != "#2f4f3a" {
		t.Fatalf("expected normalized lowercase hex, got %q", a.BaseColor)
	}
}

func TestVisualAppearanceValidate_RejectsNonHexColor(t *testing.T) {
	a := validAppearance()
	a.BaseColor = "dark green"
	if err := a.Validate(); !errors.Is(err, ErrInvalidVisualAppearance) {
		t.Fatalf("expected ErrInvalidVisualAppearance, got %v", err)
	}
}

func TestVisualAppearanceValidate_RejectsUnknownMaterialFamily(t *testing.T) {
	a := validAppearance()
	a.MaterialFamily = "plastic"
	if err := a.Validate(); !errors.Is(err, ErrInvalidVisualAppearance) {
		t.Fatalf("expected ErrInvalidVisualAppearance, got %v", err)
	}
}

func TestVisualAppearanceValidate_RejectsUnknownRoughness(t *testing.T) {
	a := validAppearance()
	a.Roughness = "shiny"
	if err := a.Validate(); !errors.Is(err, ErrInvalidVisualAppearance) {
		t.Fatalf("expected ErrInvalidVisualAppearance, got %v", err)
	}
}

func TestVisualAppearanceValidate_AcceptsAllMaterialFamilies(t *testing.T) {
	for _, f := range []MaterialFamily{MaterialFamilyFabric, MaterialFamilyLeather, MaterialFamilyWood, MaterialFamilyMetal, MaterialFamilyStone, MaterialFamilyOther} {
		a := validAppearance()
		a.MaterialFamily = f
		if err := a.Validate(); err != nil {
			t.Fatalf("expected family %q to be valid, got %v", f, err)
		}
	}
}

func TestVisualAppearanceValidate_AcceptsAllRoughnessValues(t *testing.T) {
	for _, r := range []Roughness{RoughnessMatte, RoughnessSatin, RoughnessGlossy} {
		a := validAppearance()
		a.Roughness = r
		if err := a.Validate(); err != nil {
			t.Fatalf("expected roughness %q to be valid, got %v", r, err)
		}
	}
}
