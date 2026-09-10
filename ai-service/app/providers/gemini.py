"""GeminiProvider: the only place that talks to the official google-genai
SDK. Uses provider-supported structured output — never Markdown-fence
stripping, regex JSON extraction, or prose repair (design doc "Structured
Output"). Retries a small bounded number of times for transient failures
only; auth/config/invalid-request/invalid-response failures never retry.

Structured output is requested via response_json_schema (the raw JSON
Schema generated from the Pydantic response model), not response_schema
(the Pydantic class itself). google-genai==2.18.1's response_schema path
translates Pydantic's extra="forbid" (-> JSON Schema
additionalProperties:false) into a field Gemini's API rejects with a 400
INVALID_ARGUMENT ("Unknown name \"additional_properties\""); the
response_json_schema path does not hit this incompatibility. Because the
SDK does not auto-parse into a typed instance for response_json_schema,
response.text is explicitly validated through the SAME Pydantic model via
model_validate_json — a real JSON deserializer, never response.parsed
(untrustworthy/absent for this mode), prose parsing, or schema repair.
extra="forbid" still hard-fails any unexpected field at that validation
step, exactly as it did for the response_schema path."""

import time

import httpx
from google import genai
from google.genai import errors as genai_errors
from google.genai.types import GenerateContentConfig
from pydantic import BaseModel, ValidationError

from app.config import Settings
from app.errors import (
    InvalidProviderResponse,
    ProviderRateLimited,
    ProviderTimeout,
    ProviderUnavailable,
)
from app.prompts.resources import RESOURCES_PROMPT_VERSION, build_resources_prompt
from app.prompts.spaces import SPACES_PROMPT_VERSION, build_spaces_prompt
from app.prompts.work_items import WORK_ITEMS_PROMPT_VERSION, build_work_items_prompt
from app.schemas.resources import ResourceSuggestionRequest, ResourceSuggestionResult
from app.schemas.spaces import SpaceSuggestionRequest, SpaceSuggestionResult
from app.schemas.work_items import WorkItemSuggestionRequest, WorkItemSuggestionResult

_SCHEMA_VERSION = 1

_RETRYABLE_STATUS_CODES = {429, 500, 502, 503, 504}


class GeminiProvider:
    def __init__(self, settings: Settings, client: genai.Client | None = None):
        self._settings = settings
        self._client = client or genai.Client(api_key=settings.GEMINI_API_KEY)

    def suggest_spaces(self, request: SpaceSuggestionRequest) -> SpaceSuggestionResult:
        prompt = build_spaces_prompt(request)
        parsed = self._generate(prompt, SpaceSuggestionResult)
        return SpaceSuggestionResult(
            provider="gemini",
            model=self._settings.GEMINI_MODEL,
            promptVersion=SPACES_PROMPT_VERSION,
            schemaVersion=_SCHEMA_VERSION,
            suggestions=parsed.suggestions,
        )

    def suggest_work_items(self, request: WorkItemSuggestionRequest) -> WorkItemSuggestionResult:
        prompt = build_work_items_prompt(request)
        parsed = self._generate(prompt, WorkItemSuggestionResult)
        return WorkItemSuggestionResult(
            provider="gemini",
            model=self._settings.GEMINI_MODEL,
            promptVersion=WORK_ITEMS_PROMPT_VERSION,
            schemaVersion=_SCHEMA_VERSION,
            suggestions=parsed.suggestions,
        )

    def suggest_resources(self, request: ResourceSuggestionRequest) -> ResourceSuggestionResult:
        prompt = build_resources_prompt(request)
        parsed = self._generate(prompt, ResourceSuggestionResult)
        return ResourceSuggestionResult(
            provider="gemini",
            model=self._settings.GEMINI_MODEL,
            promptVersion=RESOURCES_PROMPT_VERSION,
            schemaVersion=_SCHEMA_VERSION,
            suggestions=parsed.suggestions,
        )

    def _generate(self, prompt: str, response_schema: type[BaseModel]):
        max_retries = self._settings.AI_PROVIDER_MAX_RETRIES
        attempt = 0
        while True:
            try:
                response = self._client.models.generate_content(
                    model=self._settings.GEMINI_MODEL,
                    contents=prompt,
                    config=GenerateContentConfig(
                        response_mime_type="application/json",
                        response_json_schema=response_schema.model_json_schema(),
                    ),
                )
            except httpx.TimeoutException:
                if attempt < max_retries:
                    attempt += 1
                    time.sleep(_backoff_seconds(attempt))
                    continue
                raise ProviderTimeout("AI provider request timed out") from None
            except genai_errors.APIError as exc:
                code = exc.code
                if code == 429:
                    if attempt < max_retries:
                        attempt += 1
                        time.sleep(_backoff_seconds(attempt))
                        continue
                    raise ProviderRateLimited("AI provider rate limit exceeded") from None
                if code in _RETRYABLE_STATUS_CODES:
                    if attempt < max_retries:
                        attempt += 1
                        time.sleep(_backoff_seconds(attempt))
                        continue
                    raise ProviderUnavailable("AI provider temporarily unavailable") from None
                # 400/401/403/404 and anything else non-retryable: never
                # leak exc.message (may embed request/key details).
                raise ProviderUnavailable("AI provider request failed") from None
            else:
                # response.parsed is not populated for response_json_schema
                # (only response_schema=<pydantic class> triggers SDK
                # auto-parsing) and must never be trusted regardless — the
                # model can emit well-formed-but-wrong values for fields it
                # was not given real context for. response.text is the only
                # signal, and model_validate_json is a real JSON
                # deserializer (never prose parsing, Markdown-fence
                # stripping, regex extraction, or schema repair) that
                # re-enforces extra="forbid" and every field constraint on
                # the way back in.
                text = response.text
                if not text:
                    raise InvalidProviderResponse("AI provider returned invalid structured output")
                try:
                    return response_schema.model_validate_json(text)
                except ValidationError:
                    raise InvalidProviderResponse(
                        "AI provider returned invalid structured output"
                    ) from None


def _backoff_seconds(attempt: int) -> float:
    return min(0.1 * (2 ** (attempt - 1)), 2.0)
