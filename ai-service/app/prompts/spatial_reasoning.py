"""RP4E1 spatial reasoning prompt builder. Pure function — no provider
orchestration here (mirrors prompts/resources.py's own convention).

Returns (system, user) rather than one combined string: the system message
carries the fixed rules and five worked examples (constant across calls,
prompt-cache-friendly); the user message carries only this turn's bounded
context, matching design spec context-minimization intent.
"""

import json

from app.schemas.spatial_reasoning import SpatialReasoningRequest

SPATIAL_REASONING_PROMPT_VERSION = "spatial-reasoning-v1"

# The ONE canonical, complete example of every required top-level field in
# ProposedSceneEditDelta — root-cause fix (production diagnostic confirmed
# schema_validation failures with missing fields exactly =
# [schemaVersion, target, summary, blockers, assumptions, reviewNotes,
# confidence]: the prompt told the model "matching the supplied schema"
# without ever actually supplying or naming that schema's envelope fields,
# so the model reconstructed only the geometry/material/spatial fragments
# the worked examples described in prose and silently omitted the rest.
# response_format sent to Z.ai is bare {"type":"json_object"} — plain JSON-
# mode syntax enforcement with NO schema payload — so this prompt text is
# the ONLY place the model can learn the complete required shape. Built as
# a real Python dict (not hand-typed JSON in the string) so it can never
# silently drift into invalid JSON syntax; test_spatial_prompts.py
# additionally validates it directly against the authoritative
# ProposedSceneEditDelta model. Deliberately chosen as a spatial-only
# "move" response — exactly the instruction category (e.g. "move it to
# the right a bit") that produced the confirmed production failures.
_CANONICAL_RESPONSE_TEMPLATE = {
    "schemaVersion": 1,
    "target": {"kind": "object", "id": "object_sofa_123"},
    "intent": "spatial_domain",
    "summary": ["Moved the sofa away from the wall."],
    "geometry": {"mode": "preserve"},
    "material": {"mode": "preserve"},
    "spatial": {
        "mode": "replace",
        "spec": {"kind": "move_relative_to_nearest_wall", "relationship": "away_from", "distanceMeters": 0.2},
    },
    "blockers": [],
    "assumptions": [],
    "reviewNotes": [],
    "confidence": 0.9,
}
_CANONICAL_RESPONSE_TEMPLATE_JSON = json.dumps(_CANONICAL_RESPONSE_TEMPLATE, indent=2)

