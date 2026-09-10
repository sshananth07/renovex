"""AIProvider abstraction. GeminiProvider and MockProvider both satisfy this
Protocol; orchestration code depends only on this shape, never on a concrete
provider (design doc section 5)."""

from typing import Protocol

from app.schemas.resources import ResourceSuggestionRequest, ResourceSuggestionResult
from app.schemas.spaces import SpaceSuggestionRequest, SpaceSuggestionResult
from app.schemas.spatial_reasoning import SpatialReasoningRequest, SpatialReasoningResult
from app.schemas.work_items import WorkItemSuggestionRequest, WorkItemSuggestionResult


class AIProvider(Protocol):
    def suggest_spaces(self, request: SpaceSuggestionRequest) -> SpaceSuggestionResult: ...

    def suggest_work_items(
        self, request: WorkItemSuggestionRequest
    ) -> WorkItemSuggestionResult: ...

    def suggest_resources(
        self, request: ResourceSuggestionRequest
    ) -> ResourceSuggestionResult: ...


class SpatialReasoningProvider(Protocol):
    """A SEPARATE Protocol from AIProvider (RP4E1) — GLMProvider and
    SpatialMockProvider satisfy this, never AIProvider. The existing
    Copilot AIProvider is not widened to include spatial reasoning; the two
    surfaces stay independent per the RP4E1 design amendment."""

    def reason_element(self, request: SpatialReasoningRequest) -> SpatialReasoningResult: ...
