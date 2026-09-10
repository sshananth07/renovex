import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createProject, renameProject, updateProjectStatus } from "./api";
import type { ProjectStatus } from "./statusPresentation";

export function useCreateProject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createProject,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      queryClient.invalidateQueries({ queryKey: ["clients"] });
      queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });
}

export function useRenameProject(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => renameProject(projectId, name),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      queryClient.setQueryData(["projects", projectId], data);
    },
  });
}

export function useUpdateProjectStatus(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (status: ProjectStatus) => updateProjectStatus(projectId, status),
    onSuccess: (data) => {
      queryClient.setQueryData(["projects", projectId], data);
      queryClient.invalidateQueries({ queryKey: ["projects"] });
      queryClient.invalidateQueries({ queryKey: ["clients"] });
      queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });
}
