"""Strict schemas for RP4E1 conversational spatial design reasoning.

Every model uses ConfigDict(extra="forbid", strict=True) so unknown fields
and type coercion both fail closed at the Python boundary (design doc
"Structured Output" carried forward from the suggestion schemas; RP4E1 plan
"All Pydantic models use ConfigDict(extra='forbid', strict=True,
allow_inf_nan=False)"). allow_inf_nan=False is set on every float Field so
NaN/Infinity — whether emitted as a raw JSON token or a numeric-looking
string — is rejected rather than silently accepted or coerced; strict mode
already refuses the string form on its own, allow_inf_nan blocks the
numeric-token form.

Section mode/spec consistency (preserve/clear forbid a spec; replace
requires one) is enforced by a model_validator on each *Change model — this
is what makes "keep it," "instead," and "return to the original
shape/material/position" deterministic per the plan's wire contract.

Spatial operations are a CLOSED discriminated union (only
move_relative_to_nearest_wall and resize_axis exist in RP4E1) — a model
requesting a different kind, or emitting direct coordinates, fails schema
validation rather than being coerced into an operation Go was never told
to expect.
"""

import re
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

_SHORT_TEXT_MAX = 240
_HEX_COLOR_PATTERN = re.compile(r"^#[0-9A-Fa-f]{6}$")


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


ShortText = Annotated[str, Field(min_length=1, max_length=_SHORT_TEXT_MAX)]


class SelectedTargetRef(StrictModel):
    kind: Literal["object", "fixture"]
    id: Annotated[str, Field(min_length=1, max_length=200)]


# --- Geometry section ---


class GeometrySpec(StrictModel):
    category: Annotated[str, Field(min_length=1, max_length=100)]
    shapeDescription: Annotated[str, Field(min_length=1, max_length=1000)]
    preserveCanonicalDimensions: bool


class GeometryChange(StrictModel):
    mode: Literal["preserve", "replace", "clear"]
    spec: GeometrySpec | None = None

    @model_validator(mode="after")
    def _check_spec_matches_mode(self) -> "GeometryChange":
        _validate_mode_spec(self.mode, self.spec)
        return self


# --- Material section ---


class MaterialSpec(StrictModel):
    # Canonical sRGB hex (#RRGGBB) — RP4E2/M8.5C amendment (repository
    # findings row 4): the renderer needs deterministic color data, not a
    # free-form name like "dark green". Go revalidates this independently
    # (designplan.go's ValidateBaseColor) since a structurally valid-but-
    # adversarial provider response is never trusted twice by the same code.
    baseColor: Annotated[str, Field(pattern=_HEX_COLOR_PATTERN.pattern)]
    materialFamily: Literal["fabric", "leather", "wood", "metal", "stone", "other"]
    roughness: Literal["matte", "satin", "glossy"]
    metallic: bool


class MaterialChange(StrictModel):
    mode: Literal["preserve", "replace", "clear"]
    spec: MaterialSpec | None = None

    @model_validator(mode="after")
    def _check_spec_matches_mode(self) -> "MaterialChange":
        _validate_mode_spec(self.mode, self.spec)
        return self


# --- Spatial section: closed discriminated union ---


class MoveRelativeToNearestWallSpec(StrictModel):
    kind: Literal["move_relative_to_nearest_wall"]
    relationship: Literal["away_from", "toward"]
    distanceMeters: Annotated[float, Field(gt=0, le=2, allow_inf_nan=False)]


class ResizeAxisSpec(StrictModel):
    kind: Literal["resize_axis"]
    axis: Literal["x", "y", "z"]
    deltaMeters: Annotated[float, Field(allow_inf_nan=False)] | None = None
    targetMeters: Annotated[float, Field(gt=0, allow_inf_nan=False)] | None = None

    @model_validator(mode="after")
    def _exactly_one_of_delta_or_target(self) -> "ResizeAxisSpec":
        if (self.deltaMeters is None) == (self.targetMeters is None):
            raise ValueError("resize_axis requires exactly one of deltaMeters or targetMeters")
        return self


SpatialSpec = Annotated[
    MoveRelativeToNearestWallSpec | ResizeAxisSpec,
    Field(discriminator="kind"),
]


class SpatialChange(StrictModel):
    mode: Literal["preserve", "replace", "clear"]
    spec: SpatialSpec | None = None

    @model_validator(mode="after")
    def _check_spec_matches_mode(self) -> "SpatialChange":
        _validate_mode_spec(self.mode, self.spec)
        return self


def _validate_mode_spec(mode: str, spec: object | None) -> None:
    if mode == "replace" and spec is None:
        raise ValueError("mode='replace' requires a spec")
    if mode in ("preserve", "clear") and spec is not None:
        raise ValueError(f"mode={mode!r} must not carry a spec")


class ProposedBlocker(StrictModel):
    code: Literal["unsupported_operation", "spatially_blocked"]
    message: ShortText


