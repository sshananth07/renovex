"""GeminiProvider adapter tests. The google-genai SDK client is always
mocked/stubbed here — no live network calls in normal tests (design doc
section 34.1)."""

from unittest.mock import MagicMock

import pytest

from app.config import Settings
from app.errors import InvalidProviderResponse
from app.providers.gemini import GeminiProvider
from app.schemas.spaces import (
    ProjectContext,
    SpaceSuggestion,
    SpaceSuggestionRequest,
    SpaceSuggestionResult,
)


def _settings() -> Settings:
    return Settings(
        AI_PROVIDER="gemini",
        INTERNAL_API_TOKEN="test-token",
        GEMINI_API_KEY="test-gemini-key",
        GEMINI_MODEL="gemini-2.0-flash",
        AI_PROVIDER_MAX_RETRIES=2,
    )


def _request() -> SpaceSuggestionRequest:
    return SpaceSuggestionRequest(
        operationId="op_1",
        project=ProjectContext(id="p1", scopeBrief="Full renovation of a condo."),
        existingSpaces=[],
    )


class _JsonTextResponse:
    """Mimics genai's response shape when response_json_schema (a raw JSON
    Schema dict) is used instead of response_schema (a Pydantic class):
    the SDK does not auto-parse into a typed instance for this mode, so
    only response.text (the raw JSON string) is populated/trustworthy.
    response.parsed is deliberately absent/None here — GeminiProvider must
    never rely on it for this code path."""

    def __init__(self, text: str):
        self.text = text
        self.parsed = None


def _valid_space_result_json() -> str:
    return SpaceSuggestionResult(
        provider="ignored-should-be-overwritten",
        model="ignored-should-be-overwritten",
        promptVersion="ignored-should-be-overwritten",
        schemaVersion=999,
        suggestions=[SpaceSuggestion(name="Kitchen", spaceType="kitchen", confidence=0.9)],
    ).model_dump_json()


def test_uses_configured_model_and_requests_json_structured_output_via_json_schema():
    fake_client = MagicMock()
    fake_client.models.generate_content.return_value = _JsonTextResponse(_valid_space_result_json())
    provider = GeminiProvider(_settings(), client=fake_client)

    provider.suggest_spaces(_request())

    call = fake_client.models.generate_content.call_args
    assert call.kwargs["model"] == "gemini-2.0-flash"
    config = call.kwargs["config"]
    assert config.response_mime_type == "application/json"
    # Real Gemini (google-genai==2.18.1) rejects response_schema=<pydantic
    # class> for any model using extra="forbid" with a 400 INVALID_ARGUMENT
    # ("Unknown name \"additional_properties\"" at generation_config.
    # response_schema) — the SDK's Schema-object translation of Pydantic's
    # additionalProperties:false does not survive serialization to Gemini's
    # wire format. response_json_schema (the raw JSON Schema dict) takes a
    # different, schema-validated server-side code path that accepts
    # additionalProperties:false without rejection.
    assert config.response_schema is None
    assert config.response_json_schema == SpaceSuggestionResult.model_json_schema()


def test_validates_response_text_through_the_same_pydantic_model_and_overwrites_governance_fields():
    fake_client = MagicMock()
    fake_client.models.generate_content.return_value = _JsonTextResponse(_valid_space_result_json())
    provider = GeminiProvider(_settings(), client=fake_client)

    result = provider.suggest_spaces(_request())

    assert result.suggestions[0].name == "Kitchen"
    # provider/model/promptVersion/schemaVersion are governance metadata
    # GeminiProvider itself owns and stamps — never trusted from the model's
    # own (possibly hallucinated) output, even though the response JSON
    # included different values for these same fields above.
    assert result.provider == "gemini"
    assert result.model == "gemini-2.0-flash"
    assert result.promptVersion == "spaces-v1"
    assert result.schemaVersion == 1


def test_malformed_json_text_raises_invalid_provider_response():
    fake_client = MagicMock()
    fake_client.models.generate_content.return_value = _JsonTextResponse("not valid json at all")
    provider = GeminiProvider(_settings(), client=fake_client)

    with pytest.raises(InvalidProviderResponse):
        provider.suggest_spaces(_request())


def test_response_text_violating_extraforbid_raises_invalid_provider_response():
    """A model response containing a field the schema doesn't define must
    still hard-fail validation (extra="forbid" preserved end-to-end) even
    though it arrived as raw JSON text rather than SDK-auto-parsed output."""
    fake_client = MagicMock()
    unexpected_field_json = (
        '{"provider":"gemini","model":"gemini-2.0-flash","promptVersion":"spaces-v1",'
        '"schemaVersion":1,"suggestions":[],"unexpectedField":"should not be accepted"}'
    )
    fake_client.models.generate_content.return_value = _JsonTextResponse(unexpected_field_json)
    provider = GeminiProvider(_settings(), client=fake_client)

    with pytest.raises(InvalidProviderResponse):
        provider.suggest_spaces(_request())


def test_empty_response_text_raises_invalid_provider_response():
    fake_client = MagicMock()
    fake_client.models.generate_content.return_value = _JsonTextResponse("")
    provider = GeminiProvider(_settings(), client=fake_client)

    with pytest.raises(InvalidProviderResponse):
        provider.suggest_spaces(_request())


def test_never_performs_markdown_or_regex_extraction():
    import inspect

    import app.providers.gemini as gemini_module

    source = inspect.getsource(gemini_module)
    assert "```" not in source
    assert "re.search" not in source
    assert "re.match" not in source
    assert "re.findall" not in source
