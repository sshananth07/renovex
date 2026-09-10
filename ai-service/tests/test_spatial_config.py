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
    assert settings.GLM_MODEL == "glm-5.3"
    assert settings.GLM_BASE_URL == "https://api.z.ai/api/paas/v4"
    assert settings.GLM_API_KEY == ""


def test_glm_reasoning_effort_default():
    settings = _settings()
    assert settings.GLM_REASONING_EFFORT == "low"
