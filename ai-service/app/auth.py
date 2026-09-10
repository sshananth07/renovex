"""Internal-service bearer authentication for /internal/v1/* routes.

Compares tokens with secrets.compare_digest to avoid timing side-channels.
Never logs the token value.
"""

import secrets

from fastapi import Header, HTTPException, Request


def require_internal_token(request: Request, authorization: str = Header(default="")) -> None:
    settings = request.app.state.settings
    if not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="missing or malformed Authorization header")
    presented = authorization.removeprefix("Bearer ")
    if not secrets.compare_digest(presented, settings.INTERNAL_API_TOKEN):
        raise HTTPException(status_code=401, detail="invalid internal service token")
