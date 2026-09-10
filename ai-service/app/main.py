"""FastAPI app factory. create_app takes an explicit Settings instance so
tests never depend on process environment variables (design doc section 21:
secrets exist only in runtime configuration, never hardcoded defaults)."""

from fastapi import Depends, FastAPI
from fastapi.responses import JSONResponse
from pydantic import ValidationError

from app.auth import require_internal_token
from app.config import Settings
from app.errors import AIServiceError, InvalidAIRequest
from app.providers.mock import MockProvider
from app.schemas.reference_image import ReferenceImageRequest, ReferenceImageResult
from app.schemas.resources import ResourceSuggestionRequest, ResourceSuggestionResult
from app.schemas.spaces import SpaceSuggestionRequest, SpaceSuggestionResult
from app.schemas.spatial_reasoning import SpatialReasoningRequest, SpatialReasoningResult
from app.schemas.work_items import WorkItemSuggestionRequest, WorkItemSuggestionResult
from app.services.reference_image import ReferenceImageService
from app.services.spatial_reasoning import SpatialReasoningService
from app.services.suggestions import SuggestionService


def _build_provider(settings: Settings):
    if settings.AI_PROVIDER == "mock":
        return MockProvider()
    from app.providers.gemini import GeminiProvider

    return GeminiProvider(settings)


def _build_spatial_reasoning_provider(settings: Settings):
    """Returns None when GLM configuration is missing/invalid — a missing
    GLM_API_KEY must make ONLY the spatial route unavailable, never take
    down Copilot or silently fall back to another provider (RP4E1 plan Task
    3 Step 8)."""
    if settings.SPATIAL_AI_PROVIDER == "mock":
        from app.providers.spatial_mock import SpatialMockProvider

        return SpatialMockProvider()
    if settings.SPATIAL_AI_PROVIDER == "glm":
        if not settings.GLM_API_KEY:
            return None
        from app.providers.glm import GLMProvider

        return GLMProvider(
            api_key=settings.GLM_API_KEY,
            model=settings.GLM_MODEL,
            base_url=settings.GLM_BASE_URL,
            reasoning_effort=settings.GLM_REASONING_EFFORT,
            timeout=settings.GLM_TIMEOUT_SECONDS,
            max_output_tokens=settings.GLM_MAX_OUTPUT_TOKENS,
        )
    return None


def _build_reference_image_provider(settings: Settings):
    """Returns None when Cloudflare configuration is missing/invalid — a
    missing CLOUDFLARE_API_TOKEN must make ONLY the reference-image route
    unavailable, never take down anything else (same convention as
    _build_spatial_reasoning_provider)."""
    if settings.REFERENCE_IMAGE_PROVIDER == "mock":
        from app.providers.reference_mock import MockReferenceImageProvider

        return MockReferenceImageProvider()
    if settings.REFERENCE_IMAGE_PROVIDER == "cloudflare":
        if not settings.CLOUDFLARE_ACCOUNT_ID or not settings.CLOUDFLARE_API_TOKEN:
            return None
        from app.providers.cloudflare_flux import CloudflareFluxReferenceImageProvider

        return CloudflareFluxReferenceImageProvider(
            account_id=settings.CLOUDFLARE_ACCOUNT_ID,
            api_token=settings.CLOUDFLARE_API_TOKEN,
            model=settings.CLOUDFLARE_FLUX_MODEL,
            timeout=settings.REFERENCE_IMAGE_TIMEOUT_SECONDS,
        )
    return None


def validate_production_settings(settings: Settings) -> None:
    """T2D fail-fast: production must never silently boot with a provider
    still at its offline/mock default — mirrors the Go backend's own
    APP_ENV=production guards in internal/platform/config/config.go
    (OBJECT_STORE_PROVIDER, MONGO_URI). Raises RuntimeError, which is
    intentionally a hard crash at startup rather than a route-level
    failure discovered later."""
    if settings.ENVIRONMENT != "production":
        return
    if settings.AI_PROVIDER == "mock":
        raise RuntimeError("config: AI_PROVIDER must not be \"mock\" when ENVIRONMENT=production")
    # SPATIAL_AI_PROVIDER and REFERENCE_IMAGE_PROVIDER are each optional
    # feature routes (a missing/invalid credential already makes ONLY that
    # route unavailable, never the whole service — see
    # _build_spatial_reasoning_provider/_build_reference_image_provider's
    # own doc comments) — so "mock" is only a production problem when the
    # route is otherwise configured to be enabled at all, i.e. its non-mock
    # branch was selected but real credentials are missing. Enabling the
    # feature un-configured is already handled by those builders returning
    # None; this guard only rejects the specific case where the DEFAULT was
    # never overridden away from "mock" despite the operator apparently
    # intending to run for real.
    if settings.SPATIAL_AI_PROVIDER == "mock":
        raise RuntimeError("config: SPATIAL_AI_PROVIDER must not be \"mock\" when ENVIRONMENT=production")
    if settings.REFERENCE_IMAGE_PROVIDER == "mock":
        raise RuntimeError("config: REFERENCE_IMAGE_PROVIDER must not be \"mock\" when ENVIRONMENT=production")


