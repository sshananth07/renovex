"""Deterministic, offline SpatialReasoningProvider used for automated tests
and local development (SPATIAL_AI_PROVIDER=mock). Output is a pure function
of the request — no randomness, no wall-clock dependence — mirroring
providers/mock.py's own MockProvider convention for the Copilot routes.

The mock performs a small amount of keyword interpretation on the
instruction purely so RP4E1's own test suite (Go and Python) can exercise
distinct material/geometry/spatial/mixed/unsupported code paths offline,
without ever contacting GLM. It is not a language model and makes no
attempt at general natural-language understanding.
"""

from app.schemas.spatial_reasoning import (
    GeometryChange,
    MaterialChange,
    ProposedBlocker,
    ProposedSceneEditDelta,
    SelectedTargetRef,
    SpatialChange,
    SpatialReasoningRequest,
    SpatialReasoningResult,
)

_PROMPT_VERSION = "spatial-reasoning-mock-v1"

_STRUCTURAL_KEYWORDS = ("knock down", "remove the wall", "extend the room", "move the wall")


class SpatialMockProvider:
    def reason_element(self, request: SpatialReasoningRequest) -> SpatialReasoningResult:
        instruction = request.instruction.lower()
        target = SelectedTargetRef(kind=request.selectedElement.kind, id=request.selectedElement.id)

        if any(keyword in instruction for keyword in _STRUCTURAL_KEYWORDS):
            delta = ProposedSceneEditDelta(
                schemaVersion=1,
                target=target,
                intent="spatial_domain",
                summary=["The requested change modifies room structure, which is outside this session's scope."],
                geometry=GeometryChange(mode="preserve"),
                material=MaterialChange(mode="preserve"),
                spatial=SpatialChange(mode="preserve"),
                blockers=[ProposedBlocker(
                    code="unsupported_operation",
                    message="Moving or removing walls is not supported for an object/fixture design session.",
                )],
                assumptions=[],
                reviewNotes=[],
                confidence=0.4,
            )
            return self._result(delta)

        wants_material = any(k in instruction for k in ("beige", "velvet", "green", "color", "colour", "upholster"))
        wants_geometry = any(k in instruction for k in ("curved", "shape", "rounded", "reshape"))
        wants_spatial = any(k in instruction for k in ("move", "away from", "toward", "closer"))

        geometry = (
            GeometryChange(mode="replace", spec={
                "category": request.selectedElement.category,
                "shapeDescription": "Curved design with rounded arms",
                "preserveCanonicalDimensions": True,
            })
            if wants_geometry else GeometryChange(mode="preserve")
        )
        material = (
            MaterialChange(mode="replace", spec={
                "baseColor": "#C8A464" if "beige" in instruction else "#2F4F3A",
                "materialFamily": "fabric",
                "roughness": "matte",
                "metallic": False,
            })
            if wants_material else MaterialChange(mode="preserve")
        )
        spatial = (
            SpatialChange(mode="replace", spec={
                "kind": "move_relative_to_nearest_wall",
                "relationship": "away_from",
                "distanceMeters": 0.2,
            })
            if wants_spatial else SpatialChange(mode="preserve")
        )

        changed_count = sum([wants_geometry, wants_material, wants_spatial])
        if changed_count > 1:
            intent = "mixed"
        elif wants_geometry:
            intent = "visual_geometry"
        elif wants_material:
            intent = "material_appearance"
        elif wants_spatial:
            intent = "spatial_domain"
        else:
            intent = "material_appearance"

        summary = []
        if wants_geometry:
            summary.append("Change the shape to a curved design.")
        if wants_material:
            summary.append("Change the material/color.")
        if wants_spatial:
            summary.append("Move it relative to the nearest wall.")
        if not summary:
            summary.append("No recognized change requested.")

        delta = ProposedSceneEditDelta(
            schemaVersion=1,
            target=target,
            intent=intent,
            summary=summary,
            geometry=geometry,
            material=material,
            spatial=spatial,
            blockers=[],
            assumptions=[],
            reviewNotes=[],
            confidence=0.75,
        )
        return self._result(delta)

    def _result(self, delta: ProposedSceneEditDelta) -> SpatialReasoningResult:
        return SpatialReasoningResult(
            delta=delta,
            provider="mock",
            model="spatial-mock-v1",
            promptVersion=_PROMPT_VERSION,
            schemaVersion=1,
        )
