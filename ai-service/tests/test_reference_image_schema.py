"""Strict schema invariant tests for RP4E2 reference-image models — same
"unknown fields/type coercion/NaN fail closed" convention as
test_spatial_schemas.py."""

from typing import ClassVar

import pytest
from pydantic import ValidationError

from app.schemas.reference_image import (
    MaterialAppearance,
    ReferenceImageRequest,
    ReferenceImageResult,
    ReferenceTarget,
)

VALID_REQUEST = {
    "schemaVersion": "1",
    "designSessionId": "ds_1",
    "turnId": "dst_1",
    "planFingerprint": "sha256:abc123",
    "target": {
        "kind": "object",
        "id": "object_sofa_123",
        "category": "sofa",
        "dimensionsMeters": {"width": 2.1, "height": 0.85, "depth": 0.9},
    },
    "assetGenerationSpec": {
        "category": "sofa",
        "shapeDescription": "Curved three-seat sofa with rounded back",
        "preserveCanonicalDimensions": True,
    },
    "materialAppearance": {
        "baseColor": "#315c45",
        "materialFamily": "fabric",
        "roughness": "matte",
        "metallic": False,
    },
    "renderBrief": {
        "view": "three_quarter_front",
        "isolated": True,
        "fullObjectVisible": True,
        "background": "plain_warm_white",
        "noText": True,
        "noPeople": True,
        "noRoom": True,
    },
    "promptVersion": "reference-v1",
    "seed": 4815162342,
}


class TestReferenceImageRequestValid:
    def test_valid_full_request(self):
        ReferenceImageRequest(**VALID_REQUEST)

    def test_valid_without_material_appearance(self):
        data = {**VALID_REQUEST}
        del data["materialAppearance"]
        ReferenceImageRequest(**data)

    def test_valid_without_dimensions(self):
        data = {**VALID_REQUEST, "target": {**VALID_REQUEST["target"]}}
        del data["target"]["dimensionsMeters"]
        ReferenceImageRequest(**data)


class TestReferenceImageRequestStrictness:
    def test_rejects_unknown_top_level_field(self):
        with pytest.raises(ValidationError):
            ReferenceImageRequest(**VALID_REQUEST, unexpectedField="nope")

    def test_rejects_wrong_schema_version(self):
        with pytest.raises(ValidationError):
            ReferenceImageRequest(**{**VALID_REQUEST, "schemaVersion": "2"})

    def test_rejects_unsupported_target_kind(self):
        data = {**VALID_REQUEST, "target": {**VALID_REQUEST["target"], "kind": "wall"}}
        with pytest.raises(ValidationError):
            ReferenceImageRequest(**data)

    def test_rejects_nonpositive_dimensions(self):
        data = {
            **VALID_REQUEST,
            "target": {**VALID_REQUEST["target"], "dimensionsMeters": {"width": 0, "height": 0.85, "depth": 0.9}},
        }
        with pytest.raises(ValidationError):
            ReferenceImageRequest(**data)

    def test_rejects_nan_dimension(self):
        data = {
            **VALID_REQUEST,
            "target": {**VALID_REQUEST["target"], "dimensionsMeters": {"width": float("nan"), "height": 0.85, "depth": 0.9}},
        }
        with pytest.raises(ValidationError):
            ReferenceImageRequest(**data)

    def test_rejects_unsupported_render_view(self):
        data = {**VALID_REQUEST, "renderBrief": {**VALID_REQUEST["renderBrief"], "view": "top_down"}}
        with pytest.raises(ValidationError):
            ReferenceImageRequest(**data)

    def test_rejects_unsupported_background(self):
        data = {**VALID_REQUEST, "renderBrief": {**VALID_REQUEST["renderBrief"], "background": "studio_black"}}
        with pytest.raises(ValidationError):
            ReferenceImageRequest(**data)


class TestMaterialAppearanceBaseColor:
    def test_accepts_canonical_hex(self):
        MaterialAppearance(baseColor="#315c45", materialFamily="fabric", roughness="matte", metallic=False)

    def test_rejects_color_name(self):
        with pytest.raises(ValidationError):
            MaterialAppearance(baseColor="dark green", materialFamily="fabric", roughness="matte", metallic=False)

    def test_rejects_arbitrary_content_type_field_on_result(self):
        # ReferenceTarget is a separate strict model — extra fields still
        # fail closed on it directly.
        with pytest.raises(ValidationError):
            ReferenceTarget(kind="object", id="o1", category="sofa", extra="nope")


class TestReferenceImageResultStrictness:
    VALID_RESULT: ClassVar[dict] = {
        "schemaVersion": "1",
        "imageBase64": "abc123==",
        "contentType": "image/jpeg",
        "width": 1024,
        "height": 1024,
        "provider": "cloudflare_flux",
        "model": "@cf/black-forest-labs/flux-1-schnell",
        "providerRequestId": "req_1",
        "seed": 4815162342,
        "promptVersion": "reference-v1",
    }

    def test_valid_full_result(self):
        ReferenceImageResult(**self.VALID_RESULT)

    def test_valid_without_provider_request_id(self):
        data = {**self.VALID_RESULT}
        del data["providerRequestId"]
        ReferenceImageResult(**data)

    def test_rejects_unsupported_content_type(self):
        with pytest.raises(ValidationError):
            ReferenceImageResult(**{**self.VALID_RESULT, "contentType": "image/gif"})

    def test_rejects_empty_image_base64(self):
        with pytest.raises(ValidationError):
            ReferenceImageResult(**{**self.VALID_RESULT, "imageBase64": ""})

    def test_rejects_nonpositive_width(self):
        with pytest.raises(ValidationError):
            ReferenceImageResult(**{**self.VALID_RESULT, "width": 0})

    def test_rejects_unknown_field(self):
        with pytest.raises(ValidationError):
            ReferenceImageResult(**self.VALID_RESULT, unexpectedField="nope")