def create_app(settings: Settings) -> FastAPI:
    validate_production_settings(settings)
    app = FastAPI(title="Renovex AI Service", version="0.1.0")
    app.state.settings = settings
    app.state.suggestion_service = SuggestionService(_build_provider(settings))
    spatial_provider = _build_spatial_reasoning_provider(settings)
    app.state.spatial_reasoning_service = (
        SpatialReasoningService(spatial_provider) if spatial_provider is not None else None
    )
    reference_image_provider = _build_reference_image_provider(settings)
    app.state.reference_image_service = (
        ReferenceImageService(reference_image_provider, settings.REFERENCE_IMAGE_MAX_BYTES)
        if reference_image_provider is not None
        else None
    )

    @app.exception_handler(AIServiceError)
    def _handle_ai_service_error(request, exc: AIServiceError) -> JSONResponse:
        status_map = {
            "INVALID_AI_REQUEST": 422,
            "PROVIDER_UNAVAILABLE": 503,
            "PROVIDER_RATE_LIMITED": 503,
            "PROVIDER_TIMEOUT": 503,
            "INVALID_PROVIDER_RESPONSE": 502,
            "AI_SERVICE_UNAVAILABLE": 503,
        }
        return JSONResponse(
            status_code=status_map.get(exc.code, 503),
            content={"code": exc.code, "detail": "AI suggestions are temporarily unavailable."},
        )

    @app.get("/health")
    def health() -> dict:
        return {"status": "ok"}

    @app.post(
        "/internal/v1/spaces/suggest",
        response_model=SpaceSuggestionResult,
        dependencies=[Depends(require_internal_token)],
    )
    def suggest_spaces(body: SpaceSuggestionRequest) -> SpaceSuggestionResult:
        service: SuggestionService = app.state.suggestion_service
        return service.suggest_spaces(body)

    @app.post(
        "/internal/v1/work-items/suggest",
        response_model=WorkItemSuggestionResult,
        dependencies=[Depends(require_internal_token)],
    )
    def suggest_work_items(body: WorkItemSuggestionRequest) -> WorkItemSuggestionResult:
        service: SuggestionService = app.state.suggestion_service
        return service.suggest_work_items(body)

    @app.post(
        "/internal/v1/resources/suggest",
        response_model=ResourceSuggestionResult,
        dependencies=[Depends(require_internal_token)],
    )
    def suggest_resources(body: ResourceSuggestionRequest) -> ResourceSuggestionResult:
        service: SuggestionService = app.state.suggestion_service
        return service.suggest_resources(body)

    @app.post(
        "/internal/v1/spatial/element-proposals/reason",
        response_model=SpatialReasoningResult,
        dependencies=[Depends(require_internal_token)],
    )
    def reason_element_proposal(body: dict) -> SpatialReasoningResult:
        # The body is decoded as a raw dict (not directly as
        # SpatialReasoningRequest) so an unknown top-level field or invalid
        # value maps to the SAME INVALID_AI_REQUEST/422 envelope every other
        # validation failure on this route uses, rather than FastAPI's
        # default (differently-shaped) 422 body — the plan requires "route-
        # scoped request validation mapped to INVALID_AI_REQUEST."
        service: SpatialReasoningService | None = app.state.spatial_reasoning_service
        if service is None:
            raise AIServiceError("spatial reasoning is not configured")
        try:
            request = SpatialReasoningRequest(**body)
        except ValidationError as exc:
            raise InvalidAIRequest(f"invalid spatial reasoning request: {exc}") from None
        return service.reason_element(request)

    @app.post(
        "/internal/v1/spatial/reference-images/generate",
        response_model=ReferenceImageResult,
        dependencies=[Depends(require_internal_token)],
    )
    async def generate_reference_image(body: dict) -> ReferenceImageResult:
        # Same "decode as raw dict, map validation failure to
        # INVALID_AI_REQUEST" convention as reason_element_proposal above —
        # one shared route-scoped-validation contract for both AI routes.
        service: ReferenceImageService | None = app.state.reference_image_service
        if service is None:
            raise AIServiceError("reference image generation is not configured")
        try:
            request = ReferenceImageRequest(**body)
        except ValidationError as exc:
            raise InvalidAIRequest(f"invalid reference image request: {exc}") from None
        return await service.generate_reference(request)

    return app
