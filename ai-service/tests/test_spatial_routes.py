"""Route tests for POST /internal/v1/spatial/element-proposals/reason.
Proves bearer auth, fixed sanitized errors, route-scoped request validation
mapped to INVALID_AI_REQUEST, and coexistence with all three Copilot routes
(RP4E1 plan Task 3 Step 7)."""

import json
from pathlib import Path

from fastapi.testclient import TestClient

from app.config import Settings
from app.main import create_app

FIXTURES_DIR = Path(__file__).parent / "fixtures" / "spatial_reasoning"


def _client(spatial_ai_provider: str = "mock") -> TestClient:
    settings = Settings(
        AI_PROVIDER="mock",
        INTERNAL_API_TOKEN="test-internal-token",
        GEMINI_API_KEY="",
        GEMINI_MODEL="gemini-2.0-flash",
        AI_PROVIDER_MAX_RETRIES=2,
        SPATIAL_AI_PROVIDER=spatial_ai_provider,
    )
    app = create_app(settings)
    return TestClient(app)


def _fixture_request(name: str) -> dict:
    return json.loads((FIXTURES_DIR / name).read_text())["request"]


def _auth_headers() -> dict:
    return {"Authorization": "Bearer test-internal-token"}


def test_missing_authorization_is_rejected():
    client = _client()
    resp = client.post(
        "/internal/v1/spatial/element-proposals/reason", json=_fixture_request("material_only.json")
    )
    assert resp.status_code == 401


def test_wrong_bearer_token_is_rejected():
    client = _client()
    resp = client.post(
        "/internal/v1/spatial/element-proposals/reason",
        json=_fixture_request("material_only.json"),
        headers={"Authorization": "Bearer wrong-token"},
    )
    assert resp.status_code == 401


def test_authenticated_request_succeeds_with_mock_provider():
    client = _client()
    resp = client.post(
        "/internal/v1/spatial/element-proposals/reason",
        json=_fixture_request("material_only.json"),
        headers=_auth_headers(),
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["delta"]["target"]["id"] == "object_sofa_123"
    assert body["provider"] == "mock"


def test_invalid_request_maps_to_invalid_ai_request_422():
    client = _client()
    bad_request = _fixture_request("material_only.json")
    bad_request["instruction"] = ""
    resp = client.post(
        "/internal/v1/spatial/element-proposals/reason", json=bad_request, headers=_auth_headers()
    )
    assert resp.status_code == 422
    assert resp.json()["code"] == "INVALID_AI_REQUEST"


def test_unknown_field_in_request_maps_to_invalid_ai_request_422():
    client = _client()
    bad_request = _fixture_request("material_only.json")
    bad_request["unexpectedField"] = "nope"
    resp = client.post(
        "/internal/v1/spatial/element-proposals/reason", json=bad_request, headers=_auth_headers()
    )
    assert resp.status_code == 422
    assert resp.json()["code"] == "INVALID_AI_REQUEST"


def test_error_response_never_leaks_raw_detail():
    client = _client()
    bad_request = _fixture_request("material_only.json")
    bad_request["instruction"] = ""
    resp = client.post(
        "/internal/v1/spatial/element-proposals/reason", json=bad_request, headers=_auth_headers()
    )
    body = resp.json()
    assert "traceback" not in json.dumps(body).lower()


def test_coexists_with_all_three_copilot_routes():
    client = _client()
    space_resp = client.post(
        "/internal/v1/spaces/suggest",
        json={"operationId": "op_1", "project": {"id": "p1", "scopeBrief": "renovate"}, "existingSpaces": []},
        headers=_auth_headers(),
    )
    work_items_resp = client.post(
        "/internal/v1/work-items/suggest",
        json={"operationId": "op_2", "projectBrief": "renovate", "spaces": [], "existingWorkItems": []},
        headers=_auth_headers(),
    )
    resources_resp = client.post(
        "/internal/v1/resources/suggest",
        json={"operationId": "op_3", "workItems": [], "materialCandidates": [], "existingRequirements": []},
        headers=_auth_headers(),
    )
    spatial_resp = client.post(
        "/internal/v1/spatial/element-proposals/reason",
        json=_fixture_request("material_only.json"),
        headers=_auth_headers(),
    )
    assert space_resp.status_code == 200
    assert work_items_resp.status_code == 200
    assert resources_resp.status_code == 200
    assert spatial_resp.status_code == 200
