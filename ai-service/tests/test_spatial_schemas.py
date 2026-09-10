"""Strict schema invariant tests for RP4E1 spatial reasoning models.

Every model forbids extra fields, forbids type coercion and NaN/Infinity,
enforces declared bounds/enums, and enforces mode/spec consistency in the
delta sections. These are shape-level tests only; cross-referencing against
a specific request's supplied IDs is Go/service-layer concern (see
docs/superpowers/plans/2026-09-09-rp4e1-conversational-design-reasoning.md).
"""

import json
import math
from pathlib import Path

import pytest
from pydantic import ValidationError

from app.schemas.spatial_reasoning import (
    GeometryChange,
    MaterialChange,
    MaterialSpec,
    ProposedSceneEditDelta,
    SelectedTargetRef,
    SpatialChange,
    SpatialReasoningRequest,
)

FIXTURES_DIR = Path(__file__).parent / "fixtures" / "spatial_reasoning"


def load_fixture(name: str) -> dict:
    return json.loads((FIXTURES_DIR / name).read_text())


class TestSelectedTargetRef:
    def test_valid_object_target(self):
        t = SelectedTargetRef(kind="object", id="object_sofa_123")
        assert t.kind == "object"

    def test_valid_fixture_target(self):
        SelectedTargetRef(kind="fixture", id="fixture_ac_001")

    def test_rejects_unsupported_kind(self):
        with pytest.raises(ValidationError):
            SelectedTargetRef(kind="wall", id="wall_1")

    def test_rejects_empty_id(self):
        with pytest.raises(ValidationError):
            SelectedTargetRef(kind="object", id="")

    def test_rejects_unknown_fields(self):
        with pytest.raises(ValidationError):
            SelectedTargetRef(kind="object", id="object_1", extra="nope")


class TestMaterialSpecBaseColor:
    def test_accepts_canonical_lowercase_hex(self):
        MaterialSpec(baseColor="#2f4f3a", materialFamily="fabric", roughness="matte", metallic=False)

    def test_accepts_canonical_uppercase_hex(self):
        MaterialSpec(baseColor="#2F4F3A", materialFamily="fabric", roughness="matte", metallic=False)

    def test_rejects_color_name(self):
        with pytest.raises(ValidationError):
            MaterialSpec(baseColor="dark green", materialFamily="fabric", roughness="matte", metallic=False)

    def test_rejects_missing_hash(self):
        with pytest.raises(ValidationError):
            MaterialSpec(baseColor="2F4F3A", materialFamily="fabric", roughness="matte", metallic=False)

    def test_rejects_short_hex(self):
        with pytest.raises(ValidationError):
            MaterialSpec(baseColor="#2F4", materialFamily="fabric", roughness="matte", metallic=False)

    def test_rejects_non_hex_characters(self):
        with pytest.raises(ValidationError):
            MaterialSpec(baseColor="#GGGGGG", materialFamily="fabric", roughness="matte", metallic=False)


class TestSectionModeSpecConsistency:
    def test_preserve_mode_forbids_spec(self):
        with pytest.raises(ValidationError):
            GeometryChange(mode="preserve", spec={"category": "sofa", "shapeDescription": "x", "preserveCanonicalDimensions": True})

    def test_clear_mode_forbids_spec(self):
        with pytest.raises(ValidationError):
            MaterialChange(mode="clear", spec={"baseColor": "#C8A464", "materialFamily": "fabric", "roughness": "matte", "metallic": False})

    def test_replace_mode_requires_spec(self):
        with pytest.raises(ValidationError):
            GeometryChange(mode="replace")

    def test_replace_mode_with_spec_is_valid(self):
        GeometryChange(mode="replace", spec={"category": "sofa", "shapeDescription": "Curved sofa", "preserveCanonicalDimensions": True})

    def test_preserve_mode_without_spec_is_valid(self):
        GeometryChange(mode="preserve")
        MaterialChange(mode="preserve")
        SpatialChange(mode="preserve")

    def test_clear_mode_without_spec_is_valid(self):
        MaterialChange(mode="clear")


class TestSpatialChangeDiscriminatedUnion:
    def test_move_relative_to_nearest_wall_valid(self):
        SpatialChange(mode="replace", spec={
            "kind": "move_relative_to_nearest_wall", "relationship": "away_from", "distanceMeters": 0.2,
        })

    def test_move_relative_distance_must_be_in_open_bounded_range(self):
        with pytest.raises(ValidationError):
            SpatialChange(mode="replace", spec={
                "kind": "move_relative_to_nearest_wall", "relationship": "away_from", "distanceMeters": 0.0,
            })
        with pytest.raises(ValidationError):
            SpatialChange(mode="replace", spec={
                "kind": "move_relative_to_nearest_wall", "relationship": "away_from", "distanceMeters": 2.01,
            })
        SpatialChange(mode="replace", spec={
            "kind": "move_relative_to_nearest_wall", "relationship": "away_from", "distanceMeters": 2.0,
        })

    def test_move_relative_relationship_enum(self):
        with pytest.raises(ValidationError):
            SpatialChange(mode="replace", spec={
                "kind": "move_relative_to_nearest_wall", "relationship": "sideways", "distanceMeters": 0.2,
            })

    def test_resize_axis_requires_exactly_one_of_delta_or_target(self):
        with pytest.raises(ValidationError):
            SpatialChange(mode="replace", spec={"kind": "resize_axis", "axis": "x"})
        with pytest.raises(ValidationError):
            SpatialChange(mode="replace", spec={
                "kind": "resize_axis", "axis": "x", "deltaMeters": 0.1, "targetMeters": 1.0,
            })
        SpatialChange(mode="replace", spec={"kind": "resize_axis", "axis": "x", "deltaMeters": 0.1})
        SpatialChange(mode="replace", spec={"kind": "resize_axis", "axis": "y", "targetMeters": 1.0})

    def test_resize_axis_enum(self):
        with pytest.raises(ValidationError):
            SpatialChange(mode="replace", spec={"kind": "resize_axis", "axis": "w", "deltaMeters": 0.1})

    def test_rejects_operation_kind_outside_closed_union(self):
        with pytest.raises(ValidationError):
            SpatialChange(mode="replace", spec={"kind": "rotate_object", "degrees": 90})

    def test_rejects_direct_coordinates(self):
        fixture = load_fixture("malformed_direct_coordinates.json")
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**fixture["rawModelOutput"])


