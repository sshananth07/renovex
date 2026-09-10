"""Vercel Python runtime entrypoint: imports and exposes the existing
FastAPI `app` (see app/asgi.py) under Vercel's conventional api/index.py
path — no separate app construction, so this file can never drift from the
local/E2E entrypoint's Settings-from-environment wiring."""

from app.asgi import app

__all__ = ["app"]
