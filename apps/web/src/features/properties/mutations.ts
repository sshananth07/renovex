import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createProperty, updateProperty } from "./api";

export function useCreateProperty(projectId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createProperty,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects", projectId, "property"] });
    },
  });
}

export function useUpdateProperty(projectId: string, propertyId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: Parameters<typeof updateProperty>[1]) => updateProperty(propertyId, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects", projectId, "property"] });
    },
  });
}
