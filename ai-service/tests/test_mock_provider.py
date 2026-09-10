"""MockProvider determinism tests.

The mock must be a deterministic function of its request — no randomness —
so Go/Playwright regressions can assert exact suggestion counts/shapes.
"""

from app.providers.mock import MockProvider
from app.schemas.resources import MaterialCandidate, ResourceSuggestionRequest, WorkItemContext
from app.schemas.spaces import ExistingSpace, ProjectContext, SpaceSuggestionRequest
from app.schemas.work_items import SpaceContext, WorkItemSuggestionRequest


class TestMockSpaces:
    def _request(self, existing=None):
        return SpaceSuggestionRequest(
            operationId="op_1",
            project=ProjectContext(id="project_1", scopeBrief="Full renovation of a 3-bedroom condo."),
            existingSpaces=existing or [],
        )

    def test_returns_at_least_three_reviewable_suggestions(self):
        result = MockProvider().suggest_spaces(self._request())
        assert len(result.suggestions) >= 3

    def test_no_authoritative_fields_present(self):
        result = MockProvider().suggest_spaces(self._request())
        for s in result.suggestions:
            assert not hasattr(s, "quantityValue")
            assert not hasattr(s, "cost")

    def test_omits_exact_existing_space_duplicates(self):
        existing = [ExistingSpace(id="space_1", name="Kitchen", type="kitchen")]
        result = MockProvider().suggest_spaces(self._request(existing=existing))
        names = {s.name.strip().lower() for s in result.suggestions}
        assert "kitchen" not in names

    def test_deterministic_across_calls(self):
        r1 = MockProvider().suggest_spaces(self._request())
        r2 = MockProvider().suggest_spaces(self._request())
        assert [s.name for s in r1.suggestions] == [s.name for s in r2.suggestions]

    def test_metadata_present(self):
        result = MockProvider().suggest_spaces(self._request())
        assert result.provider == "mock"
        assert result.model == "mock-v1"
        assert result.promptVersion == "spaces-v1"
        assert result.schemaVersion == 1

    def test_every_suggestion_has_evidence_type(self):
        result = MockProvider().suggest_spaces(self._request())
        for s in result.suggestions:
            assert s.evidenceType in ("explicit", "derived", "possible_missing")

    def test_explicit_evidence_requires_source_excerpt(self):
        result = MockProvider().suggest_spaces(self._request())
        for s in result.suggestions:
            if s.evidenceType == "explicit":
                assert s.sourceExcerpt != ""

    def test_never_claims_explicit_for_unmentioned_space(self):
        # This fixture's brief never mentions a bathroom — MockProvider must
        # not fabricate explicit evidence for "Master Bathroom".
        result = MockProvider().suggest_spaces(self._request())
        bathroom = next((s for s in result.suggestions if s.spaceType == "bathroom"), None)
        assert bathroom is not None
        assert bathroom.evidenceType != "explicit"


class TestMockWorkItems:
    def _request(self):
        return WorkItemSuggestionRequest(
            operationId="op_2",
            projectBrief="Full renovation of a 3-bedroom condo.",
            spaces=[SpaceContext(id="space_123", name="Kitchen", type="kitchen")],
            existingWorkItems=[],
        )

    def test_uses_supplied_space_ids(self):
        result = MockProvider().suggest_work_items(self._request())
        space_scoped = [w for w in result.suggestions if w.scopeLevel == "space"]
        assert space_scoped
        for w in space_scoped:
            assert w.spaceId == "space_123"

    def test_covers_all_three_scope_origins(self):
        result = MockProvider().suggest_work_items(self._request())
        origins = {w.scopeOrigin for w in result.suggestions}
        assert {"explicit_scope", "supporting_scope", "possible_missing_scope"} <= origins

    def test_includes_project_level_item_with_null_space(self):
        result = MockProvider().suggest_work_items(self._request())
        project_level = [w for w in result.suggestions if w.scopeLevel == "project"]
        assert project_level
        for w in project_level:
            assert w.spaceId is None

    def test_stable_ordering(self):
        r1 = MockProvider().suggest_work_items(self._request())
        r2 = MockProvider().suggest_work_items(self._request())
        assert [w.description for w in r1.suggestions] == [w.description for w in r2.suggestions]

    def test_flooring_work_item_material_specificity_is_unspecified(self):
        # Regression fixture: no material is named in this brief, so the
        # flooring Work Item must not claim a material specificity beyond
        # "unspecified" — downstream Resource generation depends on this to
        # avoid inventing tile-specific resources.
        result = MockProvider().suggest_work_items(self._request())
        flooring = next(w for w in result.suggestions if w.workType == "flooring")
        assert flooring.materialSpecificity == "unspecified"

    def test_explicit_scope_origin_requires_source_excerpt(self):
        result = MockProvider().suggest_work_items(self._request())
        for w in result.suggestions:
            if w.scopeOrigin == "explicit_scope":
                assert w.sourceExcerpt != ""


class TestMockResources:
    def _request(self, candidates=None):
        return ResourceSuggestionRequest(
            operationId="op_3",
            workItems=[WorkItemContext(id="work_1", description="Install ceramic floor tiles")],
            materialCandidates=candidates or [],
            existingRequirements=[],
        )

    def test_references_supplied_work_item_ids(self):
        result = MockProvider().suggest_resources(self._request())
        for r in result.suggestions:
            assert r.workItemId == "work_1"

    def test_material_with_candidate_id_when_available(self):
        candidates = [MaterialCandidate(id="mat_1", name="Premium Tile Adhesive")]
        result = MockProvider().suggest_resources(self._request(candidates=candidates))
        materials_with_candidate = [
            r for r in result.suggestions if r.resourceType == "material" and r.candidateMaterialId
        ]
        assert materials_with_candidate
        assert materials_with_candidate[0].candidateMaterialId == "mat_1"

    def test_material_with_no_candidate(self):
        result = MockProvider().suggest_resources(self._request())
        materials = [r for r in result.suggestions if r.resourceType == "material"]
        assert materials
        assert all(r.candidateMaterialId is None for r in materials)

    def test_includes_trade_and_equipment(self):
        result = MockProvider().suggest_resources(self._request())
        types = {r.resourceType for r in result.suggestions}
        assert "trade" in types
        assert "equipment" in types

    def test_stable_ordering(self):
        r1 = MockProvider().suggest_resources(self._request())
        r2 = MockProvider().suggest_resources(self._request())
        assert [r.name for r in r1.suggestions] == [r.name for r in r2.suggestions]