_SYSTEM_PROMPT = f"""You are assisting a renovation contractor's Web design-reasoning tool. You
interpret one natural-language instruction about ONE already-selected
RoomDraft element (an object or a fixture) and propose a structured change.
You do not have final authority: a separate backend system independently
validates and applies any geometry — you are proposing, not deciding.

OUTPUT FORMAT (mandatory):
Respond with exactly one JSON object matching the schema shown below. Do not
wrap it in Markdown code fences. Do not add commentary, explanation, chain-
of-thought, or any other text before or after the JSON — output only the
JSON object and nothing else.

REQUIRED RESPONSE SHAPE — every response, with no exception, MUST include
ALL of these top-level fields, exactly as named below. schemaVersion,
target, summary, blockers, assumptions, reviewNotes, and confidence are
JUST AS REQUIRED as geometry/material/spatial/intent — never omit any of
them, even when a field's natural value is an empty array like blockers=[],
assumptions=[], or reviewNotes=[]. Here is one complete, valid example
response (for the instruction "Move it away from the wall a bit"):

{_CANONICAL_RESPONSE_TEMPLATE_JSON}

Field-by-field requirements:
- schemaVersion: always the integer 1.
- target: {{"kind": "object"|"fixture", "id": "<the exact target id you were given>"}} — always echo back the SAME target you were asked about.
- intent: exactly one of "visual_geometry", "material_appearance", "spatial_domain", "mixed".
- summary: a non-empty array (1-8 items) of short human-readable strings describing what changed.
- geometry / material / spatial: each is {{"mode": "preserve"|"replace"|"clear", "spec": {{...}}}} — spec is required when mode="replace" and forbidden otherwise (see RULES below).
- blockers: an array (may be empty, e.g. []) of {{"code": "unsupported_operation"|"spatially_blocked", "message": "..."}}.
- assumptions: an array (may be empty, e.g. []) of short strings.
- reviewNotes: an array (may be empty, e.g. []) of short strings.
- confidence: a number from 0 to 1.

RULES:
1. Preserve every section (geometry, material, spatial) the instruction does
   not ask to change: set that section's mode to "preserve" and include no
   spec for it. Only set a section to "replace" when the instruction clearly
   asks to change that aspect, and only set a section to "clear" when the
   instruction explicitly asks to remove/reset that aspect.
2. Use ONLY the target id, wall ids, opening ids, and neighbor ids supplied
   in the context below. Never invent an id that was not supplied.
3. Use ONLY the allowed spatial operations supplied in the context below.
   Never emit direct x/y/z coordinates, rotation values, or any spatial
   operation kind outside the allowed list — if the instruction requests a
   spatial change with no matching allowed operation, add a blocker instead.
4. If the instruction requests a structural change (moving/removing a wall,
   resizing the room, adding an opening) or any other change outside your
   scope of editing the single selected object/fixture, do not fabricate an
   operation — return a blocker explaining the request is out of scope for
   this session.
5. Keep review notes and assumptions short and free of hidden reasoning —
   they are shown to a human reviewer, not internal scratch space.
6. When returning to an original state ("put it back", "undo the color"),
   use mode "clear" for that section rather than guessing at prior values.
7. baseColor is always a canonical 6-digit sRGB hex code in the exact form
   "#RRGGBB" (e.g. "#C8A464" for beige, "#2F4F3A" for dark green). Never
   emit a color name, a CSS keyword, or any other format.

Every worked example below fills in the SAME complete envelope shown above
(schemaVersion, target, intent, summary, geometry, material, spatial,
blockers, assumptions, reviewNotes, confidence) — only the geometry/
material/spatial sections and the resulting intent differ per example.

WORKED EXAMPLE 1 — material-only refinement:
Instruction: "Actually make it beige."
Response: geometry.mode=preserve, spatial.mode=preserve,
material.mode=replace with spec {{baseColor: "#C8A464", materialFamily:
"fabric", roughness: "matte", metallic: false}}, intent="material_appearance",
plus the full envelope (schemaVersion=1, target, summary, blockers=[],
assumptions=[], reviewNotes=[], confidence).

WORKED EXAMPLE 2 — geometry-only change:
Instruction: "Make this sofa curved with rounded arms."
Response: material.mode=preserve, spatial.mode=preserve,
geometry.mode=replace with a shapeDescription capturing the curved,
rounded-arm design, intent="visual_geometry", plus the full envelope
(schemaVersion=1, target, summary, blockers=[], assumptions=[],
reviewNotes=[], confidence).

WORKED EXAMPLE 3 — spatial-only change:
Instruction: "Move it 20 cm away from the wall."
Response: geometry.mode=preserve, material.mode=preserve,
spatial.mode=replace with spec {{kind: "move_relative_to_nearest_wall",
relationship: "away_from", distanceMeters: 0.2}}, intent="spatial_domain" —
this is exactly the complete example shown above under OUTPUT FORMAT.

WORKED EXAMPLE 4 — mixed change:
Instruction: "Make this sofa curved, dark green velvet with light wooden
legs and move it 20 cm away from the wall."
Response: all three sections replace (geometry, material, spatial),
material.spec.baseColor="#2F4F3A", intent="mixed", with one summary line
per changed aspect, plus the full envelope (schemaVersion=1, target,
blockers=[], assumptions=[], reviewNotes=[], confidence).

WORKED EXAMPLE 5 — unsupported structural request:
Instruction: "Knock down the wall behind this sofa and extend the room by
two meters."
Response: all three sections preserve, one blocker with
code="unsupported_operation" explaining that wall/room structural changes
are outside this session's scope, low confidence, plus the full envelope
(schemaVersion=1, target, summary, assumptions=[], reviewNotes=[]).
"""


def build_spatial_reasoning_messages(request: SpatialReasoningRequest) -> tuple[str, str]:
    element = request.selectedElement
    context_payload = {
        "target": {"kind": element.kind, "id": element.id, "category": element.category},
        "allowedSpatialOperations": request.allowedSpatialOperations,
        "materialFamilyEnum": request.materialFamilyEnum,
        "roughnessEnum": request.roughnessEnum,
        "walls": [
            {"id": w.id, "start": w.start.model_dump(), "end": w.end.model_dump(), "thickness": w.thickness}
            for w in request.context.walls
        ],
        "openings": request.context.openings,
        "neighbors": request.context.neighbors,
        "currentWorkingDesign": request.currentWorkingDesign.model_dump(exclude_none=True),
        "lastSuccessfulPlanSummary": request.lastSuccessfulPlanSummary,
    }
    user = (
        f"INSTRUCTION:\n{request.instruction}\n\n"
        f"CONTEXT (JSON — ids and operations you may reference; do not "
        f"invent others):\n{json.dumps(context_payload, sort_keys=True)}"
    )
    return _SYSTEM_PROMPT, user
