"""Strict schemas for RP4E2 reference-image generation.

Same ConfigDict(extra="forbid", strict=True) fail-closed convention as
spatial_reasoning.py — unknown top-level fields and type coercion both
fail before any provider call. This request NEVER carries arbitrary
RoomDraft JSON, URLs, tenant data, or credentials (RP4E2 plan): it carries
only the bounded fields the reference-image prompt actually needs.
"""

import re
from typing import Annotated, Literal

from pydantic import BaseModel, ConfigDict, Field

_HEX_COLOR_PATTERN = re.compile(r"^#[0-9A-Fa-f]{6}$")


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)


class ReferenceTargetDimensions(StrictModel):
    width: Annotated[float, Field(gt=0, allow_inf_nan=False)]
    height: Annotated[float, Field(gt=0, allow_inf_nan=False)]
    depth: Annotated[float, Field(gt=0, allow_inf_nan=False)]


class ReferenceTarget(StrictModel):
    kind: Literal["object", "fixture"]
    id: Annotated[str, Field(min_length=1, max_length=200)]
    category: Annotated[str, Field(min_length=1, max_length=100)]
    dimensionsMeters: ReferenceTargetDimensions | None = None


class AssetGenerationSpec(StrictModel):
    category: Annotated[str, Field(min_length=1, max_length=100)]
    shapeDescription: Annotated[str, Field(min_length=1, max_length=1000)]
    preserveCanonicalDimensions: bool


class MaterialAppearance(StrictModel):
    baseColor: Annotated[str, Field(pattern=_HEX_COLOR_PATTERN.pattern)]
    materialFamily: Literal["fabric", "leather", "wood", "metal", "stone", "other"]
    roughness: Literal["matte", "satin", "glossy"]
    metallic: bool


class RenderBrief(StrictModel):
    view: Literal["three_quarter_front"]
    isolated: bool
    fullObjectVisible: bool
    background: Literal["plain_warm_white"]
    noText: bool
    noPeople: bool
    noRoom: bool


class ReferenceImageRequest(StrictModel):
    schemaVersion: Literal["1"]
    designSessionId: Annotated[str, Field(min_length=1, max_length=200)]
    turnId: Annotated[str, Field(min_length=1, max_length=200)]
    planFingerprint: Annotated[str, Field(min_length=1, max_length=200)]
    target: ReferenceTarget
    assetGenerationSpec: AssetGenerationSpec
    materialAppearance: MaterialAppearance | None = None
    renderBrief: RenderBrief
    promptVersion: Annotated[str, Field(min_length=1, max_length=50)]
    seed: int


class ReferenceImageResult(StrictModel):
    schemaVersion: Literal["1"]
    imageBase64: Annotated[str, Field(min_length=1)]
    contentType: Literal["image/jpeg", "image/png"]
    width: Annotated[int, Field(gt=0)]
    height: Annotated[int, Field(gt=0)]
    provider: Annotated[str, Field(min_length=1, max_length=50)]
    model: Annotated[str, Field(min_length=1, max_length=100)]
    providerRequestId: Annotated[str, Field(max_length=200)] | None = None
    seed: int
    promptVersion: Annotated[str, Field(min_length=1, max_length=50)]
