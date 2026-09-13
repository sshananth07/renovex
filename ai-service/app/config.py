"""Runtime configuration. Never log or persist secret values (INTERNAL_API_TOKEN,
GEMINI_API_KEY) — see design doc "Internal Service Security"."""

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    # T2D: mirrors the Go backend's APP_ENV — "development" (default) or
    # "production". This service previously had no production-environment
    # concept at all, so nothing stopped a real deployment from silently
    # booting with every provider still at its "mock"/offline default.
    ENVIRONMENT: str = "development"

    AI_PROVIDER: str = "mock"
    INTERNAL_API_TOKEN: str
    GEMINI_API_KEY: str = ""
    GEMINI_MODEL: str = "gemini-2.5-flash"
    AI_PROVIDER_MAX_RETRIES: int = 2

    # RP4E1: an isolated provider selector for the spatial-reasoning route,
    # deliberately separate from AI_PROVIDER (the existing Copilot
    # suggestion routes). "mock" is the default so the app never requires a
    # real GLM API key to run or test. GLM settings are read only when
    # SPATIAL_AI_PROVIDER=glm — Go never reads GLM_API_KEY; only this
    # Python service owns the provider key/model/base URL (plan Task 8
    # Step 2).
    SPATIAL_AI_PROVIDER: str = "mock"
    GLM_API_KEY: str = ""
    GLM_MODEL: str = "glm-4.7-flash"
    GLM_BASE_URL: str = "https://api.z.ai/api/paas/v4"
    # 90s (up from the original 30s): production evidence showed a valid
    # request timing out at exactly the 30s default
    # (glm_spatial_request_failed duration_ms=30078) — Z.ai occasionally
    # needs longer than 30s under glm-4.7-flash even for a successful
    # response. Still fully overridable via GLM_TIMEOUT_SECONDS.
    GLM_TIMEOUT_SECONDS: float = 90.0
    GLM_MAX_OUTPUT_TOKENS: int = 2000
    GLM_REASONING_EFFORT: str = "low"

    # RP4E2: an isolated provider selector for reference-image generation,
    # deliberately separate from SPATIAL_AI_PROVIDER — "mock" is the
    # default so the app never requires real Cloudflare credentials to run
    # or test. Cloudflare credentials are enforced together only when
    # REFERENCE_IMAGE_PROVIDER=cloudflare (see main.py's provider builder);
    # a missing/invalid credential makes ONLY the reference-image route
    # unavailable, never the rest of the service.
    REFERENCE_IMAGE_PROVIDER: str = "mock"
    CLOUDFLARE_ACCOUNT_ID: str = ""
    CLOUDFLARE_API_TOKEN: str = ""
    CLOUDFLARE_FLUX_MODEL: str = "@cf/black-forest-labs/flux-1-schnell"
    REFERENCE_IMAGE_TIMEOUT_SECONDS: float = 45.0
    REFERENCE_IMAGE_MAX_BYTES: int = 8_388_608
