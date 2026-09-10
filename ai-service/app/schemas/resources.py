"""Resource suggestion request/response schemas.

candidateMaterialId is advisory only and permitted solely on material
suggestions (design doc section 12.3-12.4). No rate/price/cost/quantity
field exists anywhere in this schema — WorkResourceRequirement carries no
authoritative cost/quantity data in M8.5B-A (design doc section 11.2).
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from app.schemas.common import GenerationMetadata


class WorkItemContext(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    description: str = Field(min_length=1)


class MaterialCandidate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    name: str = Field(min_length=1)


class ExistingRequirement(BaseModel):
    model_config = ConfigDict(extra="forbid")

    workItemId: str = Field(min_length=1)
    resourceType: str = Field(min_length=1)
    name: str = Field(min_length=1)


class ResourceSuggestionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    operationId: str = Field(min_length=1)
    workItems: list[WorkItemContext] = Field(default_factory=list)
    materialCandidates: list[MaterialCandidate] = Field(default_factory=list)
    existingRequirements: list[ExistingRequirement] = Field(default_factory=list)


ResourceType = Literal["material", "trade", "equipment"]


class ResourceSuggestion(BaseModel):
    model_config = ConfigDict(extra="forbid")

    resourceType: ResourceType
    workItemId: str = Field(min_length=1)
    name: str = Field(min_length=1)
    candidateMaterialId: str | None = None
    rationale: str = ""
    confidence: float | None = Field(default=None, ge=0.0, le=1.0)

    @model_validator(mode="after")
    def _validate_candidate_material(self) -> "ResourceSuggestion":
        if self.resourceType != "material" and self.candidateMaterialId is not None:
            raise ValueError("candidateMaterialId is only valid for resourceType=material")
        return self


class ResourceSuggestionResult(GenerationMetadata):
    suggestions: list[ResourceSuggestion]
