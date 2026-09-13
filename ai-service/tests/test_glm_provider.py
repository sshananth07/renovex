"""GLM-5.3 provider tests. Non-retrying for almost every failure mode (a
different 429 reason, 5xx, timeout, redirect, truncated/missing content,
invalid JSON, invalid schema) — EXACTLY ONE outbound HTTP attempt, never a
retry, repair, or fallback (RP4E1 plan Task 3 Step 1). Invalid LOCAL input
(a request that fails Pydantic validation before any HTTP call) must make
ZERO outbound calls. The ONE exception is Z.ai's own transient-overload
signal (HTTP 429, error.code=="1305"), which retries up to twice more (3
attempts total, bounded backoff) — see the retry-specific tests below."""

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
from app.providers.glm import _BACKOFF_JITTER_SECONDS, GLMProvider
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


def _overload_response(retry_after: str | None = None) -> httpx.Response:
    headers = {"retry-after": retry_after} if retry_after is not None else {}
    return httpx.Response(
        429,
        headers=headers,
        json={"error": {"code": "1305", "message": "The service may be temporarily overloaded, please try again later"}},
    )


class TestTransientOverloadRetry:
    """RP4E1 amendment: HTTP 429 with error.code=="1305" (Z.ai's own
    transient-overload signal) retries up to twice more (3 attempts total,
    ~1s then ~2s backoff plus jitter, or Retry-After if present) — the ONE
    exception to this provider's otherwise-exactly-one-attempt rule. Every
    test here patches time.sleep so the bounded backoff never actually
    slows the suite down."""

    def test_1305_then_200_succeeds_after_one_retry(self, monkeypatch):
        sleep_calls: list[float] = []
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: sleep_calls.append(s))

        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            if counter.count == 1:
                return _overload_response()
            return _chat_completions_response(json.dumps(VALID_DELTA))

        provider = _provider_with_transport(handler)
        result = provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert counter.count == 2
        assert result.delta.target.id == "object_sofa_123"
        assert len(sleep_calls) == 1
        assert 1.0 <= sleep_calls[0] <= 1.0 + _BACKOFF_JITTER_SECONDS + 1e-6

    def test_1305_1305_then_200_succeeds_after_two_retries(self, monkeypatch):
        sleep_calls: list[float] = []
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: sleep_calls.append(s))

        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            if counter.count <= 2:
                return _overload_response()
            return _chat_completions_response(json.dumps(VALID_DELTA))

        provider = _provider_with_transport(handler)
        result = provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert counter.count == 3
        assert result.delta.target.id == "object_sofa_123"
        assert len(sleep_calls) == 2
        assert 1.0 <= sleep_calls[0] <= 1.0 + _BACKOFF_JITTER_SECONDS + 1e-6
        assert 2.0 <= sleep_calls[1] <= 2.0 + _BACKOFF_JITTER_SECONDS + 1e-6

    def test_1305_three_times_exhausts_retries_and_raises_provider_rate_limited(self, monkeypatch, caplog):
        """All attempts exhausted with the transient-overload signal every
        time must still raise ProviderRateLimited — the SAME exception a
        single non-retried 429 always raised — so Go's existing
        needs_attention/design_reasoning_provider_rejected terminal
        classification is completely unchanged; only the number of
        attempts before reaching it grew."""
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: None)

        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            return _overload_response()

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING), pytest.raises(ProviderRateLimited):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert counter.count == 3
        assert caplog.text.count("provider_code=1305") == 3

    def test_quota_style_429_does_not_retry(self, monkeypatch):
        """A DIFFERENT error.code at the same HTTP 429 (e.g. a quota/
        balance/entitlement rejection) must never retry — retrying cannot
        help against an exhausted quota and must not burn extra attempts."""
        sleep_calls: list[float] = []
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: sleep_calls.append(s))

        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            return httpx.Response(429, json={"error": {"code": "1113", "message": "Insufficient balance"}})

        provider = _provider_with_transport(handler)
        with pytest.raises(ProviderRateLimited):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert counter.count == 1
        assert sleep_calls == []

    @pytest.mark.parametrize("status", [401, 403])
    def test_401_and_403_do_not_retry(self, monkeypatch, status):
        sleep_calls: list[float] = []
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: sleep_calls.append(s))
        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            return httpx.Response(status, json={"error": {"code": "auth_error", "message": "unauthorized"}})

        provider = _provider_with_transport(handler)
        with pytest.raises(ProviderUnavailable):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert counter.count == 1
        assert sleep_calls == []

    def test_malformed_200_response_does_not_trigger_provider_retry(self, monkeypatch):
        """A 200 with unparseable/invalid structured content is a LOCAL
        (Go-independent) validation failure on the single response already
        received — it must never re-enter the HTTP retry loop, since the
        overload retry exists only for the transport/429-overload step,
        never for output validation."""
        sleep_calls: list[float] = []
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: sleep_calls.append(s))

        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            return _chat_completions_response("not valid json {{{")

        provider = _provider_with_transport(handler)
        with pytest.raises(InvalidProviderResponse):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert counter.count == 1
        assert sleep_calls == []

    def test_retry_after_header_takes_precedence_over_fixed_backoff(self, monkeypatch):
        sleep_calls: list[float] = []
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: sleep_calls.append(s))

        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            if counter.count == 1:
                return _overload_response(retry_after="5")
            return _chat_completions_response(json.dumps(VALID_DELTA))

        provider = _provider_with_transport(handler)
        provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert len(sleep_calls) == 1
        assert sleep_calls[0] == 5.0

    def test_retry_diagnostics_log_safe_fields_per_attempt(self, monkeypatch, caplog):
        monkeypatch.setattr("app.providers.glm.time.sleep", lambda s: None)

        counter = _CallCounter()

        def handler(request: httpx.Request) -> httpx.Response:
            counter.count += 1
            if counter.count == 1:
                return _overload_response()
            return _chat_completions_response(json.dumps(VALID_DELTA))

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.INFO):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "turn_id=turn_002" in caplog.text
        assert "attempt=1" in caplog.text
        assert "attempt=2" in caplog.text
        assert "provider_code=1305" in caplog.text
        assert "retry_delay_ms=" in caplog.text
        assert "test-glm-key" not in caplog.text
        assert "Bearer" not in caplog.text
        assert FIXTURE_REQUEST["instruction"] not in caplog.text


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


