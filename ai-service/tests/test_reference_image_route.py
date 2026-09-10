"""Route tests for POST /internal/v1/spatial/reference-images/generate.
Mirrors test_spatial_routes.py's exact structure: bearer auth, route-scoped
validation mapped to INVALID_AI_REQUEST, no credential leakage, coexistence
with the other AI routes."""

import json

from fastapi.testclient import TestClient

from app.config import Settings
from app.main import create_app

VALID_REQUEST = {
    "schemaVersion": "1",
    "designSessionId": "ds_1",
    "turnId": "dst_1",
    "planFingerprint": "sha256:abc123",
    "target": {
        "kind": "object",
        "id": "object_sofa_123",
        "category": "sofa",
        "dimensionsMeters": {"width": 2.1, "height": 0.85, "depth": 0.9},
    },
    "assetGenerationSpec": {
        "category": "sofa",
        "shapeDescription": "Curved three-seat sofa with rounded back",
        "preserveCanonicalDimensions": True,
    },
    "materialAppearance": {
        "baseColor": "#315c45",
        "materialFamily": "fabric",
        "roughness": "matte",
        "metallic": False,
    },
    "renderBrief": {
        "view": "three_quarter_front",
        "isolated": True,
        "fullObjectVisible": True,
        "background": "plain_warm_white",
        "noText": True,
        "noPeople": True,
        "noRoom": True,
    },
    "promptVersion": "reference-v1",
    "seed": 4815162342,
}

ROUTE = "/internal/v1/spatial/reference-images/generate"


def _client(reference_image_provider: str = "mock", **overrides) -> TestClient:
    settings = Settings(
        AI_PROVIDER="mock",
        INTERNAL_API_TOKEN="test-internal-token",
        GEMINI_API_KEY="",
        GEMINI_MODEL="gemini-2.0-flash",
        AI_PROVIDER_MAX_RETRIES=2,
        REFERENCE_IMAGE_PROVIDER=reference_image_provider,
        **overrides,
    )
    app = create_app(settings)
    return TestClient(app)


def _auth_headers() -> dict:
    return {"Authorization": "Bearer test-internal-token"}


def test_missing_authorization_is_rejected():
    client = _client()
    resp = client.post(ROUTE, json=VALID_REQUEST)
    assert resp.status_code == 401


def test_wrong_bearer_token_is_rejected():
    client = _client()
    resp = client.post(ROUTE, json=VALID_REQUEST, headers={"Authorization": "Bearer wrong-token"})
    assert resp.status_code == 401


def test_authenticated_request_succeeds_with_mock_provider():
    client = _client()
    resp = client.post(ROUTE, json=VALID_REQUEST, headers=_auth_headers())
    assert resp.status_code == 200
    body = resp.json()
    assert body["provider"] == "mock"
    assert body["seed"] == 4815162342
    assert body["contentType"] == "image/jpeg"


def test_invalid_request_maps_to_invalid_ai_request_422():
    client = _client()
    bad_request = {**VALID_REQUEST, "materialAppearance": {**VALID_REQUEST["materialAppearance"], "baseColor": "dark green"}}
    resp = client.post(ROUTE, json=bad_request, headers=_auth_headers())
    assert resp.status_code == 422
    assert resp.json()["code"] == "INVALID_AI_REQUEST"


def test_unknown_field_in_request_maps_to_invalid_ai_request_422():
    client = _client()
    bad_request = {**VALID_REQUEST, "unexpectedField": "nope"}
    resp = client.post(ROUTE, json=bad_request, headers=_auth_headers())
    assert resp.status_code == 422
    assert resp.json()["code"] == "INVALID_AI_REQUEST"


def test_missing_cloudflare_credentials_makes_route_unavailable_not_service():
    # REFERENCE_IMAGE_PROVIDER=cloudflare with no token configured must make
    # ONLY this route unavailable — never crash app startup or take down
    # Copilot/spatial-reasoning routes (same convention as GLM's missing-key
    # handling).
    client = _client(reference_image_provider="cloudflare")
    resp = client.post(ROUTE, json=VALID_REQUEST, headers=_auth_headers())
    assert resp.status_code == 503

    other_resp = client.post(
        "/internal/v1/spaces/suggest",
        json={"operationId": "op_1", "project": {"id": "p1", "scopeBrief": "renovate"}, "existingSpaces": []},
        headers=_auth_headers(),
    )
    assert other_resp.status_code == 200


def test_error_response_never_leaks_raw_detail_or_credentials():
    client = _client()
    bad_request = {**VALID_REQUEST, "unexpectedField": "nope"}
    resp = client.post(ROUTE, json=bad_request, headers=_auth_headers())
    body_text = json.dumps(resp.json()).lower()
    assert "traceback" not in body_text
    assert "test-internal-token" not in body_text
    assert "cloudflare_api_token" not in body_text


def test_response_never_contains_provider_credentials_or_prompt_text():
    client = _client()
    resp = client.post(ROUTE, json=VALID_REQUEST, headers=_auth_headers())
    body = resp.json()
    # Only the documented ReferenceImageResult fields — no prompt text, no
    # credentials, no arbitrary provider payload passthrough.
    assert set(body.keys()) <= {
        "schemaVersion", "imageBase64", "contentType", "width", "height",
        "provider", "model", "providerRequestId", "seed", "promptVersion",
    }


def test_coexists_with_spatial_reasoning_and_copilot_routes():
    client = _client()
    reference_resp = client.post(ROUTE, json=VALID_REQUEST, headers=_auth_headers())
    space_resp = client.post(
        "/internal/v1/spaces/suggest",
        json={"operationId": "op_1", "project": {"id": "p1", "scopeBrief": "renovate"}, "existingSpaces": []},
        headers=_auth_headers(),
    )
    assert reference_resp.status_code == 200
    assert space_resp.status_code == 200
