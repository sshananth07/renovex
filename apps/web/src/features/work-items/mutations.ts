import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  createWorkItem,
  updateWorkItem,
  updateWorkItemStatus,
  type CreateWorkItemInput,
  type UpdateWorkItemInput,
  type WorkItemStatus,
} from "./api";

export function useCreateWorkItem(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: Omit<CreateWorkItemInput, "projectId">) =>
      createWorkItem({ ...input, projectId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects", projectId, "work-items"] });
    },
  });
}

export function useUpdateWorkItem(projectId: string, workItemId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateWorkItemInput) => updateWorkItem(workItemId, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects", projectId, "work-items"] });
    },
  });
}

export function useUpdateWorkItemStatus(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: WorkItemStatus }) =>
      updateWorkItemStatus(id, status),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects", projectId, "work-items"] });
    },
  });
}
