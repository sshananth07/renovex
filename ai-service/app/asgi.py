"""ASGI entrypoint for running the service locally/in E2E: `uvicorn app.asgi:app`.
Reads Settings from the process environment/.env file; create_app itself stays
environment-agnostic for tests (see main.py)."""

from app.config import Settings
from app.main import create_app

app = create_app(Settings())
