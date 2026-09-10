export type SetupStepState = "complete" | "incomplete";

export interface SetupProgress {
  client: SetupStepState;
  project: SetupStepState;
  property: SetupStepState;
  spaces: SetupStepState;
  workItems: SetupStepState;
}

interface SetupProgressInput {
  hasProperty: boolean;
  spacesTotal: number;
  workItemsTotal: number;
}

// Pure derivation from already-fetched resources (architecture doc §14): no
// new endpoint, no persisted frontend-only flag. Client/Project are always
// "complete" because a Project cannot exist without a clientId, and this
// function only runs once a Project itself has loaded.
export function deriveSetupProgress(input: SetupProgressInput): SetupProgress {
  return {
    client: "complete",
    project: "complete",
    property: input.hasProperty ? "complete" : "incomplete",
    spaces: input.spacesTotal > 0 ? "complete" : "incomplete",
    workItems: input.workItemsTotal > 0 ? "complete" : "incomplete",
  };
}
