"""T2D fail-fast tests: ENVIRONMENT=production must never silently boot
with a provider still at its offline/mock default — mirrors the Go
backend's own APP_ENV=production config guards."""

import pytest

from app.config import Settings
from app.main import validate_production_settings


def _settings(**overrides) -> Settings:
    data = {"INTERNAL_API_TOKEN": "test-token"}
    data.update(overrides)
    return Settings(**data)


def test_development_permits_every_mock_default():
    settings = _settings()  # ENVIRONMENT defaults to "development"
    validate_production_settings(settings)  # must not raise


def test_production_rejects_mock_ai_provider():
    settings = _settings(ENVIRONMENT="production", AI_PROVIDER="mock")
    with pytest.raises(RuntimeError):
        validate_production_settings(settings)


def test_production_rejects_mock_spatial_ai_provider():
    settings = _settings(ENVIRONMENT="production", AI_PROVIDER="gemini", SPATIAL_AI_PROVIDER="mock")
    with pytest.raises(RuntimeError):
        validate_production_settings(settings)


def test_production_rejects_mock_reference_image_provider():
    settings = _settings(
        ENVIRONMENT="production", AI_PROVIDER="gemini",
        SPATIAL_AI_PROVIDER="glm", REFERENCE_IMAGE_PROVIDER="mock",
    )
    with pytest.raises(RuntimeError):
        validate_production_settings(settings)


def test_production_with_every_provider_configured_succeeds():
    settings = _settings(
        ENVIRONMENT="production",
        AI_PROVIDER="gemini",
        SPATIAL_AI_PROVIDER="glm",
        REFERENCE_IMAGE_PROVIDER="cloudflare",
    )
    validate_production_settings(settings)  # must not raise
