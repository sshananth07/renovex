package ai

import "testing"

func candidates() []DomainGatewayMaterial {
	return []DomainGatewayMaterial{
		{ID: "m1", Name: "Cement", Category: "Flooring", Unit: "bag"},
		{ID: "m2", Name: "Tile Grout", Category: "Flooring", Unit: "bag"},
		{ID: "m3", Name: "Tile Adhesive", Category: "Flooring", Unit: "bag"},
	}
}

func TestMatchMaterialExactNameWinsOverCategoryUnitSiblings(t *testing.T) {
	// The regression case from the task: "Tile Adhesive" must never resolve
	// to "Cement" merely because both share category+unit.
	result := MatchMaterial("Tile Adhesive", "", "", candidates(), nil)
	if result.Tier != TierExactName {
		t.Fatalf("Tier = %q, want %q", result.Tier, TierExactName)
	}
	if result.MaterialID != "m3" {
		t.Fatalf("MaterialID = %q, want m3 (Tile Adhesive)", result.MaterialID)
	}
}

func TestMatchMaterialExactNameIsCaseAndWhitespaceInsensitive(t *testing.T) {
	result := MatchMaterial("  tile   adhesive ", "", "", candidates(), nil)
	if result.Tier != TierExactName || result.MaterialID != "m3" {
		t.Fatalf("got %+v, want exact match on m3", result)
	}
}

func TestMatchMaterialStrongLexicalRequiresCategoryUnitCompatibility(t *testing.T) {
	// "Tile Adhesives" (plural, no exact match) should still land on the
	// Tile Adhesive candidate via strong lexical + compatible category/unit,
	// not on an unrelated candidate.
	result := MatchMaterial("Tile Adhesives", "Flooring", "bag", candidates(), nil)
	if result.Tier != TierStrongLexical {
		t.Fatalf("Tier = %q, want %q", result.Tier, TierStrongLexical)
	}
	if result.MaterialID != "m3" {
		t.Fatalf("MaterialID = %q, want m3", result.MaterialID)
	}
}

func TestMatchMaterialLexicalAloneWithoutCompatibilityDoesNotWin(t *testing.T) {
	// Weak lexical similarity with incompatible category/unit must not win
	// deterministically — falls through to the model-proposed candidate (or
	// no match if none).
	weakCandidates := []DomainGatewayMaterial{
		{ID: "m9", Name: "Tiler Labour Kit", Category: "Labour", Unit: "job"},
	}
	result := MatchMaterial("Tile Adhesive", "Flooring", "bag", weakCandidates, nil)
	if result.Tier == TierStrongLexical || result.Tier == TierExactName {
		t.Fatalf("expected no strong deterministic tier, got %+v", result)
	}
}

func TestMatchMaterialValidatesModelProposedCandidateWhenNoStrongMatch(t *testing.T) {
	modelProposed := "m2" // Tile Grout — plausible synonym-ish fallback, compatible category/unit
	result := MatchMaterial("Grout", "Flooring", "bag", candidates(), &modelProposed)
	if result.Tier != TierValidatedModel {
		t.Fatalf("Tier = %q, want %q", result.Tier, TierValidatedModel)
	}
	if result.MaterialID != "m2" {
		t.Fatalf("MaterialID = %q, want m2", result.MaterialID)
	}
}

func TestMatchMaterialRejectsModelProposedCandidateNotInSuppliedList(t *testing.T) {
	modelProposed := "not-in-list"
	result := MatchMaterial("Something", "Flooring", "bag", candidates(), &modelProposed)
	if result.Tier != TierNone {
		t.Fatalf("Tier = %q, want %q (candidate not authorized)", result.Tier, TierNone)
	}
	if result.MaterialID != "" {
		t.Fatalf("MaterialID = %q, want empty on no-match", result.MaterialID)
	}
}

func TestMatchMaterialRejectsModelProposedCandidateWithIncompatibleUnit(t *testing.T) {
	modelProposed := "m1" // Cement — unit compatible (bag) but wrong resource entirely; use unit mismatch case instead
	incompatible := []DomainGatewayMaterial{
		{ID: "m1", Name: "Cement", Category: "Flooring", Unit: "m3"}, // unit mismatch vs requested "bag"
	}
	result := MatchMaterial("Cement Mix", "Flooring", "bag", incompatible, &modelProposed)
	if result.Tier != TierNone {
		t.Fatalf("Tier = %q, want %q (incompatible unit)", result.Tier, TierNone)
	}
}

func TestMatchMaterialNoMatchWhenNothingCompatible(t *testing.T) {
	result := MatchMaterial("Completely Unrelated Widget", "Electrical", "unit", candidates(), nil)
	if result.Tier != TierNone {
		t.Fatalf("Tier = %q, want %q", result.Tier, TierNone)
	}
	if result.MaterialID != "" {
		t.Fatalf("MaterialID = %q, want empty", result.MaterialID)
	}
}

func TestMatchMaterialEmptyCandidateListIsNoMatch(t *testing.T) {
	result := MatchMaterial("Tile Adhesive", "", "", nil, nil)
	if result.Tier != TierNone {
		t.Fatalf("Tier = %q, want %q", result.Tier, TierNone)
	}
}
