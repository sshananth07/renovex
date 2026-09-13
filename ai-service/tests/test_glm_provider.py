"""GLM-5.3 provider tests. Non-retrying: every failure mode (429, 5xx,
timeout, redirect, truncated/missing content, invalid JSON, invalid schema)
must result in EXACTLY ONE outbound HTTP attempt — never a retry, repair, or
fallback (RP4E1 plan Task 3 Step 1). Invalid LOCAL input (a request that
fails Pydantic validation before any HTTP call) must make ZERO outbound
calls."""

import json
import logging

import httpx
import pytest

from app.errors import (
    InvalidAIRequest,
    InvalidProviderResponse,
    ProviderRateLimited,
    ProviderTimeout,
    ProviderUnavailable,
)
from app.providers.glm import GLMProvider
from app.schemas.spatial_reasoning import SpatialReasoningRequest

FIXTURE_REQUEST = {
    "schemaVersion": 1,
    "turnId": "turn_002",
    "roomDraftId": "roomdraft_001",
    "roomDraftRevision": 17,
    "selectedElement": {
        "kind": "object",
        "id": "object_sofa_123",
        "category": "sofa",
        "transform": {
            "position": {"x": 1.2, "y": 0.0, "z": 2.4},
            "rotation": {"x": 0.0, "y": 0.0, "z": 0.0, "w": 1.0},
        },
        "dimensions": {"x": 2.0, "y": 0.85, "z": 0.95},
        "attachedToWallId": None,
        "visualAssetBound": False,
    },
    "context": {"walls": [], "openings": [], "neighbors": []},
    "currentWorkingDesign": {"geometry": None, "material": None, "resolvedSpatialOperations": []},
    "lastSuccessfulPlanSummary": [],
    "instruction": "Actually make it beige.",
    "allowedSpatialOperations": ["move_relative_to_nearest_wall", "resize_axis"],
    "materialFamilyEnum": ["fabric", "leather", "wood", "metal", "stone", "other"],
    "roughnessEnum": ["matte", "satin", "glossy"],
    "preservationDefaults": {"geometry": True, "material": True, "spatial": True},
}

VALID_DELTA = {
    "schemaVersion": 1,
    "target": {"kind": "object", "id": "object_sofa_123"},
    "intent": "material_appearance",
    "summary": ["Change the sofa upholstery to beige."],
    "geometry": {"mode": "preserve"},
    "material": {
        "mode": "replace",
        "spec": {"baseColor": "#C8A464", "materialFamily": "fabric", "roughness": "matte", "metallic": False},
    },
    "spatial": {"mode": "preserve"},
    "blockers": [],
    "assumptions": [],
    "reviewNotes": [],
    "confidence": 0.91,
}


def _chat_completions_response(content: str, status_code: int = 200) -> httpx.Response:
    body = {"choices": [{"message": {"content": content, "reasoning_content": "secret chain of thought"}}]}
    return httpx.Response(status_code, json=body)


def _provider_with_transport(handler) -> GLMProvider:
    transport = httpx.MockTransport(handler)
    return GLMProvider(
        api_key="test-glm-key",
        model="glm-5.3",
        base_url="https://api.z.ai/api/paas/v4",
        reasoning_effort="low",
        timeout=5.0,
        transport=transport,
    )


class _CallCounter:
    def __init__(self):
        self.count = 0
        self.last_request: httpx.Request | None = None


def test_success_makes_exactly_one_request_and_discards_reasoning_content():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        counter.last_request = request
        return _chat_completions_response(json.dumps(VALID_DELTA))

    provider = _provider_with_transport(handler)
    result = provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

    assert counter.count == 1
    assert result.delta.target.id == "object_sofa_123"
    assert result.provider == "glm"


def test_request_shape_no_redirects_no_tools_bounded_output():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path.endswith("/chat/completions")
        assert request.headers["authorization"] == "Bearer test-glm-key"
        body = json.loads(request.content)
        assert body["model"] == "glm-5.3"
        assert body.get("stream") is not True
        assert "tools" not in body
        assert body["response_format"]["type"] == "json_object"
        return _chat_completions_response(json.dumps(VALID_DELTA))

    provider = _provider_with_transport(handler)
    provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))


