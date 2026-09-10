"use client";

import { useWorkResourceRequirements } from "@/features/ai-assistant/queries";
import type { RequirementDTO } from "@/features/ai-assistant/api";

const resourceTypeLabels: Record<string, string> = {
  material: "Materials",
  trade: "Trades",
  equipment: "Equipment",
};

const resourceTypeOrder = ["material", "trade", "equipment"];

function groupByResourceType(requirements: RequirementDTO[]): Record<string, RequirementDTO[]> {
  const grouped: Record<string, RequirementDTO[]> = {};
  for (const requirement of requirements) {
    grouped[requirement.resourceType] = [...(grouped[requirement.resourceType] ?? []), requirement];
  }
  return grouped;
}

// Fetches all of the Project's confirmed WorkResourceRequirements once and
// filters/groups in memory, so rendering resources for N Work Items on one
// page never issues N HTTP requests (design doc plan Task 15 Step 9).
export function WorkItemResources({ projectId, workItemId }: { projectId: string; workItemId: string }) {
  const { data, isLoading } = useWorkResourceRequirements(projectId);
  const requirements = (data?.requirements ?? []).filter((r) => r.workItemId === workItemId);

  if (isLoading) return null;

  if (requirements.length === 0) {
    return <p className="text-sm text-muted-foreground">No resources yet</p>;
  }

  const grouped = groupByResourceType(requirements);

  return (
    <div className="flex flex-col gap-3">
      {resourceTypeOrder
        .filter((type) => grouped[type]?.length)
        .map((type) => (
          <div key={type} className="flex flex-col gap-1">
            <h4 className="text-xs font-semibold text-muted-foreground">{resourceTypeLabels[type] ?? type}</h4>
            <ul className="flex flex-col gap-1">
              {grouped[type].map((requirement) => (
                <li key={requirement.id} className="text-sm">
                  {requirement.name}
                </li>
              ))}
            </ul>
          </div>
        ))}
    </div>
  );
}
