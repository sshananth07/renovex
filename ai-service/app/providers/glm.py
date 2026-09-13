"""GLMProvider: the only place that talks to Z.AI's GLM-5.3 chat-completions
API for RP4E1 spatial reasoning. Deliberately separate from GeminiProvider
and its bounded-retry helper (gemini.py) — RP4E1 makes AT MOST ONE outbound
attempt per turn for every failure EXCEPT one narrow, explicitly bounded
exception: Z.ai's own transient-overload signal (HTTP 429 with
error.code=="1305", "The service may be temporarily overloaded, please try
again later"). That one case retries up to twice more (3 attempts total,
short backoff) since it is Z.ai telling us the request was never actually
rejected on its merits — every other failure (a different 429 reason,
4xx/5xx, timeout, transport error, invalid output) still makes exactly one
attempt, never repairs, never falls back to another model. Redirects and
transport-level retries are disabled explicitly rather than relying on
httpx defaults.

The retry loop lives entirely inside this one reason_element call — Go's
design_service.go still sees exactly one ReasonElement invocation per
durable turn, so its at-most-once dispatch guarantee, turn/session
identity, and RoomDraft state are all completely unaffected; no new
DesignTurn, session, or RoomDraft mutation is ever created here.

reasoning_content (GLM's chain-of-thought field, when present) is read only
to be discarded — it must never reach a prompt, log, or public DTO (RP4E1
plan "Provider bodies... reasoning content... are never returned or
logged").
"""

import json
import logging
import random
import time
from urllib.parse import urlparse

import httpx
from pydantic import ValidationError

from app.errors import (
    InvalidAIRequest,
    InvalidProviderResponse,
    ProviderRateLimited,
    ProviderTimeout,
    ProviderUnavailable,
)
from app.prompts.spatial_reasoning import (
    SPATIAL_REASONING_PROMPT_VERSION,
    build_spatial_reasoning_messages,
)
from app.schemas.spatial_reasoning import (
    ProposedSceneEditDelta,
    SpatialReasoningRequest,
    SpatialReasoningResult,
)

_RETRYABLE_SERVER_STATUS_CODES = {500, 502, 503, 504}
logger = logging.getLogger(__name__)

# Task 1 (structural fingerprint diagnostics): bounds the SERIALISED
# fingerprint string logged per failure — a pathological response with
# thousands of keys must not flood logs. This bounds bytes actually
# written to the log line, independent of fingerprint()'s own max_depth
# (which bounds recursion, not serialised size).
_FINGERPRINT_MAX_BYTES = 2048
_FINGERPRINT_MAX_DEPTH = 3


def fingerprint(obj: object, depth: int = 0, max_depth: int = _FINGERPRINT_MAX_DEPTH) -> object:
    """Structural fingerprint of obj — key NAMES and value TYPE NAMES
    only, NEVER values. Used to observe the actual shape of a parsed
    GLM/Z.ai response when it fails ProposedSceneEditDelta validation,
    instead of inferring the shape only from which fields are reported
    missing. Bounded by max_depth so a deeply/adversarially nested object
    cannot cause unbounded recursion; anything past max_depth becomes the
    placeholder "…". A list is represented by the fingerprint of its
    first element only (lists in this schema are homogeneous; the actual
    element count is not safety-sensitive but is intentionally not
    reported here to keep this function a pure shape probe)."""
    if depth > max_depth:
        return "…"
    if isinstance(obj, dict):
        return {k: fingerprint(v, depth + 1, max_depth) for k, v in sorted(obj.items(), key=lambda item: str(item[0]))}
    if isinstance(obj, list):
        return [fingerprint(obj[0], depth + 1, max_depth)] if obj else []
    return type(obj).__name__


