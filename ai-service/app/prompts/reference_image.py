"""RP4E2 reference-image prompt builder. Pure function, versioned output —
deterministic ordering so the same request always yields byte-identical
prompt text (needed for the mock provider's fingerprint+seed determinism
and for auditability). Never includes arbitrary RoomDraft JSON, URLs,
tenant data, or credentials (RP4E2 plan's explicit boundary) — only the
bounded fields ReferenceImageRequest itself carries.
"""

from app.schemas.reference_image import ReferenceImageRequest

REFERENCE_IMAGE_PROMPT_VERSION = "reference-v1"

_NEGATIVE_CONSTRAINTS = (
    "no text or watermarks, no people, no surrounding room or background objects, "
    "no multiple items, no cropped edges"
)


def build_reference_image_prompt(request: ReferenceImageRequest) -> str:
    """Deterministic isolated-object prompt: category, shape, appearance,
    canonical proportions, fixed composition, negative constraints — in
    that fixed order, every time."""
    spec = request.assetGenerationSpec
    parts = [
        f"A single {spec.category}, isolated product photograph.",
        f"Shape: {spec.shapeDescription}.",
    ]

    if request.materialAppearance is not None:
        appearance = request.materialAppearance
        metallic_text = "metallic" if appearance.metallic else "non-metallic"
        parts.append(
            f"Material: {appearance.materialFamily}, color {appearance.baseColor}, "
            f"{appearance.roughness} finish, {metallic_text}."
        )

    dims = request.target.dimensionsMeters
    if dims is not None:
        parts.append(
            f"Canonical proportions: approximately {dims.width:.2f}m wide, "
            f"{dims.height:.2f}m tall, {dims.depth:.2f}m deep — preserve these "
            f"relative proportions exactly."
        )

    brief = request.renderBrief
    composition_bits = []
    if brief.view == "three_quarter_front":
        composition_bits.append("three-quarter front view")
    if brief.isolated:
        composition_bits.append("isolated on a plain background")
    if brief.fullObjectVisible:
        composition_bits.append("the entire object fully visible in frame")
    if brief.background == "plain_warm_white":
        composition_bits.append("plain warm white background")
    parts.append("Composition: " + ", ".join(composition_bits) + ".")

    parts.append(f"Do not include: {_NEGATIVE_CONSTRAINTS}.")

    return " ".join(parts)
