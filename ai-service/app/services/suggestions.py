"""Provider-neutral orchestration. Validates contextual ID membership before
and after calling the provider — routes contain no provider-specific code
(design doc section 15, "Go Domain Revalidation": Python validates its own
output first; Go revalidates independently afterward, since Python output is
untrusted from the business-domain perspective too)."""

from app.errors import InvalidProviderResponse
from app.providers.base import AIProvider
from app.schemas.resources import ResourceSuggestionRequest, ResourceSuggestionResult
from app.schemas.spaces import SpaceSuggestionRequest, SpaceSuggestionResult
from app.schemas.work_items import WorkItemSuggestionRequest, WorkItemSuggestionResult


class SuggestionService:
    def __init__(self, provider: AIProvider):
        self._provider = provider

    def suggest_spaces(self, request: SpaceSuggestionRequest) -> SpaceSuggestionResult:
        return self._provider.suggest_spaces(request)

    def suggest_work_items(self, request: WorkItemSuggestionRequest) -> WorkItemSuggestionResult:
        result = self._provider.suggest_work_items(request)
        valid_space_ids = {s.id for s in request.spaces}
        for suggestion in result.suggestions:
            if suggestion.scopeLevel == "space" and suggestion.spaceId not in valid_space_ids:
                raise InvalidProviderResponse(
                    f"work item suggestion references unsupplied spaceId {suggestion.spaceId!r}"
                )
        return result

    def suggest_resources(self, request: ResourceSuggestionRequest) -> ResourceSuggestionResult:
        result = self._provider.suggest_resources(request)
        valid_work_item_ids = {w.id for w in request.workItems}
        valid_candidate_ids = {c.id for c in request.materialCandidates}
        for suggestion in result.suggestions:
            if suggestion.workItemId not in valid_work_item_ids:
                raise InvalidProviderResponse(
                    f"resource suggestion references unsupplied workItemId {suggestion.workItemId!r}"
                )
            if suggestion.candidateMaterialId and suggestion.candidateMaterialId not in valid_candidate_ids:
                raise InvalidProviderResponse(
                    f"resource suggestion references unsupplied candidateMaterialId "
                    f"{suggestion.candidateMaterialId!r}"
                )
        return result
