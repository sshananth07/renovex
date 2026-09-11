"""Proves Vercel's zero-config app/asgi.py entrypoint actually
imports and exposes a working ASGI app — a broken import here would only
surface at Vercel deploy time otherwise, never locally."""

import importlib
import sys

from fastapi.testclient import TestClient


def test_asgi_entrypoint_exposes_a_working_app(monkeypatch):
    monkeypatch.setenv("AI_PROVIDER", "mock")
    for mod in ("app.asgi",):
        sys.modules.pop(mod, None)
    module = importlib.import_module("app.asgi")

    client = TestClient(module.app)
    response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}

    protected_response = client.post("/internal/v1/spaces/suggest", json={})
    assert protected_response.status_code == 401
