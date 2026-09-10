"""Provider-neutral orchestration for RP4E2 reference-image generation,
mirroring services/spatial_reasoning.py's thin-orchestration convention.
The provider already validates its own structural output (base64 decoding,
image-signature detection); this service adds only the bounded-size check
that applies uniformly regardless of which provider produced the bytes."""

import base64

from app.errors import InvalidProviderResponse
from app.providers.reference_base import ReferenceImageProvider
from app.schemas.reference_image import ReferenceImageRequest, ReferenceImageResult


class ReferenceImageService:
    def __init__(self, provider: ReferenceImageProvider, max_bytes: int):
        self._provider = provider
        self._max_bytes = max_bytes

    async def generate_reference(self, request: ReferenceImageRequest) -> ReferenceImageResult:
        result = await self._provider.generate_reference(request)

        # Decode once more here (rather than trusting the provider's own
        # decode) so this bound is enforced uniformly across every
        # provider, present and future — the provider's job is producing a
        # structurally valid image; the service's job is enforcing the
        # shared size contract before Go ever sees the bytes.
        decoded_size = len(base64.b64decode(result.imageBase64, validate=True))
        if decoded_size > self._max_bytes:
            raise InvalidProviderResponse(
                f"reference image exceeds maximum allowed size ({decoded_size} > {self._max_bytes} bytes)"
            )

        return result
