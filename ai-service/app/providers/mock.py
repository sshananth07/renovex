"""Deterministic, offline AIProvider used for automated tests and local
development (AI_PROVIDER=mock). Output is a pure function of the request —
no randomness, no wall-clock dependence — so Go/Playwright regressions can
assert exact suggestion shapes."""

from app.schemas.resources import (
    ResourceSuggestion,
    ResourceSuggestionRequest,
    ResourceSuggestionResult,
)
from app.schemas.spaces import SpaceSuggestion, SpaceSuggestionRequest, SpaceSuggestionResult
from app.schemas.work_items import (
    WorkItemSuggestion,
    WorkItemSuggestionRequest,
    WorkItemSuggestionResult,
)

_CANDIDATE_SPACES = [
    ("Kitchen", "kitchen", "Kitchen renovation is explicitly mentioned in the brief.", "explicit", "kitchen"),
    ("Master Bathroom", "bathroom", "Bathroom renovation is explicitly mentioned in the brief.", "explicit", "bathroom"),
    ("Living Area", "living_area", "Flooring/paint work implies the living area is in scope.", "derived", ""),
    (
        "Additional Storage Space",
        "storage",
        "A plausible additional space commonly needed but not directly stated.",
        "possible_missing",
        "",
    ),
]


class MockProvider:
    def suggest_spaces(self, request: SpaceSuggestionRequest) -> SpaceSuggestionResult:
        existing_normalized = {
            (s.name.strip().lower(), s.type.strip().lower()) for s in request.existingSpaces
        }
        brief_lower = request.project.scopeBrief.lower()
        suggestions: list[SpaceSuggestion] = []
        for name, space_type, rationale, evidence_type, excerpt_hint in _CANDIDATE_SPACES:
            if (name.strip().lower(), space_type.strip().lower()) in existing_normalized:
                continue
            # Deterministic fixture never fabricates an excerpt: only claim
            # "explicit" when the hinted keyword genuinely appears in this
            # request's own brief, mirroring the real grounding requirement.
            if evidence_type == "explicit" and excerpt_hint not in brief_lower:
                evidence_type = "derived"
            source_excerpt = excerpt_hint if evidence_type == "explicit" else ""
            suggestions.append(
                SpaceSuggestion(
                    name=name,
                    spaceType=space_type,
                    rationale=rationale,
                    confidence=0.9,
                    evidenceType=evidence_type,
                    sourceExcerpt=source_excerpt,
                )
            )
        return SpaceSuggestionResult(
            provider="mock",
            model="mock-v1",
            promptVersion="spaces-v1",
            schemaVersion=1,
            suggestions=suggestions,
        )

    def suggest_work_items(self, request: WorkItemSuggestionRequest) -> WorkItemSuggestionResult:
        suggestions: list[WorkItemSuggestion] = []
        if request.spaces:
            first_space = request.spaces[0]
            # No material is ever named in this fixture's brief text, so
            # materialSpecificity stays "unspecified" — this deliberately
            # mirrors the regression case: an unspecified-material Work
            # Item must never let downstream Resource generation invent a
            # product-specific material.
            suggestions.append(
                WorkItemSuggestion(
                    description=f"Replace flooring in {first_space.name}",
                    workType="flooring",
                    scopeLevel="space",
                    spaceId=first_space.id,
                    scopeOrigin="explicit_scope",
                    rationale="Explicitly requested in the project brief.",
                    confidence=0.92,
                    sourceExcerpt="flooring",
                    materialSpecificity="unspecified",
                )
            )
            suggestions.append(
                WorkItemSuggestion(
                    description=f"Remove existing fixtures in {first_space.name}",
                    workType="demolition",
                    scopeLevel="space",
                    spaceId=first_space.id,
                    scopeOrigin="supporting_scope",
                    rationale="Needed to support the requested flooring replacement.",
                    confidence=0.8,
                    materialSpecificity="unspecified",
                )
            )
            suggestions.append(
                WorkItemSuggestion(
                    description=f"Make good wall surfaces in {first_space.name}",
                    workType="patching",
                    scopeLevel="space",
                    spaceId=first_space.id,
                    scopeOrigin="possible_missing_scope",
                    rationale="Often required after fixture removal but not explicitly mentioned.",
                    confidence=0.55,
                    materialSpecificity="unspecified",
                )
            )
        suggestions.append(
            WorkItemSuggestion(
                description="Site protection for common areas",
                workType="site_protection",
                scopeLevel="project",
                spaceId=None,
                scopeOrigin="supporting_scope",
                rationale="Standard supporting work for whole-unit renovations.",
                confidence=0.7,
            )
        )
        return WorkItemSuggestionResult(
            provider="mock",
            model="mock-v1",
            promptVersion="work-items-v1",
            schemaVersion=1,
            suggestions=suggestions,
        )

    def suggest_resources(self, request: ResourceSuggestionRequest) -> ResourceSuggestionResult:
        suggestions: list[ResourceSuggestion] = []
        if not request.workItems:
            return ResourceSuggestionResult(
                provider="mock",
                model="mock-v1",
                promptVersion="resources-v1",
                schemaVersion=1,
                suggestions=[],
            )
        first_work_item = request.workItems[0]
        candidate_id = request.materialCandidates[0].id if request.materialCandidates else None
        suggestions.append(
            ResourceSuggestion(
                resourceType="material",
                workItemId=first_work_item.id,
                name="Tile Adhesive",
                candidateMaterialId=candidate_id,
                rationale="Required to install the suggested flooring/tiling.",
                confidence=0.85,
            )
        )
        suggestions.append(
            ResourceSuggestion(
                resourceType="material",
                workItemId=first_work_item.id,
                name="Tile Spacers",
                candidateMaterialId=None,
                rationale="No existing catalog match found.",
                confidence=0.75,
            )
        )
        suggestions.append(
            ResourceSuggestion(
                resourceType="trade",
                workItemId=first_work_item.id,
                name="Tiler",
                candidateMaterialId=None,
                rationale="Trade skill required to perform this work.",
                confidence=0.9,
            )
        )
        suggestions.append(
            ResourceSuggestion(
                resourceType="equipment",
                workItemId=first_work_item.id,
                name="Tile Cutter",
                candidateMaterialId=None,
                rationale="Equipment typically required for this type of work.",
                confidence=0.7,
            )
        )
        return ResourceSuggestionResult(
            provider="mock",
            model="mock-v1",
            promptVersion="resources-v1",
            schemaVersion=1,
            suggestions=suggestions,
        )
