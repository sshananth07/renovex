import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { components } from "@/lib/api/generated/schema";
import * as api from "./api";

export function useOperationMutation<T>(mutationFn: (input: T) => Promise<unknown>, keys: readonly unknown[]) {
  const queryClient = useQueryClient();
  const refresh = () => queryClient.invalidateQueries({ queryKey: [...keys] });
  return useMutation({ mutationFn, onSuccess: refresh, onError: refresh });
}

export type CreateMaterialInput = components["schemas"]["CreateMaterialInputBody"];
export type CreateWorkerInput = components["schemas"]["CreateWorkerInputBody"];
export type CreateLabourInput = components["schemas"]["CreateLabourEntryInputBody"];
export type CreateCostInput = components["schemas"]["CreateCostItemInputBody"];
export { api };
