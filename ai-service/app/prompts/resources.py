"""Resource suggestion prompt builder. Pure function — no provider
orchestration here."""

from app.schemas.resources import ResourceSuggestionRequest

RESOURCES_PROMPT_VERSION = "resources-v1"


def build_resources_prompt(request: ResourceSuggestionRequest) -> str:
    work_item_lines = "\n".join(
        f"- id={w.id}, description={w.description}" for w in request.workItems
    ) or "(none)"
    candidate_lines = "\n".join(
        f"- id={c.id}, name={c.name}" for c in request.materialCandidates
    ) or "(none)"
    existing_lines = "\n".join(
        f"- {r.name} ({r.resourceType}) for workItem={r.workItemId}"
        for r in request.existingRequirements
    ) or "(none)"

    return f"""You are assisting a renovation contractor by suggesting planning-level
resources (materials, trades, equipment) each confirmed Work Item is
expected to require.

CONFIRMED WORK ITEMS (use ONLY these ids as workItemId):
{work_item_lines}

MATERIAL CATALOG CANDIDATES (a candidateMaterialId, if you include one, MUST
be one of these ids — it is advisory only, the contractor makes the final
choice):
{candidate_lines}

EXISTING RESOURCE REQUIREMENTS (avoid suggesting obvious duplicates of
these):
{existing_lines}

For each suggested resource, return only:
- resourceType ("material", "trade", or "equipment")
- workItemId (must be one of the ids listed above)
- name
- candidateMaterialId (only for resourceType="material"; must be one of the
  candidate ids above, or omitted/null if there is no good match)
- rationale (one short contractor-facing sentence, never hidden reasoning)
- confidence (0.0-1.0)

Do NOT include rate, price, cost, or quantity for any resource. This
suggestion means only "this Work Item is expected to require this
resource" — never a determined quantity, a selected worker, a selected
supplier, or a commercial commitment.
"""
