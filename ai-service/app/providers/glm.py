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

        try:
            response = self._client.post(
                f"{self._base_url}/chat/completions",
                json=payload,
                headers={"Authorization": f"Bearer {self._api_key}"},
            )
        except httpx.TimeoutException:
            raise ProviderTimeout("spatial reasoning provider request timed out") from None
        except httpx.HTTPError:
            raise ProviderUnavailable("spatial reasoning provider request failed") from None

        if response.status_code == 429:
            raise ProviderRateLimited("spatial reasoning provider rate limit exceeded")
        if response.status_code in _RETRYABLE_SERVER_STATUS_CODES:
            raise ProviderUnavailable("spatial reasoning provider temporarily unavailable")
        if response.is_redirect:
            # follow_redirects=False means httpx never follows this
            # automatically; a 3xx here is itself treated as invalid output
            # — GLM's API contract never redirects a legitimate response.
            raise InvalidProviderResponse("spatial reasoning provider returned an unexpected redirect")
        if response.status_code != 200:
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
