package ai

import "testing"

func TestApplySpaceQualityGateDowngradesUngroundedExplicitClaim(t *testing.T) {
	items := []SpaceSuggestionData{
		{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit, SourceExcerpt: "a swimming pool and a sauna"},
	}
	result := ApplySpaceQualityGate(items, condoBrief)
	if len(result.Items) != 1 {
		t.Fatalf("expected item retained (downgraded, not dropped since it's otherwise supported by brief mention), got %+v", result.Items)
	}
	if result.Items[0].EvidenceType == EvidenceExplicit {
		t.Fatal("expected ungrounded explicit claim to be downgraded, not left as explicit")
	}
}

func TestApplySpaceQualityGateKeepsGroundedExplicitClaim(t *testing.T) {
	items := []SpaceSuggestionData{
		{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit, SourceExcerpt: "redo the kitchen"},
	}
	result := ApplySpaceQualityGate(items, condoBrief)
	if result.Items[0].EvidenceType != EvidenceExplicit {
		t.Fatalf("expected grounded explicit claim to remain explicit, got %+v", result.Items[0])
	}
}

func TestApplySpaceQualityGateDropsUngroundedAndUnsupportedClaim(t *testing.T) {
	// "install a swimming pool" is grounded nowhere and not a recognized
	// common-space mention either -> not otherwise supported -> drop.
	items := []SpaceSuggestionData{
		{Name: "Swimming Pool Deck", SpaceType: "pool", EvidenceType: EvidenceExplicit, SourceExcerpt: "install a swimming pool"},
	}
	result := ApplySpaceQualityGate(items, condoBrief)
	if len(result.Items) != 0 {
		t.Fatalf("expected fabricated unsupported claim to be dropped, got %+v", result.Items)
	}
	if len(result.Dropped) != 1 {
		t.Fatalf("expected 1 dropped item recorded, got %+v", result.Dropped)
	}
}

func TestApplySpaceQualityGateRejectsUnsupportedSpecialization(t *testing.T) {
	items := []SpaceSuggestionData{
		{Name: "Guest Bedroom", SpaceType: "bedroom", EvidenceType: EvidenceExplicit, SourceExcerpt: "two additional bedrooms"},
	}
	result := ApplySpaceQualityGate(items, condoBrief)
	if len(result.Items) != 1 {
		t.Fatalf("expected the underlying bedroom claim retained (not dropped), got %+v", result.Items)
	}
	if result.Items[0].Name == "Guest Bedroom" {
		t.Fatalf("expected unsupported specialization 'Guest Bedroom' to be corrected, got %+v", result.Items[0])
	}
}

func TestApplySpaceQualityGateAllowsSupportedSpecialization(t *testing.T) {
	items := []SpaceSuggestionData{
		{Name: "Master Bedroom", SpaceType: "bedroom", EvidenceType: EvidenceExplicit, SourceExcerpt: "master bedroom"},
	}
	result := ApplySpaceQualityGate(items, condoBrief)
	if result.Items[0].Name != "Master Bedroom" {
		t.Fatalf("expected supported specialization kept as-is, got %+v", result.Items[0])
	}
}

func TestApplySpaceQualityGateDetectsCoverageGaps(t *testing.T) {
	items := []SpaceSuggestionData{
		{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit, SourceExcerpt: "redo the kitchen"},
	}
	result := ApplySpaceQualityGate(items, condoBrief)
	if len(result.Gaps) == 0 {
		t.Fatal("expected coverage gaps for the many unmentioned spaces (bedrooms, bathrooms, balcony, yard)")
	}
}

func TestApplySpaceQualityGateNoGapsWhenFullyCovered(t *testing.T) {
	items := []SpaceSuggestionData{{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit, SourceExcerpt: "redo the kitchen"}}
	result := ApplySpaceQualityGate(items, "Redo the kitchen.")
	if len(result.Gaps) != 0 {
		t.Fatalf("expected no gaps for a fully-covered brief, got %+v", result.Gaps)
	}
}