def _safe_fingerprint(obj: object) -> tuple[str | None, str | None]:
    """Wraps fingerprint() + serialization so a pathological input can
    NEVER cause this diagnostic itself to raise or to propagate an
    exception out of a logging call site. Returns
    (serialised_fingerprint, fingerprint_error_class_name) — exactly one
    of the two is non-None. The serialised string is truncated at
    _FINGERPRINT_MAX_BYTES (UTF-8), with a truncation marker appended,
    rather than emitting an unbounded string for a pathological response."""
    try:
        shape = fingerprint(obj)
        serialised = json.dumps(shape, sort_keys=True, default=str)
    except Exception as exc:  # noqa: BLE001 - deliberately broad: this must never propagate
        return None, type(exc).__name__
    encoded = serialised.encode("utf-8")
    if len(encoded) > _FINGERPRINT_MAX_BYTES:
        truncated = encoded[:_FINGERPRINT_MAX_BYTES].decode("utf-8", errors="ignore")
        return truncated + "...<truncated>", None
    return serialised, None

# Z.ai's own transient-overload error code (as opposed to a quota/balance/
# entitlement rejection, which is also surfaced as HTTP 429 but with a
# DIFFERENT error.code and must never be retried — retrying a quota
# exhaustion cannot help and only burns another attempt against the same
# limit). Only this one code, at this one HTTP status, is retryable.
_TRANSIENT_OVERLOAD_PROVIDER_CODE = "1305"
_TRANSIENT_OVERLOAD_HTTP_STATUS = 429

# A slow/unreachable TCP connect and a slow Z.ai RESPONSE are different
# failure modes — the production timeout that motivated GLM_TIMEOUT_SECONDS'
# 90s default was the response taking too long (duration_ms=30078 at the
# old 30s ceiling), not a slow connection. Z.ai is a stable, generally-fast-
# to-reach host, so connect keeps a much shorter, fixed ceiling regardless
# of how high the overall read timeout is configured — a genuinely
# unreachable host should still fail fast rather than waiting the full
# (now much longer) read timeout.
_CONNECT_TIMEOUT_SECONDS = 10.0

# Bounded retry: at most 2 retries after the initial request (3 attempts
# total). Backoff is ~1s then ~2s plus a small jitter, UNLESS Z.ai's own
# Retry-After header is present on that response, which then takes
# precedence over the fixed schedule for that attempt.
_MAX_ATTEMPTS = 3
_BASE_BACKOFF_SECONDS = (0.0, 1.0, 2.0)  # index 0 unused (no delay before attempt 1)
_BACKOFF_JITTER_SECONDS = 0.25


def _is_retryable_overload(status_code: int, provider_code: str | None) -> bool:
    """True only for Z.ai's documented transient-overload signal — HTTP 429
    with error.code=="1305". A quota/balance/entitlement 429 carries a
    different code and must fall through to the existing non-retrying
    ProviderRateLimited path unchanged."""
    return status_code == _TRANSIENT_OVERLOAD_HTTP_STATUS and provider_code == _TRANSIENT_OVERLOAD_PROVIDER_CODE


def _retry_delay_seconds(attempt_number: int, retry_after: str | None) -> float:
    """attempt_number is the attempt about to be made (2 or 3 here, since
    there is never a delay before attempt 1). Z.ai's Retry-After, when
    present and parseable, takes precedence over the fixed backoff
    schedule for that attempt."""
    if retry_after is not None:
        try:
            parsed = float(retry_after)
            if parsed >= 0:
                return parsed
        except ValueError:
            pass
    base = _BASE_BACKOFF_SECONDS[attempt_number - 1] if attempt_number - 1 < len(_BASE_BACKOFF_SECONDS) else _BASE_BACKOFF_SECONDS[-1]
    return base + random.uniform(0, _BACKOFF_JITTER_SECONDS)


