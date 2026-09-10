"""Work Item suggestion request/response schemas.

scopeLevel="space" requires spaceId; scopeLevel="project" requires
spaceId=None (design doc section 10.4). Membership of spaceId in the
supplied Space set is an orchestration-layer concern, not a schema concern,
since it is contextual rather than syntactic.

No quantity/unit/cost/price fields exist anywhere in this schema — WorkItem
quantity/unit remain contractor-controlled and are supplied only at Go
acceptance time (design doc section 10.6).
"""

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

from app.schemas.common import GenerationMetadata


class SpaceContext(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    type: str = Field(min_length=1)


class ExistingWorkItem(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    spaceId: str | None = None
    description: str = Field(min_length=1)
    workType: str = ""


class WorkItemSuggestionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    operationId: str = Field(min_length=1)
    projectBrief: str
    spaces: list[SpaceContext] = Field(default_factory=list)
    existingWorkItems: list[ExistingWorkItem] = Field(default_factory=list)


ScopeLevel = Literal["space", "project"]
ScopeOrigin = Literal["explicit_scope", "supporting_scope", "possible_missing_scope"]
MaterialSpecificity = Literal["explicit", "inferred", "unspecified"]


class WorkItemSuggestion(BaseModel):
    model_config = ConfigDict(extra="forbid")

    description: str = Field(min_length=1)
    workType: str = ""
    scopeLevel: ScopeLevel
    spaceId: str | None = None
    scopeOrigin: ScopeOrigin
    rationale: str = ""
    confidence: float | None = Field(default=None, ge=0.0, le=1.0)
    sourceExcerpt: str = ""
    materialSpecificity: MaterialSpecificity = "unspecified"

    @model_validator(mode="after")
    def _validate_space_linkage(self) -> "WorkItemSuggestion":
        if self.scopeLevel == "space" and not self.spaceId:
            raise ValueError("scopeLevel=space requires a non-null spaceId")
        if self.scopeLevel == "project" and self.spaceId is not None:
            raise ValueError("scopeLevel=project requires spaceId to be null")
        return self


class WorkItemSuggestionResult(GenerationMetadata):
    suggestions: list[WorkItemSuggestion]