class ProposedSceneEditDelta(StrictModel):
    schemaVersion: Literal[1]
    target: SelectedTargetRef
    intent: Literal["visual_geometry", "material_appearance", "spatial_domain", "mixed"]
    summary: Annotated[list[ShortText], Field(min_length=1, max_length=8)]
    geometry: GeometryChange
    material: MaterialChange
    spatial: SpatialChange
    blockers: Annotated[list[ProposedBlocker], Field(max_length=8)]
    assumptions: Annotated[list[ShortText], Field(max_length=8)]
    reviewNotes: Annotated[list[ShortText], Field(max_length=8)]
    confidence: Annotated[float, Field(ge=0, le=1, allow_inf_nan=False)]

    @model_validator(mode="after")
    def _check_intent_matches_sections(self) -> "ProposedSceneEditDelta":
        replaced = {
            "geometry": self.geometry.mode == "replace",
            "material": self.material.mode == "replace",
            "spatial": self.spatial.mode == "replace",
        }
        # A material-only turn must not also replace geometry or spatial —
        # otherwise "Actually make it beige" and "reshape it" would be
        # indistinguishable at the intent level (plan's approved
        # turn-local/cumulative execution-flag amendment depends on this).
        single_section_intents = {
            "visual_geometry": "geometry",
            "material_appearance": "material",
            "spatial_domain": "spatial",
        }
        if self.intent in single_section_intents:
            only_section = single_section_intents[self.intent]
            for section, is_replaced in replaced.items():
                if section != only_section and is_replaced:
                    raise ValueError(
                        f"intent={self.intent!r} must not replace section {section!r}"
                    )
        return self


# --- Internal request context (Go -> Python) ---


class RoomLocalPointModel(StrictModel):
    x: Annotated[float, Field(allow_inf_nan=False)]
    y: Annotated[float, Field(allow_inf_nan=False)]
    z: Annotated[float, Field(allow_inf_nan=False)]


class RoomLocalQuaternionModel(StrictModel):
    x: Annotated[float, Field(allow_inf_nan=False)]
    y: Annotated[float, Field(allow_inf_nan=False)]
    z: Annotated[float, Field(allow_inf_nan=False)]
    w: Annotated[float, Field(allow_inf_nan=False)]


class RoomLocalTransformModel(StrictModel):
    position: RoomLocalPointModel
    rotation: RoomLocalQuaternionModel


class SelectedElementContext(StrictModel):
    kind: Literal["object", "fixture"]
    id: Annotated[str, Field(min_length=1, max_length=200)]
    category: Annotated[str, Field(min_length=1, max_length=100)]
    transform: RoomLocalTransformModel
    dimensions: RoomLocalPointModel | None = None
    attachedToWallId: Annotated[str, Field(min_length=1, max_length=200)] | None = None
    visualAssetBound: bool


class ContextWall(StrictModel):
    id: Annotated[str, Field(min_length=1, max_length=200)]
    start: RoomLocalPointModel
    end: RoomLocalPointModel
    thickness: Annotated[float, Field(gt=0, allow_inf_nan=False)] | None = None


class DesignReasoningNeighborhood(StrictModel):
    walls: Annotated[list[ContextWall], Field(max_length=8)]
    openings: Annotated[list[dict], Field(max_length=8)] = Field(default_factory=list)
    neighbors: Annotated[list[dict], Field(max_length=16)] = Field(default_factory=list)


class WorkingDesign(StrictModel):
    geometry: GeometrySpec | None = None
    material: MaterialSpec | None = None
    resolvedSpatialOperations: Annotated[list[dict], Field(max_length=8)] = Field(default_factory=list)


class PreservationDefaults(StrictModel):
    geometry: bool
    material: bool
    spatial: bool


class SpatialReasoningRequest(StrictModel):
    schemaVersion: Literal[1]
    turnId: Annotated[str, Field(min_length=1, max_length=200)]
    roomDraftId: Annotated[str, Field(min_length=1, max_length=200)]
    roomDraftRevision: Annotated[int, Field(ge=0)]
    selectedElement: SelectedElementContext
    context: DesignReasoningNeighborhood
    currentWorkingDesign: WorkingDesign
    lastSuccessfulPlanSummary: Annotated[list[ShortText], Field(max_length=8)]
    instruction: Annotated[str, Field(min_length=1, max_length=2000)]
    allowedSpatialOperations: Annotated[
        list[Literal["move_relative_to_nearest_wall", "resize_axis"]],
        Field(min_length=1, max_length=2),
    ]
    materialFamilyEnum: Annotated[list[str], Field(min_length=1, max_length=16)]
    roughnessEnum: Annotated[list[str], Field(min_length=1, max_length=16)]
    preservationDefaults: PreservationDefaults


class SpatialReasoningResult(StrictModel):
    delta: ProposedSceneEditDelta
    provider: Annotated[str, Field(min_length=1, max_length=50)]
    model: Annotated[str, Field(min_length=1, max_length=100)]
    promptVersion: Annotated[str, Field(min_length=1, max_length=50)]
    schemaVersion: Literal[1]
