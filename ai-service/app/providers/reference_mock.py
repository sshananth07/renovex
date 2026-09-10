"""Deterministic, offline ReferenceImageProvider used for automated tests
and local development (REFERENCE_IMAGE_PROVIDER=mock) — mirrors
providers/spatial_mock.py's own convention exactly. Output bytes are a
pure function of (planFingerprint, seed): the same pair always yields the
same image bytes, no randomness, no wall-clock dependence, no real network
call. Not a real image model.
"""

import hashlib

from app.prompts.reference_image import REFERENCE_IMAGE_PROMPT_VERSION
from app.schemas.reference_image import ReferenceImageRequest, ReferenceImageResult

# A minimal valid 1x1 JPEG, used as deterministic placeholder bytes — real
# JPEG magic bytes/structure so downstream signature validation (Go's
# structural check) accepts it as a genuine (if trivial) JPEG, matching how
# a real FLUX response would structurally validate.
_MINIMAL_JPEG_BASE64 = (
    "/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAMCAgICAgMCAgIDAwMDBAYEBAQEBAgGBgUGCQgKCgkI"
    "CQkKDA8MCgsOCwkJDRENDg8QEBEQCgwSExIQEw8QEBD/2wBDAQMDAwQDBAgEBAgQCwkLEBAQEBAQ"
    "EBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBD/wAARCAABAAEDASIA"
    "AhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAj/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/8QAFQEB"
    "AQAAAAAAAAAAAAAAAAAAAAX/xAAUEQEAAAAAAAAAAAAAAAAAAAAA/9oADAMBAAIRAxEAPwCdABmX"
    "/9k="
)


class MockReferenceImageProvider:
    async def generate_reference(self, request: ReferenceImageRequest) -> ReferenceImageResult:
        # The deterministic key (planFingerprint + seed) is used only to
        # prove determinism in tests, not to vary the returned bytes — a
        # real image model would vary output per key; the mock keeps fixed
        # placeholder bytes for simplicity and documents the key it would
        # hash on.
        digest = hashlib.sha256(f"{request.planFingerprint}:{request.seed}".encode()).hexdigest()

        return ReferenceImageResult(
            schemaVersion="1",
            imageBase64=_MINIMAL_JPEG_BASE64,
            contentType="image/jpeg",
            width=1,
            height=1,
            provider="mock",
            model="reference-mock-v1",
            providerRequestId=digest[:16],
            seed=request.seed,
            promptVersion=REFERENCE_IMAGE_PROMPT_VERSION,
        )
