"""Provider transient-error mapping and bounded-retry tests.

Covers: 429 -> PROVIDER_RATE_LIMITED, timeout -> PROVIDER_TIMEOUT, 5xx ->
PROVIDER_UNAVAILABLE, schema/parse failure -> INVALID_PROVIDER_RESPONSE,
auth/config failure -> PROVIDER_UNAVAILABLE without leaking the key. At most
AI_PROVIDER_MAX_RETRIES retries, and no retry for invalid request/schema
failures (design doc section 22.1 / plan Task 2 Step 5-6)."""

from unittest.mock import MagicMock

import httpx
import pytest
from google.genai import errors as genai_errors

from app.config import Settings
from app.errors import (
    InvalidProviderResponse,
    ProviderRateLimited,
    ProviderTimeout,
    ProviderUnavailable,
)
from app.providers.gemini import GeminiProvider
from app.schemas.spaces import (
    ProjectContext,
    SpaceSuggestion,
    SpaceSuggestionRequest,
    SpaceSuggestionResult,
)


def _settings(max_retries: int = 2) -> Settings:
    return Settings(
        AI_PROVIDER="gemini",
        INTERNAL_API_TOKEN="test-token",
        GEMINI_API_KEY="test-gemini-key",
        GEMINI_MODEL="gemini-2.0-flash",
        AI_PROVIDER_MAX_RETRIES=max_retries,
    )


def _request() -> SpaceSuggestionRequest:
    return SpaceSuggestionRequest(
        operationId="op_1",
        project=ProjectContext(id="p1", scopeBrief="Full renovation of a condo."),
        existingSpaces=[],
    )


class _ParsedSpacesResponse:
    """response.parsed is never read by GeminiProvider (see gemini.py's
    module docstring) — only response.text, explicitly validated through
    the Pydantic response model via model_validate_json."""

    def __init__(self, text: str | None):
        self.parsed = None
        self.text = text


def _client_error(code: int) -> genai_errors.APIError:
    return genai_errors.ClientError(code, {"error": {"message": "boom", "status": "ERR"}})


def _server_error(code: int) -> genai_errors.APIError:
    return genai_errors.ServerError(code, {"error": {"message": "boom", "status": "ERR"}})


def test_rate_limit_maps_to_provider_rate_limited():
    fake_client = MagicMock()
    fake_client.models.generate_content.side_effect = _client_error(429)
    provider = GeminiProvider(_settings(max_retries=0), client=fake_client)

    with pytest.raises(ProviderRateLimited):
        provider.suggest_spaces(_request())


def test_timeout_maps_to_provider_timeout():
    fake_client = MagicMock()
    fake_client.models.generate_content.side_effect = httpx.TimeoutException("timed out")
    provider = GeminiProvider(_settings(max_retries=0), client=fake_client)

    with pytest.raises(ProviderTimeout):
        provider.suggest_spaces(_request())


def test_server_5xx_maps_to_provider_unavailable():
    fake_client = MagicMock()
    fake_client.models.generate_content.side_effect = _server_error(503)
    provider = GeminiProvider(_settings(max_retries=0), client=fake_client)

    with pytest.raises(ProviderUnavailable):
        provider.suggest_spaces(_request())


def test_schema_parse_failure_maps_to_invalid_provider_response():
    fake_client = MagicMock()
    fake_client.models.generate_content.return_value = _ParsedSpacesResponse(None)
    provider = GeminiProvider(_settings(max_retries=0), client=fake_client)

    with pytest.raises(InvalidProviderResponse):
        provider.suggest_spaces(_request())


def test_auth_failure_maps_to_provider_unavailable_without_leaking_key():
    fake_client = MagicMock()
    fake_client.models.generate_content.side_effect = _client_error(401)
    provider = GeminiProvider(_settings(max_retries=0), client=fake_client)

    with pytest.raises(ProviderUnavailable) as excinfo:
        provider.suggest_spaces(_request())
    assert "test-gemini-key" not in str(excinfo.value)


