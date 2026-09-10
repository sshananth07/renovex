import type { components } from "@/lib/api/generated/schema";

type MaterialRequirement = components["schemas"]["MaterialRequirementDTO"];

export type DuplicateCandidate = {
  materialId: string;
  workItemId?: string | null;
  quantityUnit: string;
};

// Advisory only — no backend uniqueness exists or should be added; legitimate
// scenarios exist for the same material/work-item/unit to appear twice
// (separate procurement batches, different required-by dates). This is
// purely a client-side "did you mean to do this?" nudge.
export function buildDuplicateCandidateKey(candidate: DuplicateCandidate): string {
  return [candidate.materialId, candidate.workItemId ?? "", candidate.quantityUnit.trim()].join("|");
}

const TERMINAL_STATUSES = new Set(["archived", "split"]);

// Compares against every non-terminal requirement already loaded for the
// project — not manual-only, since a generated (cost_item) requirement with
// the same material/work-item/unit is still worth a soft warning against a
// new manual candidate. No unit conversion: different units are always
// distinct procurement demand per the backend's own design.
export function findSimilarRequirement(
  existing: MaterialRequirement[],
  candidate: DuplicateCandidate
): MaterialRequirement | undefined {
  const candidateKey = buildDuplicateCandidateKey(candidate);
  return existing.find((requirement) => {
    if (TERMINAL_STATUSES.has(requirement.status)) return false;
    const existingKey = buildDuplicateCandidateKey({
      materialId: requirement.materialId,
      workItemId: requirement.workItemId ?? null,
      quantityUnit: requirement.requiredQuantity.unit,
    });
    return existingKey === candidateKey;
  });
}