def test_429_maps_to_provider_rate_limited_single_attempt():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        return httpx.Response(429, json={"error": "rate limited"})

    provider = _provider_with_transport(handler)
    with pytest.raises(ProviderRateLimited):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_429_rejection_logs_safe_provider_diagnostics(caplog):
    """The diagnostics-only fix: a non-2xx (429) response must safely log
    HTTP status, Z.ai's own error.code/error.message, Retry-After, and a
    provider request id — never the API key, Authorization header, prompt,
    or RoomDraft/context payload. Z.ai's documented error shape is
    {"error": {"code": ..., "message": ...}}."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            429,
            headers={"retry-after": "30", "x-request-id": "req_abc123"},
            json={"error": {"code": "1302", "message": "Rate limit reached for requests"}},
        )

    provider = _provider_with_transport(handler)
    with caplog.at_level(logging.WARNING), pytest.raises(ProviderRateLimited):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

    assert "glm_spatial_provider_rejected" in caplog.text
    assert "http_status=429" in caplog.text
    assert "provider_code=1302" in caplog.text
    assert "Rate limit reached for requests" in caplog.text
    assert "retry_after=30" in caplog.text
    assert "req_abc123" in caplog.text
    assert "test-glm-key" not in caplog.text
    assert "Bearer" not in caplog.text
    assert FIXTURE_REQUEST["instruction"] not in caplog.text


def test_429_with_non_dict_error_shape_logs_safely_without_crashing(caplog):
    """Existing test_429_maps_to_provider_rate_limited_single_attempt uses a
    plain string "error" field (not Z.ai's real {"code","message"} shape) —
    the new diagnostics must degrade to unset fields rather than raising on
    an unexpected/malformed error body."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(429, json={"error": "rate limited"})

    provider = _provider_with_transport(handler)
    with caplog.at_level(logging.WARNING), pytest.raises(ProviderRateLimited):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

    assert "glm_spatial_provider_rejected" in caplog.text
    assert "http_status=429" in caplog.text
    assert "provider_code=None" in caplog.text
    assert "provider_message=None" in caplog.text


def test_5xx_maps_to_provider_unavailable_single_attempt():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        return httpx.Response(503, json={"error": "unavailable"})

    provider = _provider_with_transport(handler)
    with pytest.raises(ProviderUnavailable):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_timeout_maps_to_provider_timeout_single_attempt():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        raise httpx.TimeoutException("timed out")

    provider = _provider_with_transport(handler)
    with pytest.raises(ProviderTimeout):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_redirect_response_is_not_followed_and_maps_to_invalid_response():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        return httpx.Response(302, headers={"Location": "https://evil.example/steal"})

    provider = _provider_with_transport(handler)
    with pytest.raises(InvalidProviderResponse):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_truncated_missing_content_maps_to_invalid_provider_response():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        return httpx.Response(200, json={"choices": [{"message": {}}]})

    provider = _provider_with_transport(handler)
    with pytest.raises(InvalidProviderResponse):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_invalid_json_content_maps_to_invalid_provider_response():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        return _chat_completions_response("not json at all {{{")

    provider = _provider_with_transport(handler)
    with pytest.raises(InvalidProviderResponse):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_invalid_schema_content_maps_to_invalid_provider_response():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        bad = dict(VALID_DELTA)
        bad["unexpectedField"] = "nope"
        return _chat_completions_response(json.dumps(bad))

    provider = _provider_with_transport(handler)
    with pytest.raises(InvalidProviderResponse):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_target_mismatch_maps_to_invalid_provider_response():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        mismatched = dict(VALID_DELTA)
        mismatched["target"] = {"kind": "object", "id": "object_other_999"}
        return _chat_completions_response(json.dumps(mismatched))

    provider = _provider_with_transport(handler)
    with pytest.raises(InvalidProviderResponse):
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))
    assert counter.count == 1


def test_invalid_local_input_makes_zero_calls():
    counter = _CallCounter()

    def handler(request: httpx.Request) -> httpx.Response:
        counter.count += 1
        return _chat_completions_response(json.dumps(VALID_DELTA))

    provider = _provider_with_transport(handler)
    with pytest.raises(InvalidAIRequest):
        provider.reason_element_from_raw({**FIXTURE_REQUEST, "instruction": ""})
    assert counter.count == 0
