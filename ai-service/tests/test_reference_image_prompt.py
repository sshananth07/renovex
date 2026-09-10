"""Golden tests for the RP4E2 reference-image prompt builder: deterministic
ordering, isolated full object, three-quarter front view, canonical
proportions, no room/people/text, no named-part persistence claim."""

from app.prompts.reference_image import REFERENCE_IMAGE_PROMPT_VERSION, build_reference_image_prompt
from app.schemas.reference_image import ReferenceImageRequest

_BASE_REQUEST = {
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


def test_prompt_version_is_stable():
    assert REFERENCE_IMAGE_PROMPT_VERSION == "reference-v1"


def test_prompt_is_deterministic_for_identical_input():
    a = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    b = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    assert a == b


def test_prompt_includes_category_and_shape():
    prompt = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    assert "sofa" in prompt
    assert "Curved three-seat sofa with rounded back" in prompt


def test_prompt_includes_material_appearance():
    prompt = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    assert "fabric" in prompt
    assert "#315c45" in prompt
    assert "matte" in prompt
    assert "non-metallic" in prompt


def test_prompt_omits_material_section_when_absent():
    data = {**_BASE_REQUEST}
    del data["materialAppearance"]
    prompt = build_reference_image_prompt(ReferenceImageRequest(**data))
    assert "Material:" not in prompt


def test_prompt_includes_canonical_proportions():
    prompt = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    assert "2.10m wide" in prompt
    assert "0.85m tall" in prompt
    assert "0.90m deep" in prompt


def test_prompt_omits_proportions_when_dimensions_absent():
    data = {**_BASE_REQUEST, "target": {**_BASE_REQUEST["target"]}}
    del data["target"]["dimensionsMeters"]
    prompt = build_reference_image_prompt(ReferenceImageRequest(**data))
    assert "Canonical proportions" not in prompt


def test_prompt_specifies_isolated_full_object_three_quarter_view():
    prompt = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    assert "three-quarter front view" in prompt
    assert "isolated" in prompt
    assert "entire object fully visible" in prompt
    assert "plain warm white background" in prompt


def test_prompt_excludes_room_people_text():
    prompt = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    assert "no people" in prompt
    assert "no surrounding room" in prompt
    assert "no text or watermarks" in prompt


def test_prompt_never_claims_named_part_persistence():
    # The prompt describes the WHOLE object's shape/material — it must never
    # reference individual named sub-parts (legs, arms, cushions, etc.) as
    # if they were independently persisted or addressable, matching the
    # plan's "no named-part persistence claim" invariant.
    prompt = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    for forbidden in ("named part", "sub-part", "component id", "part id"):
        assert forbidden not in prompt.lower()


def test_prompt_contains_no_room_json_urls_or_tenant_data():
    prompt = build_reference_image_prompt(ReferenceImageRequest(**_BASE_REQUEST))
    assert "http://" not in prompt
    assert "https://" not in prompt
    assert "ds_1" not in prompt  # designSessionId never leaks into prompt text
    assert "company" not in prompt.lower()
