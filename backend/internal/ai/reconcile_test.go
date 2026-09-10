package ai

import "testing"

func TestClassifySpaceDeltaUnchangedWhenMatchesCurrentAuthoritative(t *testing.T) {
	fresh := []SpaceSuggestionData{{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit}}
	current := []DomainGatewaySpace{{ID: "space_1", Name: "Kitchen", Type: "kitchen"}}
	items := ClassifySpaceDelta(fresh, current, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaUnchanged {
		t.Fatalf("expected UNCHANGED, got %+v", items)
	}
	if items[0].CurrentAuthoritativeID != "space_1" {
		t.Fatalf("expected authoritative id space_1, got %+v", items[0])
	}
}

func TestClassifySpaceDeltaNewWhenNoMatchAndNoHistory(t *testing.T) {
	fresh := []SpaceSuggestionData{{Name: "Study", SpaceType: "study", EvidenceType: EvidenceExplicit}}
	items := ClassifySpaceDelta(fresh, nil, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaNew {
		t.Fatalf("expected NEW, got %+v", items)
	}
}

func TestClassifySpaceDeltaChangedRequiresTrustworthyStableLink(t *testing.T) {
	// "Bedroom 3" was previously accepted -> real Space "space_3" named
	// "Bedroom". Fresh suggestion proposes "Home Office" for the same
	// underlying prior suggestion (same SpaceType lineage via
	// AcceptedDomainObjectID) -> CHANGED, not a fuzzy name guess.
	fresh := []SpaceSuggestionData{{Name: "Home Office", SpaceType: "bedroom", EvidenceType: EvidenceExplicit}}
	current := []DomainGatewaySpace{{ID: "space_3", Name: "Bedroom", Type: "bedroom"}}
	prior := []PriorSpaceDecision{
		{
			Data:                   SpaceSuggestionData{Name: "Bedroom", SpaceType: "bedroom"},
			Status:                 SuggestionStatusAccepted,
			AcceptedDomainObjectID: "space_3",
		},
	}
	items := ClassifySpaceDelta(fresh, current, prior, "brief")
	if len(items) != 1 || items[0].Classification != DeltaChanged {
		t.Fatalf("expected CHANGED, got %+v", items)
	}
	if items[0].CurrentAuthoritativeID != "space_3" {
		t.Fatalf("expected authoritative id space_3, got %+v", items[0])
	}
}

func TestClassifySpaceDeltaDoesNotInferChangeFromFuzzyNameAlone(t *testing.T) {
	// No prior AcceptedDomainObjectID lineage links "Guest Room" to
	// "Bedroom" — must not be classified CHANGED merely because the names
	// are semantically similar. Falls through to NEW.
	fresh := []SpaceSuggestionData{{Name: "Guest Room", SpaceType: "bedroom", EvidenceType: EvidenceExplicit}}
	current := []DomainGatewaySpace{{ID: "space_3", Name: "Bedroom", Type: "bedroom"}}
	items := ClassifySpaceDelta(fresh, current, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaNew {
		t.Fatalf("expected NEW (no fuzzy inference), got %+v", items)
	}
}

func TestClassifySpaceDeltaChangedIgnoresLinkToDeletedEntity(t *testing.T) {
	// AcceptedDomainObjectID points to a Space that no longer exists in
	// currentSpaces (deleted) -> not a trustworthy link -> not CHANGED.
	fresh := []SpaceSuggestionData{{Name: "Home Office", SpaceType: "bedroom", EvidenceType: EvidenceExplicit}}
	prior := []PriorSpaceDecision{
		{
			Data:                   SpaceSuggestionData{Name: "Bedroom", SpaceType: "bedroom"},
			Status:                 SuggestionStatusAccepted,
			AcceptedDomainObjectID: "space_deleted",
		},
	}
	items := ClassifySpaceDelta(fresh, nil, prior, "brief")
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %+v", items)
	}
	if items[0].Classification == DeltaChanged {
		t.Fatalf("expected NOT CHANGED for a link to a deleted entity, got %+v", items)
	}
}

func TestClassifySpaceDeltaSuppressedForEquivalentRejectionUnderSameBrief(t *testing.T) {
	fresh := []SpaceSuggestionData{{Name: "Guest Bedroom", SpaceType: "bedroom", EvidenceType: EvidencePossibleMissing}}
	prior := []PriorSpaceDecision{
		{
			Data:        SpaceSuggestionData{Name: "Guest Bedroom", SpaceType: "bedroom"},
			Status:      SuggestionStatusRejected,
			SourceBrief: "same brief text",
		},
	}
	items := ClassifySpaceDelta(fresh, nil, prior, "same brief text")
	if len(items) != 1 || items[0].Classification != DeltaSuppressed {
		t.Fatalf("expected SUPPRESSED, got %+v", items)
	}
}

// --- Work Item delta (T1.5B closure) ---

func TestClassifyWorkItemDeltaUnchangedWhenMatchesCurrentAuthoritative(t *testing.T) {
	spaceID := "space_1"
	fresh := []WorkItemSuggestionData{{Description: "Replace flooring", SpaceID: &spaceID, ScopeLevel: ScopeLevelSpace}}
	current := []DomainGatewayWorkItem{{ID: "wi_1", SpaceID: "space_1", Description: "Replace flooring"}}
	items := ClassifyWorkItemDelta(fresh, current, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaUnchanged {
		t.Fatalf("expected UNCHANGED, got %+v", items)
	}
	if items[0].CurrentAuthoritativeID != "wi_1" {
		t.Fatalf("expected authoritative id wi_1, got %+v", items[0])
	}
}

func TestClassifyWorkItemDeltaNewWhenNoMatchAndNoHistory(t *testing.T) {
	fresh := []WorkItemSuggestionData{{Description: "Paint ceiling", ScopeLevel: ScopeLevelProject}}
	items := ClassifyWorkItemDelta(fresh, nil, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaNew {
		t.Fatalf("expected NEW, got %+v", items)
	}
}

func TestClassifyWorkItemDeltaChangedRequiresTrustworthyStableLink(t *testing.T) {
	spaceID := "space_1"
	fresh := []WorkItemSuggestionData{{Description: "Replace flooring with tile", SpaceID: &spaceID, ScopeLevel: ScopeLevelSpace}}
	current := []DomainGatewayWorkItem{{ID: "wi_1", SpaceID: "space_1", Description: "Replace flooring"}}
	prior := []PriorWorkItemDecision{
		{
			Data:                   WorkItemSuggestionData{Description: "Replace flooring", SpaceID: &spaceID},
			Status:                 SuggestionStatusAccepted,
			AcceptedDomainObjectID: "wi_1",
		},
	}
	items := ClassifyWorkItemDelta(fresh, current, prior, "brief")
	if len(items) != 1 || items[0].Classification != DeltaChanged {
		t.Fatalf("expected CHANGED, got %+v", items)
	}
	if items[0].CurrentAuthoritativeID != "wi_1" {
		t.Fatalf("expected authoritative id wi_1, got %+v", items[0])
	}
}

func TestClassifyWorkItemDeltaDoesNotInferChangeFromFuzzyDescriptionAlone(t *testing.T) {
	spaceID := "space_1"
	fresh := []WorkItemSuggestionData{{Description: "Replace flooring with tile", SpaceID: &spaceID, ScopeLevel: ScopeLevelSpace}}
	current := []DomainGatewayWorkItem{{ID: "wi_1", SpaceID: "space_1", Description: "Replace flooring"}}
	items := ClassifyWorkItemDelta(fresh, current, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaNew {
		t.Fatalf("expected NEW (no fuzzy inference), got %+v", items)
	}
}

func TestClassifyWorkItemDeltaSuppressedForEquivalentRejectionUnderSameBrief(t *testing.T) {
	fresh := []WorkItemSuggestionData{{Description: "Install hot tub", ScopeLevel: ScopeLevelProject}}
	prior := []PriorWorkItemDecision{
		{
			Data:        WorkItemSuggestionData{Description: "Install hot tub"},
			Status:      SuggestionStatusRejected,
			SourceBrief: "same brief text",
		},
	}
	items := ClassifyWorkItemDelta(fresh, nil, prior, "same brief text")
	if len(items) != 1 || items[0].Classification != DeltaSuppressed {
		t.Fatalf("expected SUPPRESSED, got %+v", items)
	}
}

func TestClassifyWorkItemDeltaRejectionDoesNotSuppressAfterBriefChanges(t *testing.T) {
	fresh := []WorkItemSuggestionData{{Description: "Install hot tub", ScopeLevel: ScopeLevelProject}}
	prior := []PriorWorkItemDecision{
		{
			Data:        WorkItemSuggestionData{Description: "Install hot tub"},
			Status:      SuggestionStatusRejected,
			SourceBrief: "old brief",
		},
	}
	items := ClassifyWorkItemDelta(fresh, nil, prior, "new brief explicitly requesting a hot tub")
	if len(items) != 1 || items[0].Classification != DeltaNew {
		t.Fatalf("expected NEW after brief materially changed, got %+v", items)
	}
}

// --- Resource delta (T1.5B closure) ---

func TestClassifyResourceDeltaUnchangedWhenMatchesCurrentAuthoritative(t *testing.T) {
	fresh := []ResourceDeltaItem{{WorkItemID: "wi_1", ResourceType: "trade", Name: "Tiler"}}
	current := []DomainGatewayRequirement{{ID: "req_1", WorkItemID: "wi_1", ResourceType: "trade", Name: "Tiler"}}
	items := ClassifyResourceDelta(fresh, current, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaUnchanged {
		t.Fatalf("expected UNCHANGED, got %+v", items)
	}
	if items[0].CurrentAuthoritativeID != "req_1" {
		t.Fatalf("expected authoritative id req_1, got %+v", items[0])
	}
}

func TestClassifyResourceDeltaNewWhenNoMatchAndNoHistory(t *testing.T) {
	fresh := []ResourceDeltaItem{{WorkItemID: "wi_1", ResourceType: "material", Name: "Grout"}}
	items := ClassifyResourceDelta(fresh, nil, nil, "brief")
	if len(items) != 1 || items[0].Classification != DeltaNew {
		t.Fatalf("expected NEW, got %+v", items)
	}
}

func TestClassifyResourceDeltaConflictInsteadOfChangedWhenLineageDiffers(t *testing.T) {
	// This is the key T1.5B closure behavior: a fresh suggestion linking
	// (via trustworthy AcceptedDomainObjectID lineage) to a current
	// requirement with a DIFFERENT name must surface as CONFLICT, never
	// CHANGED — workresources has no update-in-place path, so there is no
	// safe "Use suggestion" action to offer.
	fresh := []ResourceDeltaItem{{WorkItemID: "wi_1", ResourceType: "material", Name: "Premium Tile Adhesive"}}
	current := []DomainGatewayRequirement{{ID: "req_1", WorkItemID: "wi_1", ResourceType: "material", Name: "Tile Adhesive"}}
	prior := []PriorResourceDecision{
		{
			WorkItemID: "wi_1", ResourceType: "material", Name: "Tile Adhesive",
			Status: SuggestionStatusAccepted, AcceptedDomainObjectID: "req_1",
		},
	}
	items := ClassifyResourceDelta(fresh, current, prior, "brief")
	if len(items) != 1 || items[0].Classification != DeltaConflict {
		t.Fatalf("expected CONFLICT (never CHANGED), got %+v", items)
	}
	if items[0].CurrentAuthoritativeID != "req_1" {
		t.Fatalf("expected authoritative id req_1, got %+v", items[0])
	}
}

func TestClassifyResourceDeltaNeverReturnsChanged(t *testing.T) {
	fresh := []ResourceDeltaItem{{WorkItemID: "wi_1", ResourceType: "trade", Name: "Painter"}}
	current := []DomainGatewayRequirement{{ID: "req_1", WorkItemID: "wi_1", ResourceType: "trade", Name: "Tiler"}}
	prior := []PriorResourceDecision{
		{WorkItemID: "wi_1", ResourceType: "trade", Name: "Tiler", Status: SuggestionStatusAccepted, AcceptedDomainObjectID: "req_1"},
	}
	items := ClassifyResourceDelta(fresh, current, prior, "brief")
	for _, item := range items {
		if item.Classification == DeltaChanged {
			t.Fatalf("ClassifyResourceDelta must never return DeltaChanged, got %+v", items)
		}
	}
}

func TestClassifyResourceDeltaSuppressedForEquivalentRejectionUnderSameBrief(t *testing.T) {
	fresh := []ResourceDeltaItem{{WorkItemID: "wi_1", ResourceType: "material", Name: "Marble Tile"}}
	prior := []PriorResourceDecision{
		{WorkItemID: "wi_1", ResourceType: "material", Name: "Marble Tile", Status: SuggestionStatusRejected, SourceBrief: "same brief"},
	}
	items := ClassifyResourceDelta(fresh, nil, prior, "same brief")
	if len(items) != 1 || items[0].Classification != DeltaSuppressed {
		t.Fatalf("expected SUPPRESSED, got %+v", items)
	}
}

func TestClassifySpaceDeltaRejectionDoesNotSuppressAfterBriefChanges(t *testing.T) {
	fresh := []SpaceSuggestionData{{Name: "Guest Bedroom", SpaceType: "bedroom", EvidenceType: EvidenceExplicit}}
	prior := []PriorSpaceDecision{
		{
			Data:        SpaceSuggestionData{Name: "Guest Bedroom", SpaceType: "bedroom"},
			Status:      SuggestionStatusRejected,
			SourceBrief: "old brief without a guest bedroom",
		},
	}
	items := ClassifySpaceDelta(fresh, nil, prior, "new brief: convert Bedroom 3 into a guest bedroom")
	if len(items) != 1 || items[0].Classification != DeltaNew {
		t.Fatalf("expected NEW after brief materially changed, got %+v", items)
	}
}
