"""Prompt-construction tests for RP4E1 spatial reasoning (mirrors
test_prompts.py's structure). Asserts mandatory rules and the five worked
examples are present, the serialized context stays bounded, and no
credential/provider/storage field can leak into the prompt text."""

import json
from pathlib import Path

from app.prompts.spatial_reasoning import (
    SPATIAL_REASONING_PROMPT_VERSION,
    build_spatial_reasoning_messages,
)
from app.schemas.spatial_reasoning import SpatialReasoningRequest

FIXTURES_DIR = Path(__file__).parent / "fixtures" / "spatial_reasoning"


def _request_from_fixture(name: str) -> SpatialReasoningRequest:
    data = json.loads((FIXTURES_DIR / name).read_text())
    return SpatialReasoningRequest(**data["request"])


class TestSpatialReasoningPrompt:
    def test_version_constant(self):
        assert SPATIAL_REASONING_PROMPT_VERSION == "spatial-reasoning-v1"

    def test_requests_exactly_one_json_object(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("material_only.json"))
        lower = system.lower()
        assert "one json object" in lower or "exactly one json" in lower

    def test_forbids_markdown_or_prose_wrapping(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("material_only.json"))
        lower = system.lower()
        assert "markdown" in lower
        assert "no other text" in lower or "nothing else" in lower or "only the json" in lower

    def test_instructs_preserve_unchanged_sections(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("material_only.json"))
        assert "preserve" in system.lower()

    def test_instructs_use_only_supplied_ids_and_operations(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("spatial_only.json"))
        lower = system.lower()
        assert "allowed" in lower or "supplied" in lower

    def test_instructs_no_direct_coordinates(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("spatial_only.json"))
        lower = system.lower()
        assert "coordinate" in lower

    def test_instructs_blockers_for_unsupported_work(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("unsupported_structural.json"))
        assert "blocker" in system.lower()

    def test_contains_five_worked_examples(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("mixed.json"))
        # five semantic cases from Task 1's fixtures: material-only,
        # geometry-only, spatial-only, mixed, unsupported-structural.
        assert system.lower().count("example") >= 5

    def test_user_message_contains_bounded_context(self):
        request = _request_from_fixture("mixed.json")
        _, user = build_spatial_reasoning_messages(request)
        assert request.instruction in user
        assert request.selectedElement.id in user

    def test_serialized_context_is_bounded(self):
        request = _request_from_fixture("mixed.json")
        _, user = build_spatial_reasoning_messages(request)
        # Sanity bound: the user context payload must not balloon into an
        # unbounded full-room dump (design spec §context minimization).
        assert len(user) < 20_000

    def test_no_credential_or_storage_field_can_enter_prompt(self):
        request = _request_from_fixture("mixed.json")
        system, user = build_spatial_reasoning_messages(request)
        combined_lower = (system + user).lower()
        for forbidden in ("api_key", "token", "storagekey", "bearer", "authorization"):
            assert forbidden not in combined_lower