def test_retries_up_to_max_on_transient_server_error():
    fake_client = MagicMock()
    fake_client.models.generate_content.side_effect = _server_error(503)
    provider = GeminiProvider(_settings(max_retries=2), client=fake_client)

    with pytest.raises(ProviderUnavailable):
        provider.suggest_spaces(_request())

    assert fake_client.models.generate_content.call_count == 3  # 1 initial + 2 retries


def test_no_retry_on_invalid_provider_response():
    fake_client = MagicMock()
    fake_client.models.generate_content.return_value = _ParsedSpacesResponse(None)
    provider = GeminiProvider(_settings(max_retries=2), client=fake_client)

    with pytest.raises(InvalidProviderResponse):
        provider.suggest_spaces(_request())

    assert fake_client.models.generate_content.call_count == 1


def test_no_retry_on_client_400_invalid_request():
    fake_client = MagicMock()
    fake_client.models.generate_content.side_effect = _client_error(400)
    provider = GeminiProvider(_settings(max_retries=2), client=fake_client)

    with pytest.raises(ProviderUnavailable):
        provider.suggest_spaces(_request())

    assert fake_client.models.generate_content.call_count == 1


def test_succeeds_after_one_transient_failure():
    fake_client = MagicMock()
    ok = _ParsedSpacesResponse(
        SpaceSuggestionResult(
            provider="gemini", model="gemini-2.0-flash", promptVersion="spaces-v1", schemaVersion=1,
            suggestions=[SpaceSuggestion(name="Kitchen", spaceType="kitchen")],
        ).model_dump_json()
    )
    fake_client.models.generate_content.side_effect = [_server_error(503), ok]
    provider = GeminiProvider(_settings(max_retries=2), client=fake_client)

    result = provider.suggest_spaces(_request())

    assert result.suggestions[0].name == "Kitchen"
    assert fake_client.models.generate_content.call_count == 2


class TestGLMProviderNeverRetries:
    """RP4E1's GLMProvider deliberately does NOT share GeminiProvider's
    bounded-retry helper — every transient failure (503 here) makes exactly
    one attempt, unlike Gemini's AI_PROVIDER_MAX_RETRIES-bounded retry loop
    proven above. See test_glm_provider.py for the full failure-mode
    matrix; this test exists specifically to contrast the two providers'
    retry policies side by side in one file."""

    def test_transient_5xx_is_not_retried_unlike_gemini(self):
        import httpx

        from app.errors import ProviderUnavailable
        from app.providers.glm import GLMProvider
        from app.schemas.spatial_reasoning import SpatialReasoningRequest

        calls = {"count": 0}

        def handler(request: httpx.Request) -> httpx.Response:
            calls["count"] += 1
            return httpx.Response(503, json={"error": "unavailable"})

        provider = GLMProvider(
            api_key="test-key", model="glm-5.3", base_url="https://api.z.ai/api/paas/v4",
            reasoning_effort="low", timeout=5.0, transport=httpx.MockTransport(handler),
        )
        request = SpatialReasoningRequest(schemaVersion=1, turnId="t1", roomDraftId="rd1", roomDraftRevision=1, selectedElement={
                "kind": "object", "id": "o1", "category": "sofa",
                "transform": {"position": {"x": 0, "y": 0, "z": 0}, "rotation": {"x": 0, "y": 0, "z": 0, "w": 1}},
                "visualAssetBound": False,
            }, context={"walls": [], "openings": [], "neighbors": []}, currentWorkingDesign={"resolvedSpatialOperations": []}, lastSuccessfulPlanSummary=[], instruction="make it beige", allowedSpatialOperations=["resize_axis"], materialFamilyEnum=["fabric"], roughnessEnum=["matte"], preservationDefaults={"geometry": True, "material": True, "spatial": True})

        with pytest.raises(ProviderUnavailable):
            provider.reason_element(request)

        assert calls["count"] == 1  # never 2+, unlike Gemini's retry loop
