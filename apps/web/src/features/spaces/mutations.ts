import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createSpace, updateSpace, type CreateSpaceInput, type UpdateSpaceInput } from "./api";

export function useCreateSpace(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: Omit<CreateSpaceInput, "projectId">) =>
      createSpace({ ...input, projectId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects", projectId, "spaces"] });
    },
  });
}

export function useUpdateSpace(projectId: string, spaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateSpaceInput) => updateSpace(spaceId, input),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["projects", projectId, "spaces"] });
      queryClient.setQueryData(["spaces", spaceId], data);
    },
  });
}
