"""ReferenceImageProvider protocol — mirrors SpatialReasoningProvider's
provider-neutral Protocol convention (services/spatial_reasoning.py).
Implementations raise the shared app.errors.AIServiceError subclasses on
failure; there is no retry at this layer (RP4E2 plan: "does no automatic
retry")."""

from typing import Protocol

from app.schemas.reference_image import ReferenceImageRequest, ReferenceImageResult


class ReferenceImageProvider(Protocol):
    async def generate_reference(self, request: ReferenceImageRequest) -> ReferenceImageResult: ...
