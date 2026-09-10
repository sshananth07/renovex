"""Pydantic schema invariant tests.

These prove shape-level constraints only — contextual membership (e.g. a
spaceId actually belonging to the supplied Space set) is an orchestration
concern, not a schema concern (design doc "Structured Output" section).
"""

import pytest
from pydantic import ValidationError

from app.schemas.resources import ResourceSuggestion
from app.schemas.spaces import SpaceSuggestion
from app.schemas.work_items import WorkItemSuggestion


class TestSpaceSuggestion:
    def test_valid_minimal(self):
        s = SpaceSuggestion(name="Kitchen", spaceType="kitchen")
        assert s.name == "Kitchen"
        assert s.confidence is None

    def test_name_required_non_empty(self):
        with pytest.raises(ValidationError):
            SpaceSuggestion(name="", spaceType="kitchen")

    def test_space_type_required_non_empty(self):
        with pytest.raises(ValidationError):
            SpaceSuggestion(name="Kitchen", spaceType="")

    def test_confidence_bounded_zero_to_one(self):
        SpaceSuggestion(name="Kitchen", spaceType="kitchen", confidence=0.0)
        SpaceSuggestion(name="Kitchen", spaceType="kitchen", confidence=1.0)
        with pytest.raises(ValidationError):
            SpaceSuggestion(name="Kitchen", spaceType="kitchen", confidence=1.1)
        with pytest.raises(ValidationError):
            SpaceSuggestion(name="Kitchen", spaceType="kitchen", confidence=-0.1)

    def test_rejects_unknown_fields(self):
        with pytest.raises(ValidationError):
            SpaceSuggestion(
                name="Kitchen", spaceType="kitchen", floorAreaSqm=12.0
            )

    def test_schema_has_no_quantity_cost_price_fields(self):
        field_names = set(SpaceSuggestion.model_fields.keys())
        forbidden = {
            "quantity", "quantityValue", "quantityUnit", "cost", "price",
            "dimensions", "floorArea", "measurements",
        }
        assert field_names.isdisjoint(forbidden)


class TestWorkItemSuggestion:
    def _base(self, **overrides):
        data = {
            "description": "Install ceramic floor tiles",
            "workType": "tile_installation",
            "scopeLevel": "space",
            "spaceId": "space_123",
            "scopeOrigin": "explicit_scope",
        }
        data.update(overrides)
        return data

    def test_valid_space_scoped(self):
        w = WorkItemSuggestion(**self._base())
        assert w.spaceId == "space_123"

    def test_space_scoped_requires_space_id(self):
        with pytest.raises(ValidationError):
            WorkItemSuggestion(**self._base(scopeLevel="space", spaceId=None))

    def test_project_scoped_requires_null_space_id(self):
        with pytest.raises(ValidationError):
            WorkItemSuggestion(**self._base(scopeLevel="project", spaceId="space_123"))

    def test_project_scoped_valid_with_null_space(self):
        w = WorkItemSuggestion(**self._base(scopeLevel="project", spaceId=None))
        assert w.spaceId is None

    @pytest.mark.parametrize(
        "origin", ["explicit_scope", "supporting_scope", "possible_missing_scope"]
    )
    def test_valid_scope_origins(self, origin):
        WorkItemSuggestion(**self._base(scopeOrigin=origin))

    def test_invalid_scope_origin_rejected(self):
        with pytest.raises(ValidationError):
            WorkItemSuggestion(**self._base(scopeOrigin="fabricated"))

    def test_schema_has_no_quantity_unit_cost_price_fields(self):
        field_names = set(WorkItemSuggestion.model_fields.keys())
        forbidden = {"quantityValue", "quantityUnit", "cost", "price", "rate"}
        assert field_names.isdisjoint(forbidden)

    def test_rejects_unknown_fields(self):
        with pytest.raises(ValidationError):
            WorkItemSuggestion(**self._base(quantityValue="30"))


class TestResourceSuggestion:
    def _base(self, **overrides):
        data = {
            "resourceType": "material",
            "workItemId": "work_1",
            "name": "Tile Adhesive",
            "candidateMaterialId": None,
        }
        data.update(overrides)
        return data

    @pytest.mark.parametrize("resource_type", ["material", "trade", "equipment"])
    def test_valid_resource_types(self, resource_type):
        ResourceSuggestion(**self._base(resourceType=resource_type, candidateMaterialId=None))

    def test_invalid_resource_type_rejected(self):
        with pytest.raises(ValidationError):
            ResourceSuggestion(**self._base(resourceType="subcontractor"))

    def test_work_item_id_required(self):
        with pytest.raises(ValidationError):
            ResourceSuggestion(**self._base(workItemId=""))

    def test_candidate_material_id_allowed_only_for_material(self):
        ResourceSuggestion(**self._base(resourceType="material", candidateMaterialId="mat_1"))
        with pytest.raises(ValidationError):
            ResourceSuggestion(**self._base(resourceType="trade", candidateMaterialId="mat_1"))
        with pytest.raises(ValidationError):
            ResourceSuggestion(**self._base(resourceType="equipment", candidateMaterialId="mat_1"))

    def test_schema_has_no_rate_price_cost_quantity_fields(self):
        field_names = set(ResourceSuggestion.model_fields.keys())
        forbidden = {"rate", "price", "cost", "quantity", "quantityValue", "quantityUnit"}
        assert field_names.isdisjoint(forbidden)

    def test_rejects_unknown_fields(self):
        with pytest.raises(ValidationError):
            ResourceSuggestion(**self._base(estimatedCost=100))
