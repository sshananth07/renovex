"""Orchestration-layer tests: contextual ID membership validation that schema
validation alone cannot express (Task 1 Step 7 / design doc section 15 —
Python validates its own output before returning it to Go, even though Go
revalidates independently)."""

import pytest

from app.errors import InvalidProviderResponse
from app.providers.mock import MockProvider
from app.schemas.resources import ResourceSuggestionRequest, WorkItemContext
from app.schemas.spaces import ProjectContext, SpaceSuggestionRequest
from app.schemas.work_items import SpaceContext, WorkItemSuggestionRequest
from app.services.suggestions import SuggestionService


class _FixedWorkItemsProvider:
    """Provider stub that returns a work item referencing a spaceId NOT in
    the supplied context, to prove the service catches it."""

    def suggest_spaces(self, request):
        return MockProvider().suggest_spaces(request)

    def suggest_work_items(self, request):
        from app.schemas.work_items import WorkItemSuggestion, WorkItemSuggestionResult

        return WorkItemSuggestionResult(
            provider="mock",
            model="mock-v1",
            promptVersion="work-items-v1",
            schemaVersion=1,
            suggestions=[
                WorkItemSuggestion(
                    description="Bogus work item",
                    workType="demolition",
                    scopeLevel="space",
                    spaceId="space_not_supplied",
                    scopeOrigin="explicit_scope",
                )
            ],
        )

    def suggest_resources(self, request):
        return MockProvider().suggest_resources(request)


class _FixedResourcesProvider:
    def suggest_spaces(self, request):
        return MockProvider().suggest_spaces(request)

    def suggest_work_items(self, request):
        return MockProvider().suggest_work_items(request)

    def suggest_resources(self, request):
        from app.schemas.resources import ResourceSuggestion, ResourceSuggestionResult

        return ResourceSuggestionResult(
            provider="mock",
            model="mock-v1",
            promptVersion="resources-v1",
            schemaVersion=1,
            suggestions=[
                ResourceSuggestion(
                    resourceType="material",
                    workItemId="work_not_supplied",
                    name="Bogus Material",
                    candidateMaterialId=None,
                )
            ],
        )


class TestSpaceGeneration:
    def test_valid_request_returns_result(self):
        service = SuggestionService(MockProvider())
        result = service.suggest_spaces(
            SpaceSuggestionRequest(
                operationId="op_1",
                project=ProjectContext(id="p1", scopeBrief="Full renovation."),
                existingSpaces=[],
            )
        )
        assert len(result.suggestions) >= 1


class TestWorkItemGeneration:
    def test_rejects_space_id_outside_supplied_context(self):
        service = SuggestionService(_FixedWorkItemsProvider())
        with pytest.raises(InvalidProviderResponse):
            service.suggest_work_items(
                WorkItemSuggestionRequest(
                    operationId="op_2",
                    projectBrief="Full renovation.",
                    spaces=[SpaceContext(id="space_123", name="Kitchen", type="kitchen")],
                    existingWorkItems=[],
                )
            )

    def test_accepts_valid_space_membership(self):
        service = SuggestionService(MockProvider())
        result = service.suggest_work_items(
            WorkItemSuggestionRequest(
                operationId="op_2",
                projectBrief="Full renovation.",
                spaces=[SpaceContext(id="space_123", name="Kitchen", type="kitchen")],
                existingWorkItems=[],
            )
        )
        for w in result.suggestions:
            if w.scopeLevel == "space":
                assert w.spaceId == "space_123"


class TestResourceGeneration:
    def test_rejects_work_item_id_outside_supplied_context(self):
        service = SuggestionService(_FixedResourcesProvider())
        with pytest.raises(InvalidProviderResponse):
            service.suggest_resources(
                ResourceSuggestionRequest(
                    operationId="op_3",
                    workItems=[WorkItemContext(id="work_1", description="Install tiles")],
                    materialCandidates=[],
                    existingRequirements=[],
                )
            )

    def test_accepts_valid_work_item_membership(self):
        service = SuggestionService(MockProvider())
        result = service.suggest_resources(
            ResourceSuggestionRequest(
                operationId="op_3",
                workItems=[WorkItemContext(id="work_1", description="Install tiles")],
                materialCandidates=[],
                existingRequirements=[],
            )
        )
        for r in result.suggestions:
            assert r.workItemId == "work_1"
