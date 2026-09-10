"""Proves api/index.py (Vercel's Python runtime entrypoint) actually
imports and exposes a working ASGI app — a broken import here would only
surface at Vercel deploy time otherwise, never locally."""

import importlib
import sys

from fastapi.testclient import TestClient


def test_api_index_exposes_a_working_app(monkeypatch):
    monkeypatch.setenv("AI_PROVIDER", "mock")
    for mod in ("api.index", "app.asgi"):
        sys.modules.pop(mod, None)
    module = importlib.import_module("api.index")

    client = TestClient(module.app)
    response = client.get("/health")

    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