def _safe_provider_error_fields(response: httpx.Response) -> tuple[str | None, str | None, str | None, str | None]:
    """Extracts ONLY (provider_code, provider_message, retry_after,
    provider_request_id) from a non-2xx GLM/Z.ai response, for diagnostic
    logging — never the raw body, headers, or anything else. Z.ai's
    documented error shape is {"error": {"code": ..., "message": ...}};
    this degrades to (None, None, ...) for any other shape (e.g. a string
    "error" field, or a body that isn't a JSON object) rather than raising,
    so a malformed/unexpected error body never breaks the failure path
    itself. Mirrors cloudflare_flux.py's _safe_request_id "defensive,
    type-checked extraction" convention."""
    provider_code: str | None = None
    provider_message: str | None = None
    try:
        body = response.json()
    except ValueError:
        body = None
    if isinstance(body, dict):
        error = body.get("error")
        if isinstance(error, dict):
            code = error.get("code")
            if isinstance(code, (str, int)):
                provider_code = str(code)[:200]
            message = error.get("message")
            if isinstance(message, str):
                provider_message = message[:500]

    retry_after = response.headers.get("retry-after")
    if isinstance(retry_after, str):
        retry_after = retry_after[:50]

    provider_request_id = response.headers.get("x-request-id") or response.headers.get("z-request-id")
    if isinstance(body, dict) and provider_request_id is None:
        request_id = body.get("id") or body.get("request_id")
        if isinstance(request_id, str):
            provider_request_id = request_id
    if isinstance(provider_request_id, str):
        provider_request_id = provider_request_id[:200]

    return provider_code, provider_message, retry_after, provider_request_id


def _safe_validation_error_locations(exc: ValidationError) -> list[str]:
    """Extracts ONLY dotted field-location paths from a Pydantic
    ValidationError — e.g. "spatial.spec.kind" — never the error type,
    message text, or (critically) the "input" key each error carries,
    which embeds the actual offending value from the response content.
    Bounded to 20 entries so a pathological error list cannot inflate a
    single log line unboundedly."""
    locations: list[str] = []
    for err in exc.errors():
        loc = err.get("loc")
        if isinstance(loc, (list, tuple)):
            locations.append(".".join(str(part) for part in loc))
        if len(locations) >= 20:
            break
    return locations


def _safe_reasoning_tokens(body: object) -> int | None:
    """Extracts ONLY usage.completion_tokens_details.reasoning_tokens (an
    integer count) — never any other usage field, never the response
    content. Returns None when absent or any expected level isn't a dict,
    never raises."""
    usage = body.get("usage") if isinstance(body, dict) else None
    details = usage.get("completion_tokens_details") if isinstance(usage, dict) else None
    reasoning_tokens = details.get("reasoning_tokens") if isinstance(details, dict) else None
    return reasoning_tokens if isinstance(reasoning_tokens, int) else None


def _log_post_success_failure_stage(
    request_turn_id: str,
    attempt: int,
    stage: str,
    exc: Exception | None,
    *,
    parsed: object = None,
    body: object = None,
) -> None:
    """Root-cause diagnostic ONLY (no behavior change): every post-200
    parsing/validation failure inside reason_element currently raises the
    SAME InvalidProviderResponse, with no way to tell from logs which of
    the five local stages actually failed. This logs turn id, attempt,
    the stage name, and the exception's CLASS NAME only — never
    str(exc)/repr(exc), since several of these exception types (KeyError,
    IndexError, json.JSONDecodeError) can embed a fragment of the actual
    response content in their message. For a Pydantic ValidationError
    specifically, only field-location paths are logged (never its full
    errors() objects, which carry the offending input value) — see
    _safe_validation_error_locations.

    Task 1 (structural fingerprint diagnostics) additions: when `parsed`
    is supplied (only schema_validation/target_mismatch have a parsed
    JSON object at all — envelope_extraction/empty_content/
    content_json_decode never reach json.loads), a bounded, key-names-
    and-types-only fingerprint of it is logged so the ACTUAL shape of a
    malformed response can be observed instead of only inferred from
    which fields are reported missing; a pathological/unfingerprintable
    input can never raise here (see _safe_fingerprint) — the failure
    itself is logged as fingerprint_error=<class> instead. When `body` is
    supplied, usage.completion_tokens_details.reasoning_tokens is also
    logged if present. Never logs the response body, prompt, RoomDraft,
    API key, or Authorization header — none of those are read here at
    all."""
    exception_type = type(exc).__name__ if exc is not None else None
    field_locations = _safe_validation_error_locations(exc) if isinstance(exc, ValidationError) else None
    fingerprint_str, fingerprint_error = (None, None)
    if parsed is not None:
        fingerprint_str, fingerprint_error = _safe_fingerprint(parsed)
    reasoning_tokens = _safe_reasoning_tokens(body)
    logger.warning(
        "glm_spatial_post_success_failure turn_id=%s attempt=%d stage=%s exception_type=%s field_locations=%s "
        "fingerprint=%s fingerprint_error=%s reasoning_tokens=%s",
        request_turn_id,
        attempt,
        stage,
        exception_type,
        field_locations,
        fingerprint_str,
        fingerprint_error,
        reasoning_tokens,
    )


