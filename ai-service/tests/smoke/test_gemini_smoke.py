"""Real Gemini synthetic smoke test (design doc plan Task 19).

Not part of the normal offline/deterministic regression suite — every test
here is marked `gemini` and self-skips unless GEMINI_API_KEY is actually
set, so `python -m pytest -q` never requires a live credential or network
access. Run explicitly with:

    AI_PROVIDER=gemini GEMINI_API_KEY=<secret> GEMINI_MODEL=<model> \
        python -m pytest -m gemini tests/smoke/test_gemini_smoke.py -q

Only synthetic, non-sensitive project data is used. Assertions are
invariants the real model's output must satisfy (schema validity, referential
integrity against supplied context, absence of forbidden financial/quantity
fields) — never exact suggestion text, since a live model's wording is not
deterministic.
"""

import os

import pytest

from app.config import Settings
from app.providers.gemini import GeminiProvider
from app.schemas.resources import MaterialCandidate, ResourceSuggestionRequest, WorkItemContext
from app.schemas.spaces import ProjectContext, SpaceSuggestionRequest
from app.schemas.work_items import SpaceContext, WorkItemSuggestionRequest

pytestmark = pytest.mark.gemini

_SYNTHETIC_BRIEF = (
    "Full renovation of a synthetic 3-bedroom test condominium. Redo the "
    "kitchen and two bathrooms, replace flooring throughout, and repaint "
    "the whole unit. This is synthetic test data, not a real project."
)


def _require_live_credentials() -> Settings:
    api_key = os.environ.get("GEMINI_API_KEY", "")
    if not api_key:
        pytest.skip(
            "GEMINI_API_KEY not set — real Gemini smoke test skipped. "
            "This means the M8.5B-A completion gate is not yet fully "
            "satisfied; report as pending, not passed."
        )
    return Settings(
        AI_PROVIDER="gemini",
        INTERNAL_API_TOKEN="smoke-test-token",
        GEMINI_API_KEY=api_key,
        GEMINI_MODEL=os.environ.get("GEMINI_MODEL", "gemini-2.5-flash"),
        AI_PROVIDER_MAX_RETRIES=2,
    )


def test_space_suggestions_validate_and_exclude_quantity_cost_fields():
    settings = _require_live_credentials()
    provider = GeminiProvider(settings)

    result = provider.suggest_spaces(
        SpaceSuggestionRequest(
            operationId="smoke-space-1",
            project=ProjectContext(id="smoke-project", scopeBrief=_SYNTHETIC_BRIEF),
            existingSpaces=[],
        )
    )

    assert result.provider == "gemini"
    assert result.model == settings.GEMINI_MODEL
    assert isinstance(result.suggestions, list)
    for suggestion in result.suggestions:
        # extra="forbid" on SpaceSuggestion already makes any quantity/cost/
        # price field a hard validation failure at parse time (design doc
        # §9.2) — reaching this line at all is itself proof no such field
        # was present. This assertion documents the invariant explicitly.
        assert suggestion.name.strip() != ""
        assert suggestion.spaceType.strip() != ""
        assert not hasattr(suggestion, "quantity")
        assert not hasattr(suggestion, "cost")
        assert not hasattr(suggestion, "price")


def test_work_item_suggestions_respect_supplied_space_lineage_and_scope_rules():
    settings = _require_live_credentials()
    provider = GeminiProvider(settings)

    supplied_spaces = [
        SpaceContext(id="smoke-space-kitchen", name="Kitchen", type="kitchen"),
        SpaceContext(id="smoke-space-bathroom", name="Master Bathroom", type="bathroom"),
    ]
    supplied_space_ids = {s.id for s in supplied_spaces}

    result = provider.suggest_work_items(
        WorkItemSuggestionRequest(
            operationId="smoke-workitem-1",
            projectBrief=_SYNTHETIC_BRIEF,
            spaces=supplied_spaces,
            existingWorkItems=[],
        )
    )

    assert result.provider == "gemini"
    assert isinstance(result.suggestions, list)
    valid_scope_origins = {"explicit_scope", "supporting_scope", "possible_missing_scope"}
    for suggestion in result.suggestions:
        assert suggestion.scopeOrigin in valid_scope_origins
        if suggestion.scopeLevel == "space":
            # Go re-validates this lineage independently on acceptance
            # (design doc §15) — this smoke test proves the model itself
            # stays within the context it was given.
            assert suggestion.spaceId in supplied_space_ids, (
                f"space-scoped suggestion referenced spaceId {suggestion.spaceId!r} "
                f"not in supplied set {supplied_space_ids!r}"
            )
        else:
            assert suggestion.spaceId is None
        # No quantity/unit/cost/price field exists on this schema at all
        # (extra="forbid") — contractor-controlled only (design doc §10.6).
        assert not hasattr(suggestion, "quantityValue")
        assert not hasattr(suggestion, "cost")


def test_resource_suggestions_respect_supplied_work_item_and_material_context():
    settings = _require_live_credentials()
    provider = GeminiProvider(settings)

    supplied_work_items = [
        WorkItemContext(id="smoke-workitem-flooring", description="Replace kitchen floor tiles"),
    ]
    supplied_work_item_ids = {w.id for w in supplied_work_items}
    supplied_materials = [
        MaterialCandidate(id="smoke-material-adhesive", name="Tile Adhesive"),
    ]
    supplied_material_ids = {m.id for m in supplied_materials}

    result = provider.suggest_resources(
        ResourceSuggestionRequest(
            operationId="smoke-resource-1",
            workItems=supplied_work_items,
            materialCandidates=supplied_materials,
            existingRequirements=[],
        )
    )

    assert result.provider == "gemini"
    assert isinstance(result.suggestions, list)
    valid_resource_types = {"material", "trade", "equipment"}
    for suggestion in result.suggestions:
        assert suggestion.resourceType in valid_resource_types
        assert suggestion.workItemId in supplied_work_item_ids, (
            f"suggestion referenced workItemId {suggestion.workItemId!r} "
            f"not in supplied set {supplied_work_item_ids!r}"
        )
        if suggestion.candidateMaterialId is not None:
            assert suggestion.candidateMaterialId in supplied_material_ids
        if suggestion.resourceType != "material":
            assert suggestion.candidateMaterialId is None
        # No rate/price/cost/quantity field exists on this schema at all
        # (extra="forbid") — WorkResourceRequirement carries no
        # authoritative cost/quantity data in M8.5B-A (design doc §11.2).
        assert not hasattr(suggestion, "rate")
        assert not hasattr(suggestion, "quantity")
        assert not hasattr(suggestion, "cost")
