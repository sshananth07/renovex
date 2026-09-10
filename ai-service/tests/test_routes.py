"""Route-level tests for the three internal suggestion endpoints."""

from fastapi.testclient import TestClient

from app.config import Settings
from app.main import create_app

TOKEN = "test-internal-token"


def _client() -> TestClient:
    settings = Settings(
        AI_PROVIDER="mock",
        INTERNAL_API_TOKEN=TOKEN,
        GEMINI_API_KEY="",
        GEMINI_MODEL="gemini-2.0-flash",
        AI_PROVIDER_MAX_RETRIES=2,
    )
    return TestClient(create_app(settings))


def _auth():
    return {"Authorization": f"Bearer {TOKEN}"}


def test_suggest_spaces_returns_metadata_and_suggestions():
    client = _client()
    resp = client.post(
        "/internal/v1/spaces/suggest",
        headers=_auth(),
        json={
            "operationId": "op_1",
            "project": {"id": "project_1", "scopeBrief": "Full renovation of a condo."},
            "existingSpaces": [],
        },
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["provider"] == "mock"
    assert body["promptVersion"] == "spaces-v1"
    assert body["schemaVersion"] == 1
    assert len(body["suggestions"]) >= 3


def test_suggest_work_items_returns_suggestions():
    client = _client()
    resp = client.post(
        "/internal/v1/work-items/suggest",
        headers=_auth(),
        json={
            "operationId": "op_2",
            "projectBrief": "Full renovation of a condo.",
            "spaces": [{"id": "space_123", "name": "Kitchen", "type": "kitchen"}],
            "existingWorkItems": [],
        },
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["promptVersion"] == "work-items-v1"
    assert len(body["suggestions"]) > 0


def test_suggest_resources_returns_suggestions():
    client = _client()
    resp = client.post(
        "/internal/v1/resources/suggest",
        headers=_auth(),
        json={
            "operationId": "op_3",
            "workItems": [{"id": "work_1", "description": "Install ceramic floor tiles"}],
            "materialCandidates": [],
            "existingRequirements": [],
        },
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["promptVersion"] == "resources-v1"
    assert len(body["suggestions"]) > 0


def test_malformed_request_returns_422():
    client = _client()
    resp = client.post(
        "/internal/v1/spaces/suggest",
        headers=_auth(),
        json={"operationId": "op_1"},
    )
    assert resp.status_code == 422
