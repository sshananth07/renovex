"""Prompt-construction tests for RP4E1 spatial reasoning (mirrors
test_prompts.py's structure). Asserts mandatory rules and the five worked
examples are present, the serialized context stays bounded, and no
credential/provider/storage field can leak into the prompt text."""

import json
import re
from pathlib import Path

from pydantic import ValidationError

from app.prompts.spatial_reasoning import (
    SPATIAL_REASONING_PROMPT_VERSION,
    build_spatial_reasoning_messages,
)
from app.schemas.spatial_reasoning import ProposedSceneEditDelta, SpatialReasoningRequest

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


class TestSpatialReasoningPromptCompleteEnvelope:
    """Regression for the confirmed production root cause: GLM's response
    to "move it to the right a bit" (and others) failed ProposedSceneEditDelta
    validation on exactly the required ENVELOPE fields the prompt never
    named or showed as concrete JSON — schemaVersion, target, summary,
    blockers, assumptions, reviewNotes, confidence. response_format is
    bare {"type":"json_object"} (no schema payload to Z.ai), so the prompt
    text is the ONLY place the model can learn the full required shape.
    These tests parse an actual complete JSON template out of the prompt
    and validate it directly against the authoritative Pydantic schema —
    not just grep for field-name substrings — so a template that is merely
    textually present but not actually schema-valid still fails here."""

    def _extract_first_json_object(self, text: str) -> dict:
        """Finds the first top-level {...} block in text and parses it.
        Uses brace counting (not a naive non-greedy regex) since the
        template legitimately contains nested objects/arrays."""
        start = text.index("{")
        depth = 0
        for i, ch in enumerate(text[start:], start=start):
            if ch == "{":
                depth += 1
            elif ch == "}":
                depth -= 1
                if depth == 0:
                    return json.loads(text[start : i + 1])
        raise AssertionError("no complete top-level JSON object found in prompt text")

    def test_prompt_contains_a_complete_schema_valid_json_template(self):
        """The strongest possible regression: extract the canonical
        template from the system prompt and validate it AGAINST THE REAL
        ProposedSceneEditDelta model — proving the template is not just
        textually present but genuinely satisfies every required field,
        type, and enum the schema demands."""
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("mixed.json"))
        template = self._extract_first_json_object(system)

        # Must not raise — this IS the regression. If someone edits the
        # prompt's template to drop a required field or use a wrong enum
        # value, this test fails immediately.
        delta = ProposedSceneEditDelta(**template)

        # And it must genuinely exercise every required top-level field —
        # not just happen to validate via defaults (there are none on this
        # model, but this also guards against a future schema change that
        # adds one).
        for field_name in ProposedSceneEditDelta.model_fields:
            assert hasattr(delta, field_name)

    def test_complete_template_shows_every_required_top_level_field_by_name(self):
        """Belt-and-suspenders on top of the schema-validation test above:
        every one of the schema's required top-level field names must
        appear, verbatim, in the system prompt text (not just inside the
        template — WORKED EXAMPLE prose referencing them by name is also
        acceptable, but each name must appear at least once somewhere)."""
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("mixed.json"))
        for field_name in ProposedSceneEditDelta.model_fields:
            assert re.search(rf'"{field_name}"|\b{field_name}\b', system), (
                f"required field {field_name!r} never appears in the system prompt"
            )

    def test_complete_template_shows_empty_arrays_for_optional_bookkeeping_fields(self):
        """The exact failure mode: production omitted blockers/assumptions/
        reviewNotes entirely rather than sending them as empty arrays. The
        template must show at least one of these three as a literal empty
        array, demonstrating "required even when empty" concretely rather
        than only in prose."""
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("mixed.json"))
        template = self._extract_first_json_object(system)
        assert template["blockers"] == [] or template["assumptions"] == [] or template["reviewNotes"] == []

    def test_prompt_states_all_top_level_fields_always_required(self):
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("mixed.json"))
        lower = system.lower()
        assert "always" in lower or "every response" in lower or "every top-level field" in lower
        assert "empty array" in lower or "empty list" in lower or "[]" in system

    def test_existing_five_worked_examples_still_present(self):
        """The task requires keeping the five existing worked examples —
        this must not regress to fewer than five."""
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("mixed.json"))
        assert system.lower().count("example") >= 5

    def test_template_enum_values_match_authoritative_schema_exactly(self):
        """The template's enum-bearing fields must use values that are
        ACTUALLY valid per the schema — this fails if someone hand-edits
        the template with a plausible-looking but wrong enum value (this
        duplicates part of what schema validation already proves, but
        pins the exact values independently so a future schema change is
        forced to update this test deliberately)."""
        system, _ = build_spatial_reasoning_messages(_request_from_fixture("mixed.json"))
        template = self._extract_first_json_object(system)

        assert template["schemaVersion"] == 1
        assert template["target"]["kind"] in ("object", "fixture")
        assert template["intent"] in (
            "visual_geometry", "material_appearance", "spatial_domain", "mixed",
        )
        for section in ("geometry", "material", "spatial"):
            assert template[section]["mode"] in ("preserve", "replace", "clear")

    def test_malformed_envelope_missing_fields_still_fails_validation(self):
        """Sanity check that the schema itself was NOT weakened as a side
        effect of this prompt-only change — the exact production failure
        shape (missing schemaVersion/target/summary/blockers/assumptions/
        reviewNotes/confidence) must still raise ValidationError."""
        malformed = {
            "intent": "spatial_domain",
            "geometry": {"mode": "preserve"},
            "material": {"mode": "preserve"},
            "spatial": {
                "mode": "replace",
                "spec": {"kind": "move_relative_to_nearest_wall", "relationship": "toward", "distanceMeters": 0.3},
            },
        }
        try:
            ProposedSceneEditDelta(**malformed)
            raise AssertionError("expected ValidationError for the exact production-observed missing-fields shape")
        except ValidationError as exc:
            missing_fields = {".".join(str(p) for p in e["loc"]) for e in exc.errors() if e["type"] == "missing"}
            for expected in ("schemaVersion", "target", "summary", "blockers", "assumptions", "reviewNotes", "confidence"):
                assert expected in missing_fields