func TestApplyWorkItemQualityGateDropsFabricatedUnsupportedClaim(t *testing.T) {
	// "install a hot tub" has zero token overlap with condoBrief and isn't
	// otherwise supported -> must be dropped, not merely downgraded.
	items := []WorkItemSuggestionData{
		{Description: "Install a hot tub", WorkType: "plumbing", ScopeOrigin: ScopeOriginExplicit, SourceExcerpt: "install a hot tub", MaterialSpecificity: MaterialUnspecified},
	}
	result := ApplyWorkItemQualityGate(items, condoBrief)
	if len(result.Items) != 0 {
		t.Fatalf("expected fabricated claim dropped, got %+v", result.Items)
	}
	if len(result.Dropped) != 1 {
		t.Fatalf("expected 1 dropped item, got %+v", result.Dropped)
	}
}

func TestApplyWorkItemQualityGateDowngradesUngroundedButOtherwiseSupportedClaim(t *testing.T) {
	// The excerpt itself doesn't ground verbatim (extra unrelated words
	// push it below IsGrounded's 0.8 threshold), but "flooring" and
	// "necessary" both genuinely appear in the brief's real conditional
	// flooring statement, clearing the looser otherwiseSupportedWorkItem
	// bar (>= 0.5) -> downgrade rather than drop.
	items := []WorkItemSuggestionData{
		{
			Description: "Flooring work", WorkType: "flooring", ScopeOrigin: ScopeOriginExplicit,
			SourceExcerpt: "flooring necessary urgent immediate priority action required now",
			MaterialSpecificity: MaterialUnspecified,
		},
	}
	result := ApplyWorkItemQualityGate(items, condoBrief)
	if len(result.Items) != 1 {
		t.Fatalf("expected item retained (downgraded), got %+v", result.Items)
	}
	if result.Items[0].ScopeOrigin == ScopeOriginExplicit {
		t.Fatal("expected ungrounded claim to be downgraded from explicit_scope")
	}
}

func TestApplyWorkItemQualityGatePreservesConditionalScopeNotPromotedToDefinite(t *testing.T) {
	// "replace damaged flooring where necessary" with no space established
	// must not be silently converted into definite Kitchen-scoped work.
	items := []WorkItemSuggestionData{
		{
			Description: "Replace flooring in Kitchen", WorkType: "flooring",
			ScopeLevel: ScopeLevelSpace, SpaceID: strPtr("space_kitchen"),
			ScopeOrigin: ScopeOriginExplicit, SourceExcerpt: "replace damaged flooring where necessary",
			MaterialSpecificity: MaterialUnspecified,
		},
	}
	result := ApplyWorkItemQualityGate(items, condoBrief)
	if len(result.Items) != 1 {
		t.Fatalf("expected the conditional flooring claim retained after correction, got %+v", result.Items)
	}
	if result.Items[0].ScopeLevel == ScopeLevelSpace {
		t.Fatalf("expected space-scoped conditional claim without grounded space evidence to be corrected to project-level/unknown scope, got %+v", result.Items[0])
	}
}

func TestApplyWorkItemQualityGateKeepsExplicitSpaceScopeWhenGrounded(t *testing.T) {
	items := []WorkItemSuggestionData{
		{
			Description: "Redo the kitchen", WorkType: "renovation",
			ScopeLevel: ScopeLevelSpace, SpaceID: strPtr("space_kitchen"),
			ScopeOrigin: ScopeOriginExplicit, SourceExcerpt: "redo the kitchen",
			MaterialSpecificity: MaterialUnspecified,
		},
	}
	result := ApplyWorkItemQualityGate(items, condoBrief)
	if result.Items[0].ScopeLevel != ScopeLevelSpace {
		t.Fatalf("expected genuinely grounded space scope to be kept, got %+v", result.Items[0])
	}
}
