"""Provider-neutral orchestration for RP4E1 spatial reasoning, mirroring
services/suggestions.py's own thin-orchestration convention. The provider
(GLMProvider or SpatialMockProvider) already validates its own structural
output and cross-checks the echoed target before returning — this service
adds no further retry/repair; a provider exception propagates unchanged
(RP4E1 plan: "invalid output causes no repair/fallback, provider metadata
is locally trusted rather than model-authored")."""

from typing import Protocol

from app.schemas.spatial_reasoning import SpatialReasoningRequest, SpatialReasoningResult


class SpatialReasoningProvider(Protocol):
    def reason_element(self, request: SpatialReasoningRequest) -> SpatialReasoningResult: ...


class SpatialReasoningService:
    def __init__(self, provider: SpatialReasoningProvider):
        self._provider = provider

    def reason_element(self, request: SpatialReasoningRequest) -> SpatialReasoningResult:
        return self._provider.reason_element(request)
