export type AISetupStage = "brief" | "spaces" | "workItems" | "resources";

interface AISetupStageInput {
  scopeBrief: string;
  confirmedSpaces: number;
  confirmedWorkItems: number;
}

// Pure derivation from real authoritative Project/Space/WorkItem state
// (design doc §24.2) — never local fake progress state. The contractor does
// not need to accept every AI suggestion; manually-created Spaces/WorkItems
// count identically to AI-accepted ones because both flow through the same
// real domain services.
export function deriveAISetupStage(input: AISetupStageInput): AISetupStage {
  if (input.scopeBrief.trim() === "") return "brief";
  if (input.confirmedSpaces === 0) return "spaces";
  if (input.confirmedWorkItems === 0) return "workItems";
  return "resources";
}
