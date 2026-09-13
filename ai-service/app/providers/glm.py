"""GLMProvider: the only place that talks to Z.AI's GLM-5.3 chat-completions
API for RP4E1 spatial reasoning. Deliberately separate from GeminiProvider
and its bounded-retry helper (gemini.py) — RP4E1 makes AT MOST ONE outbound
attempt per turn, never retries, never repairs, never falls back to another
model (RP4E1 plan's global constraints). Redirects and transport-level
retries are disabled explicitly rather than relying on httpx defaults.

reasoning_content (GLM's chain-of-thought field, when present) is read only
to be discarded — it must never reach a prompt, log, or public DTO (RP4E1
plan "Provider bodies... reasoning content... are never returned or
logged").
"""

import json
import logging
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


def _log_provider_rejected(request_turn_id: str, response: httpx.Response) -> None:
    """Logs the safe, bounded diagnostic fields for a non-2xx GLM response —
    HTTP status, Z.ai's own error.code/error.message, Retry-After, and a
    provider request id, if present. Never logs the API key, the
    Authorization header, the prompt, or the RoomDraft/context payload —
    none of those are read here at all."""
    provider_code, provider_message, retry_after, provider_request_id = _safe_provider_error_fields(response)
    logger.warning(
        "glm_spatial_provider_rejected turn_id=%s http_status=%s provider_code=%s provider_message=%s retry_after=%s provider_request_id=%s",
        request_turn_id,
        response.status_code,
        provider_code,
        json.dumps(provider_message) if provider_message is not None else None,
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
        # network transport).
        self._client = httpx.Client(timeout=timeout, follow_redirects=False, transport=transport)

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
            "glm_spatial_request_started turn_id=%s destination_host=%s destination_path=%s model=%s",
            request.turnId,
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
            logger.warning("glm_spatial_request_failed turn_id=%s failure_class=timeout duration_ms=%d", request.turnId, int((time.monotonic() - started_at) * 1000))
            raise ProviderTimeout("spatial reasoning provider request timed out") from None
        except httpx.HTTPError:
            logger.warning("glm_spatial_request_failed turn_id=%s failure_class=transport duration_ms=%d", request.turnId, int((time.monotonic() - started_at) * 1000))
            raise ProviderUnavailable("spatial reasoning provider request failed") from None

        logger.info(
            "glm_spatial_response_received turn_id=%s http_status=%s duration_ms=%d",
            request.turnId,
            response.status_code,
            int((time.monotonic() - started_at) * 1000),
        )

        if response.status_code == 429:
            _log_provider_rejected(request.turnId, response)
            raise ProviderRateLimited("spatial reasoning provider rate limit exceeded")
        if response.status_code in _RETRYABLE_SERVER_STATUS_CODES:
            _log_provider_rejected(request.turnId, response)
            raise ProviderUnavailable("spatial reasoning provider temporarily unavailable")
        if response.is_redirect:
            # follow_redirects=False means httpx never follows this
            # automatically; a 3xx here is itself treated as invalid output
            # — GLM's API contract never redirects a legitimate response.
            _log_provider_rejected(request.turnId, response)
            raise InvalidProviderResponse("spatial reasoning provider returned an unexpected redirect")
        if response.status_code != 200:
            _log_provider_rejected(request.turnId, response)
            raise ProviderUnavailable("spatial reasoning provider request failed")

        try:
            body = response.json()
            content = body["choices"][0]["message"]["content"]
        except (KeyError, IndexError, TypeError, ValueError):
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output") from None
        if not content:
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output")

        try:
            parsed = json.loads(content)
        except json.JSONDecodeError:
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output") from None

        try:
            delta = ProposedSceneEditDelta(**parsed)
        except ValidationError:
            raise InvalidProviderResponse("spatial reasoning provider returned invalid structured output") from None

        # Cross-check: the model must echo back the SAME target it was
        # given — never trust the model to have understood which element it
        # was reasoning about (RP4E1 plan "design_plan_target_mismatch").
        if delta.target.kind != request.selectedElement.kind or delta.target.id != request.selectedElement.id:
            raise InvalidProviderResponse("spatial reasoning provider returned a mismatched target")

        return SpatialReasoningResult(
            delta=delta,
            provider="glm",
            model=self._model,
            promptVersion=SPATIAL_REASONING_PROMPT_VERSION,
            schemaVersion=1,
        )
