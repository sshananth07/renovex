"""Secret/error redaction proof (design doc section 21.2 / plan Task 2 Step
7): GEMINI_API_KEY, INTERNAL_API_TOKEN, and raw provider response bodies
must never appear in a contractor-safe error response or in captured logs.

RP4E1 adds the equivalent proof for GLM_API_KEY and GLM reasoning_content."""

import json
import logging
from pathlib import Path
from unittest.mock import MagicMock

import httpx
from fastapi.testclient import TestClient
from google.genai import errors as genai_errors

from app.config import Settings
from app.main import create_app
from app.providers.gemini import GeminiProvider

FIXTURES_DIR = Path(__file__).parent / "fixtures" / "spatial_reasoning"


def test_provider_failure_response_has_no_secrets_or_traceback(caplog):
    settings = Settings(
        AI_PROVIDER="gemini",
        INTERNAL_API_TOKEN="super-secret-internal-token",
        GEMINI_API_KEY="super-secret-gemini-key",
        GEMINI_MODEL="gemini-2.0-flash",
        AI_PROVIDER_MAX_RETRIES=0,
    )
    app = create_app(settings)

    fake_client = MagicMock()
    fake_client.models.generate_content.side_effect = genai_errors.ServerError(
        503, {"error": {"message": "super-secret-gemini-key leaked in body", "status": "UNAVAILABLE"}}
    )
    app.state.suggestion_service._provider = GeminiProvider(settings, client=fake_client)

    client = TestClient(app, raise_server_exceptions=False)
    with caplog.at_level(logging.DEBUG):
        resp = client.post(
            "/internal/v1/spaces/suggest",
            headers={"Authorization": "Bearer super-secret-internal-token"},
            json={
                "operationId": "op_1",
                "project": {"id": "p1", "scopeBrief": "Full renovation."},
                "existingSpaces": [],
            },
        )

    assert resp.status_code in (502, 503)
    body_text = resp.text
    assert "super-secret-gemini-key" not in body_text
    assert "super-secret-internal-token" not in body_text
    assert "Traceback" not in body_text
    assert "traceback" not in body_text.lower()

    log_text = caplog.text
    assert "super-secret-gemini-key" not in log_text
    assert "super-secret-internal-token" not in log_text


def test_glm_provider_failure_response_has_no_secrets_reasoning_or_traceback(caplog):
    """RP4E1: GLM_API_KEY and reasoning_content (GLM's chain-of-thought
    field) must never reach a contractor-safe error response or logs, even
    when the provider's raw response body contains them."""
    settings = Settings(
        AI_PROVIDER="mock",
        INTERNAL_API_TOKEN="super-secret-internal-token",
        SPATIAL_AI_PROVIDER="glm",
        GLM_API_KEY="super-secret-glm-key",
    )
    app = create_app(settings)

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            503,
            json={"error": "super-secret-glm-key leaked in body", "reasoning_content": "secret chain of thought"},
        )

    from app.providers.glm import GLMProvider

    app.state.spatial_reasoning_service._provider = GLMProvider(
        api_key=settings.GLM_API_KEY,
        model=settings.GLM_MODEL,
        base_url=settings.GLM_BASE_URL,
        reasoning_effort=settings.GLM_REASONING_EFFORT,
        timeout=5.0,
        transport=httpx.MockTransport(handler),
    )

    request_body = json.loads((FIXTURES_DIR / "material_only.json").read_text())["request"]

    client = TestClient(app, raise_server_exceptions=False)
    with caplog.at_level(logging.DEBUG):
        resp = client.post(
            "/internal/v1/spatial/element-proposals/reason",
            headers={"Authorization": "Bearer super-secret-internal-token"},
            json=request_body,
        )

    assert resp.status_code in (502, 503)
    body_text = resp.text
    assert "super-secret-glm-key" not in body_text
    assert "secret chain of thought" not in body_text
    assert "super-secret-internal-token" not in body_text
    assert "traceback" not in body_text.lower()

    log_text = caplog.text
    assert "super-secret-glm-key" not in log_text
    assert "secret chain of thought" not in log_text