class TestProposedSceneEditDeltaStrictness:
    def _valid_kwargs(self, **overrides):
        data = {
            "schemaVersion": 1,
            "target": {"kind": "object", "id": "object_sofa_123"},
            "intent": "material_appearance",
            "summary": ["Change the sofa upholstery to beige."],
            "geometry": {"mode": "preserve"},
            "material": {
                "mode": "replace",
                "spec": {"baseColor": "#C8A464", "materialFamily": "fabric", "roughness": "matte", "metallic": False},
            },
            "spatial": {"mode": "preserve"},
            "blockers": [],
            "assumptions": [],
            "reviewNotes": [],
            "confidence": 0.91,
        }
        data.update(overrides)
        return data

    def test_valid_minimal(self):
        ProposedSceneEditDelta(**self._valid_kwargs())

    def test_rejects_unknown_top_level_field(self):
        fixture = load_fixture("malformed_extra_fields.json")
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**fixture["rawModelOutput"])

    def test_rejects_nan_and_infinity_numeric(self):
        fixture = load_fixture("malformed_nonfinite.json")
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**fixture["rawModelOutputNumericNaN"])

    def test_rejects_infinity_as_string_without_coercion(self):
        fixture = load_fixture("malformed_nonfinite.json")
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**fixture["rawModelOutputStringInfinity"])

    def test_rejects_unsupported_spatial_operation(self):
        fixture = load_fixture("malformed_unsupported_operation.json")
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**fixture["rawModelOutput"])

    def test_rejects_replace_mode_missing_spec(self):
        fixture = load_fixture("malformed_missing_geometry_spec.json")
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**fixture["rawModelOutput"])

    def test_material_only_intent_forbids_geometry_replace(self):
        fixture = load_fixture("malformed_material_only_with_geometry_replace.json")
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**fixture["rawModelOutput"])

    def test_confidence_bounded_zero_to_one(self):
        ProposedSceneEditDelta(**self._valid_kwargs(confidence=0.0))
        ProposedSceneEditDelta(**self._valid_kwargs(confidence=1.0))
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**self._valid_kwargs(confidence=1.01))
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**self._valid_kwargs(confidence=-0.01))

    def test_summary_bounded_one_to_eight_items(self):
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**self._valid_kwargs(summary=[]))
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**self._valid_kwargs(summary=["x"] * 9))

    def test_blockers_bounded_to_eight(self):
        with pytest.raises(ValidationError):
            ProposedSceneEditDelta(**self._valid_kwargs(
                blockers=[{"code": "unsupported_operation", "message": "x"}] * 9
            ))

    @pytest.mark.parametrize(
        "fixture_name",
        ["material_only.json", "geometry_only.json", "spatial_only.json", "mixed.json", "unsupported_structural.json"],
    )
    def test_semantic_fixtures_all_parse(self, fixture_name):
        fixture = load_fixture(fixture_name)
        ProposedSceneEditDelta(**fixture["expectedProposedDelta"])


class TestSpatialReasoningRequestBounds:
    def _base(self, **overrides):
        data = load_fixture("material_only.json")["request"]
        data.update(overrides)
        return data

    def test_valid_minimal(self):
        SpatialReasoningRequest(**self._base())

    def test_rejects_unknown_top_level_field(self):
        with pytest.raises(ValidationError):
            SpatialReasoningRequest(**self._base(unexpectedField="nope"))

    def test_instruction_bounded_one_to_two_thousand_bytes(self):
        with pytest.raises(ValidationError):
            SpatialReasoningRequest(**self._base(instruction=""))
        with pytest.raises(ValidationError):
            SpatialReasoningRequest(**self._base(instruction="x" * 2001))
        SpatialReasoningRequest(**self._base(instruction="x" * 2000))

    def test_selected_element_transform_rejects_nonfinite(self):
        data = self._base()
        data["selectedElement"]["transform"]["position"]["x"] = math.inf
        with pytest.raises(ValidationError):
            SpatialReasoningRequest(**data)

    def test_selected_element_kind_restricted_to_object_or_fixture(self):
        data = self._base()
        data["selectedElement"]["kind"] = "wall"
        with pytest.raises(ValidationError):
            SpatialReasoningRequest(**data)

    def test_visual_asset_bound_is_bool_only(self):
        data = self._base()
        data["selectedElement"]["visualAssetBound"] = "yes"
        with pytest.raises(ValidationError):
            SpatialReasoningRequest(**data)