class TestPostSuccessFailureStageDiagnostics:
    """Root-cause instrumentation only: a 200 from Z.ai can still fail
    LOCALLY at one of several distinct parsing/validation stages inside
    reason_element, and every one of them currently raises the SAME
    InvalidProviderResponse with no way to tell them apart from logs.
    These tests pin a distinct, safe `stage` name logged at each site —
    behavior (the raised exception, its message, retry count) must stay
    completely unchanged; only a new log line is added per stage."""

    def test_envelope_extraction_stage_logged_on_missing_message_shape(self, caplog):
        def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(200, json={"choices": [{"message": {}}]})

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING), pytest.raises(InvalidProviderResponse):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "glm_spatial_post_success_failure" in caplog.text
        assert "stage=envelope_extraction" in caplog.text
        assert "turn_id=turn_002" in caplog.text
        assert "attempt=1" in caplog.text

    def test_empty_content_stage_logged(self, caplog):
        def handler(request: httpx.Request) -> httpx.Response:
            return _chat_completions_response("")

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING), pytest.raises(InvalidProviderResponse):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "stage=empty_content" in caplog.text

    def test_content_json_decode_stage_logged(self, caplog):
        def handler(request: httpx.Request) -> httpx.Response:
            return _chat_completions_response("not json at all {{{")

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING), pytest.raises(InvalidProviderResponse):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "stage=content_json_decode" in caplog.text
        # The raw unparseable content must never appear in the log.
        assert "not json at all" not in caplog.text

    def test_schema_validation_stage_logged_with_field_paths_only(self, caplog):
        """This is the leading hypothesis for the production failure: an
        instruction with no valid spatial-operation mapping (e.g. "move to
        the right edge") plausibly produces a spatial.spec that fails
        ProposedSceneEditDelta's closed discriminated union. The log must
        carry ONLY field/location paths from the Pydantic error — never
        pydantic's full errors() objects (which embed the offending input
        value) and never the raw response content."""

        def handler(request: httpx.Request) -> httpx.Response:
            bad = dict(VALID_DELTA)
            bad["unexpectedField"] = "this-should-never-appear-in-logs"
            return _chat_completions_response(json.dumps(bad))

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING), pytest.raises(InvalidProviderResponse):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "stage=schema_validation" in caplog.text
        assert "unexpectedField" in caplog.text  # the field PATH is safe and useful
        assert "this-should-never-appear-in-logs" not in caplog.text  # the offending VALUE is not

    def test_target_mismatch_stage_logged(self, caplog):
        def handler(request: httpx.Request) -> httpx.Response:
            mismatched = dict(VALID_DELTA)
            mismatched["target"] = {"kind": "object", "id": "object_other_999"}
            return _chat_completions_response(json.dumps(mismatched))

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING), pytest.raises(InvalidProviderResponse):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "stage=target_mismatch" in caplog.text

    def test_diagnostic_logging_never_leaks_secrets_or_content(self, caplog):
        """Blanket safety net across every stage: API key, Authorization
        header, instruction text, and raw content must never appear."""

        def handler(request: httpx.Request) -> httpx.Response:
            return _chat_completions_response("garbage-content-marker {{{")

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING), pytest.raises(InvalidProviderResponse):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "test-glm-key" not in caplog.text
        assert "Bearer" not in caplog.text
        assert "garbage-content-marker" not in caplog.text
        assert FIXTURE_REQUEST["instruction"] not in caplog.text

    def test_success_path_logs_no_failure_stage(self, caplog):
        """A successful parse/validation must not emit any post-success
        failure-stage log — this instrumentation is diagnostic-only for
        the failure paths, never noise on the happy path."""

        def handler(request: httpx.Request) -> httpx.Response:
            return _chat_completions_response(json.dumps(VALID_DELTA))

        provider = _provider_with_transport(handler)
        with caplog.at_level(logging.WARNING):
            provider.reason_element(SpatialReasoningRequest(**FIXTURE_REQUEST))

        assert "glm_spatial_post_success_failure" not in caplog.text