def _log_post_success_shape(request_turn_id: str, attempt: int, body: object) -> None:
    """Root-cause instrumentation ONLY (no behavior change): logs the
    SHAPE of a successful HTTP 200 response body — never any of its
    actual content — immediately before the empty_content check, for
    EVERY successful response (not only the ones that end up empty).
    Production confirmed a 200 reaching stage=empty_content; this exists
    to reveal WHY content came back empty (e.g. finish_reason="length",
    a tool_calls-only response, or reasoning_content consuming the whole
    token budget) without ever reading message.content or
    reasoning_content's VALUE — only their presence, Python type, and
    length. Every field read here is presence/count/type/length metadata;
    none of it is prompt, RoomDraft, credential, or response-content
    data."""
    choices = body.get("choices") if isinstance(body, dict) else None
    choices_count = len(choices) if isinstance(choices, list) else None
    first_choice = choices[0] if isinstance(choices, list) and choices else None
    first_choice_present = first_choice is not None

    message = first_choice.get("message") if isinstance(first_choice, dict) else None
    message_keys = sorted(message.keys()) if isinstance(message, dict) else None

    content = message.get("content") if isinstance(message, dict) else None
    content_type = type(content).__name__ if content is not None else None
    content_length = len(content) if isinstance(content, (str, list, dict)) else None

    reasoning_content = message.get("reasoning_content") if isinstance(message, dict) else None
    reasoning_content_present = reasoning_content is not None and reasoning_content != ""
    reasoning_content_type = type(reasoning_content).__name__ if reasoning_content is not None else None
    reasoning_content_length = len(reasoning_content) if isinstance(reasoning_content, (str, list, dict)) else None

    tool_calls = message.get("tool_calls") if isinstance(message, dict) else None
    tool_calls_present = bool(tool_calls)
    tool_calls_count = len(tool_calls) if isinstance(tool_calls, list) else 0

    finish_reason = first_choice.get("finish_reason") if isinstance(first_choice, dict) else None

    usage = body.get("usage") if isinstance(body, dict) else None
    completion_tokens = usage.get("completion_tokens") if isinstance(usage, dict) else None
    prompt_tokens = usage.get("prompt_tokens") if isinstance(usage, dict) else None
    total_tokens = usage.get("total_tokens") if isinstance(usage, dict) else None

    logger.info(
        "glm_spatial_post_success_shape turn_id=%s attempt=%d choices_count=%s first_choice_present=%s "
        "message_keys=%s content_type=%s content_length=%s reasoning_content_present=%s "
        "reasoning_content_type=%s reasoning_content_length=%s tool_calls_present=%s tool_calls_count=%s "
        "finish_reason=%s completion_tokens=%s prompt_tokens=%s total_tokens=%s",
        request_turn_id,
        attempt,
        choices_count,
        first_choice_present,
        message_keys,
        content_type,
        content_length,
        reasoning_content_present,
        reasoning_content_type,
        reasoning_content_length,
        tool_calls_present,
        tool_calls_count,
        finish_reason,
        completion_tokens,
        prompt_tokens,
        total_tokens,
    )


