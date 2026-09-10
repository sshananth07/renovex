"""CloudflareFluxReferenceImageProvider: the only place that talks to
Cloudflare Workers AI's FLUX.1-schnell model for RP4E2 reference-image
generation. Mirrors GLMProvider's exact discipline (providers/glm.py): one
bounded HTTP call, no retry, no fallback, redirects disabled, persisted
seed passed through unchanged so a Regenerate that reuses the same
seed+prompt is reproducible."""

import base64
import binascii
import struct

import httpx

from app.errors import (
    InvalidProviderResponse,
    ProviderRateLimited,
    ProviderTimeout,
    ProviderUnavailable,
)
from app.prompts.reference_image import REFERENCE_IMAGE_PROMPT_VERSION, build_reference_image_prompt
from app.schemas.reference_image import ReferenceImageRequest, ReferenceImageResult

_RETRYABLE_SERVER_STATUS_CODES = {500, 502, 503, 504}

# JPEG (SOI marker) and PNG (8-byte signature) magic bytes — the smallest
# real structural check available without a full image-decoding dependency.
# Full geometry/dimension validation happens on the Go side once the bytes
# are received (RP4E2 plan: Go validates image signature/dimensions/size).
_JPEG_MAGIC = b"\xff\xd8\xff"
_PNG_MAGIC = b"\x89PNG\r\n\x1a\n"


class CloudflareFluxReferenceImageProvider:
    def __init__(
        self,
        account_id: str,
        api_token: str,
        model: str,
        timeout: float,
        transport: httpx.BaseTransport | None = None,
    ):
        self._account_id = account_id
        self._api_token = api_token
        self._model = model
        self._client = httpx.AsyncClient(timeout=timeout, follow_redirects=False, transport=transport)

    async def generate_reference(self, request: ReferenceImageRequest) -> ReferenceImageResult:
        prompt = build_reference_image_prompt(request)
        url = f"https://api.cloudflare.com/client/v4/accounts/{self._account_id}/ai/run/{self._model}"
        payload = {"prompt": prompt, "seed": request.seed}

        try:
            response = await self._client.post(
                url, json=payload, headers={"Authorization": f"Bearer {self._api_token}"}
            )
        except httpx.TimeoutException:
            raise ProviderTimeout("reference image provider request timed out") from None
        except httpx.HTTPError:
            raise ProviderUnavailable("reference image provider request failed") from None

        if response.status_code == 429:
            raise ProviderRateLimited("reference image provider rate limit exceeded")
        if response.status_code in _RETRYABLE_SERVER_STATUS_CODES:
            raise ProviderUnavailable("reference image provider temporarily unavailable")
        if response.is_redirect:
            raise InvalidProviderResponse("reference image provider returned an unexpected redirect")
        if response.status_code != 200:
            raise ProviderUnavailable("reference image provider request failed")

        try:
            body = response.json()
        except ValueError:
            raise InvalidProviderResponse("reference image provider returned invalid structured output") from None

        image_base64 = _extract_image_base64(body)
        if image_base64 is None:
            raise InvalidProviderResponse("reference image provider returned no image data")

        try:
            image_bytes = base64.b64decode(image_base64, validate=True)
        except (binascii.Error, ValueError):
            raise InvalidProviderResponse("reference image provider returned invalid base64 image data") from None

        content_type = _detect_content_type(image_bytes)
        if content_type is None:
            raise InvalidProviderResponse("reference image provider returned an unrecognized image format")

        dimensions = _read_image_dimensions(image_bytes, content_type)
        if dimensions is None:
            raise InvalidProviderResponse("reference image provider returned an image with unreadable dimensions")
        width, height = dimensions

        return ReferenceImageResult(
            schemaVersion="1",
            imageBase64=image_base64,
            contentType=content_type,
            width=width,
            height=height,
            provider="cloudflare_flux",
            model=self._model,
            providerRequestId=_safe_request_id(body),
            seed=request.seed,
            promptVersion=REFERENCE_IMAGE_PROMPT_VERSION,
        )


def _extract_image_base64(body: dict) -> str | None:
    # Workers AI's documented response shape for image models is
    # {"result": {"image": "<base64>"}, "success": true, ...}.
    result = body.get("result")
    if isinstance(result, dict):
        image = result.get("image")
        if isinstance(image, str) and image:
            return image
    return None


def _safe_request_id(body: dict) -> str | None:
    errors = body.get("errors")
    if isinstance(errors, list) and errors:
        return None
    request_id = body.get("request_id") or body.get("id")
    if isinstance(request_id, str):
        return request_id[:200]
    return None


def _detect_content_type(image_bytes: bytes) -> str | None:
    if image_bytes.startswith(_JPEG_MAGIC):
        return "image/jpeg"
    if image_bytes.startswith(_PNG_MAGIC):
        return "image/png"
    return None


# ponytail: hand-rolled header parsing (no Pillow dependency) — sufficient
# for reading width/height from a well-formed JPEG/PNG; add a real image
# library if this ever needs to decode pixel data or handle more formats.
def _read_image_dimensions(image_bytes: bytes, content_type: str) -> tuple[int, int] | None:
    if content_type == "image/png":
        return _read_png_dimensions(image_bytes)
    if content_type == "image/jpeg":
        return _read_jpeg_dimensions(image_bytes)
    return None


def _read_png_dimensions(image_bytes: bytes) -> tuple[int, int] | None:
    # PNG: 8-byte signature, then the IHDR chunk (4-byte length + "IHDR" +
    # 4-byte width + 4-byte height), always the first chunk.
    if len(image_bytes) < 24 or image_bytes[12:16] != b"IHDR":
        return None
    width, height = struct.unpack(">II", image_bytes[16:24])
    if width <= 0 or height <= 0:
        return None
    return width, height


def _read_jpeg_dimensions(image_bytes: bytes) -> tuple[int, int] | None:
    # JPEG: walk marker segments looking for a Start-Of-Frame (SOFn) marker,
    # which carries height/width right after its 2-byte length + 1-byte
    # precision. 0xC4/0xC8/0xCC are not SOF markers despite being in the
    # 0xC0-0xCF range (DHT/JPG/DAC) and are explicitly excluded.
    offset = 2  # skip the SOI marker (0xFFD8)
    length = len(image_bytes)
    while offset + 4 <= length:
        if image_bytes[offset] != 0xFF:
            return None
        marker = image_bytes[offset + 1]
        if marker in (0xD8, 0x01) or 0xD0 <= marker <= 0xD7:
            offset += 2
            continue
        if offset + 4 > length:
            return None
        segment_length = struct.unpack(">H", image_bytes[offset + 2 : offset + 4])[0]
        if 0xC0 <= marker <= 0xCF and marker not in (0xC4, 0xC8, 0xCC):
            if offset + 9 > length:
                return None
            height, width = struct.unpack(">HH", image_bytes[offset + 5 : offset + 9])
            if width <= 0 or height <= 0:
                return None
            return width, height
        if marker == 0xD9:  # EOI reached with no SOF found
            return None
        offset += 2 + segment_length
    return None
