"""Config tests for RP4E1's isolated spatial/GLM settings. SPATIAL_AI_PROVIDER
is separate from AI_PROVIDER (the existing Copilot suggestion routes) so a
missing/invalid GLM configuration can never affect Copilot (plan Task 3
Step 8)."""

from app.config import Settings


def _settings(**overrides) -> Settings:
    data = {"INTERNAL_API_TOKEN": "test-token"}
    data.update(overrides)
    return Settings(**data)


def test_spatial_ai_provider_defaults_to_mock():
    settings = _settings()
    assert settings.SPATIAL_AI_PROVIDER == "mock"


def test_spatial_ai_provider_independent_of_copilot_ai_provider():
    settings = _settings(AI_PROVIDER="gemini", SPATIAL_AI_PROVIDER="mock")
    assert settings.AI_PROVIDER == "gemini"
    assert settings.SPATIAL_AI_PROVIDER == "mock"


def test_glm_defaults_present_without_requiring_api_key():
    settings = _settings()
    assert settings.GLM_MODEL == "glm-4.7-flash"
    assert settings.GLM_BASE_URL == "https://api.z.ai/api/paas/v4"
    assert settings.GLM_API_KEY == ""


def test_glm_reasoning_effort_default():
    settings = _settings()
    assert settings.GLM_REASONING_EFFORT == "low"


def test_glm_timeout_seconds_defaults_to_90_and_is_configurable():
    """Production evidence: a GLM chat-completions request timed out at
    exactly the previous 30s default (glm_spatial_request_failed
    duration_ms=30078) — Z.ai occasionally needs longer than 30s to
    respond, even for a valid, successful request. Raising the default to
    90s is the smallest production-safe change; it must stay overridable
    via GLM_TIMEOUT_SECONDS, matching every other timeout in this config
    (REFERENCE_IMAGE_TIMEOUT_SECONDS)."""
    settings = _settings()
    assert settings.GLM_TIMEOUT_SECONDS == 90.0

    overridden = _settings(GLM_TIMEOUT_SECONDS=45.0)
    assert overridden.GLM_TIMEOUT_SECONDS == 45.0