def _log_empty_content_diagnostic(request_turn_id: str, attempt: int, body: object) -> None:
    """Root-cause instrumentation ONLY for the empty_content branch
    specifically (no behavior change): pins the exact fields needed to
    confirm or rule out the "thinking budget exhausted max_tokens" root-
    cause hypothesis — finish_reason, whether content/reasoning_content
    were present, their LENGTHS only (never their values), tool_calls
    presence, and completion/total token counts. Deliberately separate
    from _log_post_success_shape (which already logs a superset of this on
    every 200) so this one exact branch's diagnostic is self-contained
    and easy to grep for in isolation. Never logs the response body,
    content, reasoning_content, prompts, RoomDraft, API key, or
    Authorization header — none of those are read here at all."""
    choices = body.get("choices") if isinstance(body, dict) else None
    choices_count = len(choices) if isinstance(choices, list) else None
    first_choice = choices[0] if isinstance(choices, list) and choices else None

    message = first_choice.get("message") if isinstance(first_choice, dict) else None
    content = message.get("content") if isinstance(message, dict) else None
    content_present = bool(content)
    content_length = len(content) if isinstance(content, (str, list, dict)) else 0

    reasoning_content = message.get("reasoning_content") if isinstance(message, dict) else None
    reasoning_content_present = bool(reasoning_content)
    reasoning_content_length = len(reasoning_content) if isinstance(reasoning_content, (str, list, dict)) else 0

    tool_calls = message.get("tool_calls") if isinstance(message, dict) else None
    tool_calls_present = bool(tool_calls)

    finish_reason = first_choice.get("finish_reason") if isinstance(first_choice, dict) else None

    usage = body.get("usage") if isinstance(body, dict) else None
    completion_tokens = usage.get("completion_tokens") if isinstance(usage, dict) else None
    total_tokens = usage.get("total_tokens") if isinstance(usage, dict) else None

    logger.warning(
        "glm_spatial_empty_content_diagnostic turn_id=%s attempt=%d finish_reason=%s choices_count=%s "
        "content_present=%s content_length=%s reasoning_content_present=%s reasoning_content_length=%s "
        "tool_calls_present=%s completion_tokens=%s total_tokens=%s",
        request_turn_id,
        attempt,
        finish_reason,
        choices_count,
        content_present,
        content_length,
        reasoning_content_present,
        reasoning_content_length,
        tool_calls_present,
        completion_tokens,
        total_tokens,
    )


def _log_provider_rejected(
    request_turn_id: str,
    response: httpx.Response,
    *,
    attempt: int | None = None,
    duration_ms: int | None = None,
) -> None:
    """Logs the safe, bounded diagnostic fields for a non-2xx GLM response —
    turn id, attempt number, HTTP status, Z.ai's own error.code/
    error.message, request duration, Retry-After, and a provider request
    id, if present. Never logs the API key, the Authorization header, the
    prompt, or the RoomDraft/context payload — none of those are read here
    at all."""
    provider_code, provider_message, retry_after, provider_request_id = _safe_provider_error_fields(response)
    logger.warning(
        "glm_spatial_provider_rejected turn_id=%s attempt=%s http_status=%s provider_code=%s provider_message=%s duration_ms=%s retry_after=%s provider_request_id=%s",
        request_turn_id,
        attempt,
        response.status_code,
        provider_code,
        json.dumps(provider_message) if provider_message is not None else None,
        duration_ms,
        retry_after,
        provider_request_id,
    )


