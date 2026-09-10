"""Internal-auth tests for /internal/v1/* routes.

Proves: missing Authorization -> 401, wrong bearer token -> 401, correct
bearer token -> request reaches the route, and /health responds without the
internal token while exposing no business data.
"""

from fastapi.testclient import TestClient

from app.config import Settings
from app.main import create_app


def _client(token: str = "test-internal-token") -> TestClient:
    settings = Settings(
        AI_PROVIDER="mock",
        INTERNAL_API_TOKEN=token,
        GEMINI_API_KEY="",
        GEMINI_MODEL="gemini-2.0-flash",
        AI_PROVIDER_MAX_RETRIES=2,
    )
    app = create_app(settings)
    return TestClient(app)


def _space_request_body() -> dict:
    return {
        "operationId": "op_1",
        "project": {"id": "project_1", "scopeBrief": "Full renovation of a condo."},
        "existingSpaces": [],
    }


def test_missing_authorization_is_rejected():
    client = _client()
    resp = client.post("/internal/v1/spaces/suggest", json=_space_request_body())
    assert resp.status_code == 401


def test_wrong_bearer_token_is_rejected():
    client = _client()
    resp = client.post(
        "/internal/v1/spaces/suggest",
        json=_space_request_body(),
        headers={"Authorization": "Bearer wrong-token"},
    )
    assert resp.status_code == 401


def test_correct_bearer_token_reaches_route():
    client = _client()
    resp = client.post(
        "/internal/v1/spaces/suggest",
        json=_space_request_body(),
        headers={"Authorization": "Bearer test-internal-token"},
    )
    assert resp.status_code == 200


def test_health_does_not_require_internal_token():
    client = _client()
    resp = client.get("/health")
    assert resp.status_code == 200


def test_health_exposes_no_business_data():
    client = _client()
    resp = client.get("/health")
    body = resp.json()
    assert "suggestions" not in body
    assert "provider" not in body
    assert "scopeBrief" not in str(body)


def test_rejected_request_never_invokes_the_provider(monkeypatch):
    """Proves 401 rejection happens in the auth dependency, before the route
    handler (and therefore the provider) ever runs (design doc plan Task 18
    Step 1) — not just that the response code is 401."""
    from app.providers.mock import MockProvider

    def _fail_if_called(self, request):
        raise AssertionError("MockProvider.suggest_spaces must not be invoked for an unauthenticated request")

    monkeypatch.setattr(MockProvider, "suggest_spaces", _fail_if_called)

    client = _client()
    resp = client.post("/internal/v1/spaces/suggest", json=_space_request_body())
    assert resp.status_code == 401


def test_spatial_route_missing_authorization_is_rejected():
    """RP4E1: the internal spatial reasoning route uses the SAME
    require_internal_token dependency as the three Copilot routes — no
    separate auth surface."""
    client = _client()
    resp = client.post(
        "/internal/v1/spatial/element-proposals/reason",
        json={
            "schemaVersion": 1,
            "turnId": "t1",
            "roomDraftId": "rd1",
            "roomDraftRevision": 1,
            "selectedElement": {
                "kind": "object", "id": "o1", "category": "sofa",
                "transform": {"position": {"x": 0, "y": 0, "z": 0}, "rotation": {"x": 0, "y": 0, "z": 0, "w": 1}},
                "visualAssetBound": False,
            },
            "context": {"walls": [], "openings": [], "neighbors": []},
            "currentWorkingDesign": {"resolvedSpatialOperations": []},
            "lastSuccessfulPlanSummary": [],
            "instruction": "make it beige",
            "allowedSpatialOperations": ["resize_axis"],
            "materialFamilyEnum": ["fabric"],
            "roughnessEnum": ["matte"],
            "preservationDefaults": {"geometry": True, "material": True, "spatial": True},
        },
    )
    assert resp.status_code == 401


def test_authenticated_request_does_invoke_the_provider(monkeypatch):
    """Companion to the rejection test above: confirms the monkeypatch
    technique actually detects invocation (a correctly-authenticated request
    must reach the provider)."""
    from app.providers.mock import MockProvider

    calls = []
    original = MockProvider.suggest_spaces

    def _record_and_call(self, request):
        calls.append(request)
        return original(self, request)

    monkeypatch.setattr(MockProvider, "suggest_spaces", _record_and_call)

    client = _client()
    resp = client.post(
        "/internal/v1/spaces/suggest",
        json=_space_request_body(),
        headers={"Authorization": "Bearer test-internal-token"},
    )
    assert resp.status_code == 200
    assert len(calls) == 1
