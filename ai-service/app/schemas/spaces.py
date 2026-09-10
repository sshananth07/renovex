"""Space suggestion request/response schemas.

Per design doc section 9.2, the schema intentionally excludes dimensions,
floor areas, measurements, quantities, costs, material requirements, and
financial values — extra="forbid" on every model makes any such field a hard
validation failure rather than a silently-ignored one.
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from app.schemas.common import GenerationMetadata

EvidenceType = Literal["explicit", "derived", "possible_missing"]


class ProjectContext(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    scopeBrief: str


class ExistingSpace(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    type: str = Field(min_length=1)


class SpaceSuggestionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    operationId: str = Field(min_length=1)
    project: ProjectContext
    existingSpaces: list[ExistingSpace] = Field(default_factory=list)
    # Set only for Go's single bounded repair call (T1.5 section 9) — asks
    # the provider to repair specific coverage omissions from a prior
    # generation, never a second independent generation. Empty on every
    # normal call.
    repairInstruction: str = ""


class SpaceSuggestion(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1)
    spaceType: str = Field(min_length=1)
    rationale: str = ""
    confidence: float | None = Field(default=None, ge=0.0, le=1.0)
    evidenceType: EvidenceType = "possible_missing"
    sourceExcerpt: str = ""


class SpaceSuggestionResult(GenerationMetadata):
    suggestions: list[SpaceSuggestion]