class GLMProvider:
    def __init__(
        self,
        api_key: str,
        model: str,
        base_url: str,
        reasoning_effort: str,
        timeout: float,
        max_output_tokens: int = 2000,
        transport: httpx.BaseTransport | None = None,
    ):
        self._api_key = api_key
        self._model = model
        self._base_url = base_url.rstrip("/")
        self._reasoning_effort = reasoning_effort
        self._max_output_tokens = max_output_tokens
        # follow_redirects=False and no transport-level retry policy: a
        # single non-streaming POST, no more. transport is injectable for
        # httpx.MockTransport in tests; production leaves it unset (real
        # network transport). connect keeps its own short, fixed ceiling
        # (_CONNECT_TIMEOUT_SECONDS) independent of the caller-configured
        # overall/read timeout — see that constant's own doc comment.
        self._client = httpx.Client(
            timeout=httpx.Timeout(timeout, connect=_CONNECT_TIMEOUT_SECONDS),
            follow_redirects=False,
            transport=transport,
        )

    def reason_element_from_raw(self, raw: dict) -> SpatialReasoningResult:
        """Validates raw input into SpatialReasoningRequest BEFORE any HTTP
        call. Invalid local input (fails Pydantic validation) makes ZERO
        outbound calls (RP4E1 plan Task 3 Step 1)."""
        try:
            request = SpatialReasoningRequest(**raw)
        except ValidationError as exc:
            raise InvalidAIRequest(f"invalid spatial reasoning request: {exc}") from None
        return self.reason_element(request)

    def reason_element(self, request: SpatialReasoningRequest) -> SpatialReasoningResult:
        response, attempt = self._post_with_bounded_overload_retry(request)

        body: object = None
        try:
            body = response.json()
            content = body["choices"][0]["message"]["content"]
        except (KeyError, IndexError, TypeError, ValueError) as exc:
            _log_post_success_failure_stage(request.turnId, attempt, "envelope_extraction", exc, body=body)
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output") from None
        _log_post_success_shape(request.turnId, attempt, body)
        if not content:
            _log_post_success_failure_stage(request.turnId, attempt, "empty_content", None, body=body)
            _log_empty_content_diagnostic(request.turnId, attempt, body)
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output")

        try:
            parsed = json.loads(content)
        except json.JSONDecodeError as exc:
            _log_post_success_failure_stage(request.turnId, attempt, "content_json_decode", exc, body=body)
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output") from None

        try:
            delta = ProposedSceneEditDelta(**parsed)
        except ValidationError as exc:
            _log_post_success_failure_stage(request.turnId, attempt, "schema_validation", exc, parsed=parsed, body=body)
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output") from None

        # Cross-check: the model must echo back the SAME target it was
        # given — never trust the model to have understood which element it
        # was reasoning about (RP4E1 plan "design_plan_target_mismatch").
        if delta.target.kind != request.selectedElement.kind or delta.target.id != request.selectedElement.id:
            _log_post_success_failure_stage(request.turnId, attempt, "target_mismatch", None, parsed=parsed, body=body)
            raise InvalidProviderResponse("spatial reasoning provider returned a mismatched target")

        return SpatialReasoningResult(
            delta=delta,
            provider="glm",
            model=self._model,
            promptVersion=SPATIAL_REASONING_PROMPT_VERSION,
            schemaVersion=1,
        )

    def _post_with_bounded_overload_retry(self, request: SpatialReasoningRequest) -> tuple[httpx.Response, int]:
        """Makes the outbound POST, retrying ONLY on Z.ai's own transient-
        overload signal (HTTP 429, error.code=="1305") — up to _MAX_ATTEMPTS
        total attempts. Every other outcome (success, a different 429
        reason, any other status, timeout, transport failure) returns or
        raises immediately from the first attempt that produces it, exactly
        as reason_element did before this retry existed — _post_once
        raises directly for all of those, so this loop only ever has to
        handle the one case it returns normally for: a retryable overload
        response. The same SpatialReasoningRequest/turn/payload is reused
        for every attempt — no new DesignTurn, session, or request context
        is created; this loop is invisible to Go, which still sees one
        ReasonElement call. Returns (response, winning_attempt_number) so
        callers can attribute diagnostics to the attempt that actually
        produced the response being parsed, rather than assuming attempt 1."""
        last_overload_response: httpx.Response | None = None
        for attempt in range(1, _MAX_ATTEMPTS + 1):
            response, retryable_overload = self._post_once(request, attempt)
            if not retryable_overload:
                return response, attempt
            last_overload_response = response
            if attempt < _MAX_ATTEMPTS:
                retry_after = response.headers.get("retry-after")
                delay = _retry_delay_seconds(attempt + 1, retry_after)
                delay_ms = int(delay * 1000)
                logger.warning(
                    "glm_spatial_overload_retry_scheduled turn_id=%s attempt=%d next_attempt=%d retry_delay_ms=%d",
                    request.turnId,
                    attempt,
                    attempt + 1,
                    delay_ms,
                )
                time.sleep(delay)

        # _MAX_ATTEMPTS consecutive transient-overload responses: surface
        # the SAME ProviderRateLimited a single non-retried 429 always has
        # — Go's existing needs_attention/design_reasoning_provider_rejected
        # classification is completely unchanged, it just now happens after
        # a bounded number of attempts instead of always after exactly one.
        assert last_overload_response is not None  # loop always runs >=1 time and only reaches here via the overload branch
        raise ProviderRateLimited("spatial reasoning provider rate limit exceeded")

    def _post_once(self, request: SpatialReasoningRequest, attempt: int) -> tuple[httpx.Response, bool]:
        """One HTTP attempt. Returns (response, is_retryable_overload):
        - (response, False) on 2xx — the caller returns it as
          reason_element always has.
        - (response, True) ONLY for HTTP 429 with error.code=="1305" — the
          caller may retry.
        Every other outcome (a different 429 reason, any other non-2xx
        status, a redirect, timeout, or transport failure) raises directly
        — never retried, matching every pre-existing classification other
        than the one narrow overload case."""
        system, user = build_spatial_reasoning_messages(request)
        payload = {
            "model": self._model,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
            "response_format": {"type": "json_object"},
            "max_tokens": self._max_output_tokens,
            "stream": False,
            "reasoning_effort": self._reasoning_effort,
        }

        destination = urlparse(f"{self._base_url}/chat/completions")
        started_at = time.monotonic()
        logger.info(
            "glm_spatial_request_started turn_id=%s attempt=%d destination_host=%s destination_path=%s model=%s",
            request.turnId,
            attempt,
            destination.hostname,
            destination.path,
            self._model,
        )
        try:
            response = self._client.post(
                f"{self._base_url}/chat/completions",
                json=payload,
                headers={"Authorization": f"Bearer {self._api_key}"},
            )
        except httpx.TimeoutException:
            logger.warning(
                "glm_spatial_request_failed turn_id=%s attempt=%d failure_class=timeout duration_ms=%d",
                request.turnId, attempt, int((time.monotonic() - started_at) * 1000),
            )
            raise ProviderTimeout("spatial reasoning provider request timed out") from None
        except httpx.HTTPError:
            logger.warning(
                "glm_spatial_request_failed turn_id=%s attempt=%d failure_class=transport duration_ms=%d",
                request.turnId, attempt, int((time.monotonic() - started_at) * 1000),
            )
            raise ProviderUnavailable("spatial reasoning provider request failed") from None

        duration_ms = int((time.monotonic() - started_at) * 1000)
        logger.info(
            "glm_spatial_response_received turn_id=%s attempt=%d http_status=%s duration_ms=%d",
            request.turnId,
            attempt,
            response.status_code,
            duration_ms,
        )

        if response.status_code == _TRANSIENT_OVERLOAD_HTTP_STATUS:
            provider_code, _, _, _ = _safe_provider_error_fields(response)
            _log_provider_rejected(request.turnId, response, attempt=attempt, duration_ms=duration_ms)
            if _is_retryable_overload(response.status_code, provider_code):
                return response, True
            raise ProviderRateLimited("spatial reasoning provider rate limit exceeded")
        if response.status_code in _RETRYABLE_SERVER_STATUS_CODES:
            _log_provider_rejected(request.turnId, response, attempt=attempt, duration_ms=duration_ms)
            raise ProviderUnavailable("spatial reasoning provider temporarily unavailable")
        if response.is_redirect:
            # follow_redirects=False means httpx never follows this
            # automatically; a 3xx here is itself treated as invalid output
            # — GLM's API contract never redirects a legitimate response.
            _log_provider_rejected(request.turnId, response, attempt=attempt, duration_ms=duration_ms)
            raise InvalidProviderResponse("spatial reasoning provider returned an unexpected redirect")
        if response.status_code != 200:
            _log_provider_rejected(request.turnId, response, attempt=attempt, duration_ms=duration_ms)
            raise ProviderUnavailable("spatial reasoning provider request failed")

        return response, False
