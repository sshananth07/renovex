import { useQuery } from "@tanstack/react-query";
import { getQuotationShareStatus, listCostItems, listEstimates, listLabourEntries, listMaterials, listQuotations, listWorkers } from "./api";

export const useMaterials = () => useQuery({ queryKey: ["materials"], queryFn: listMaterials });
export const useWorkers = () => useQuery({ queryKey: ["workers"], queryFn: listWorkers });
export const useLabourEntries = (projectId: string) => useQuery({ queryKey: ["projects", projectId, "labour"], queryFn: () => listLabourEntries(projectId), enabled: Boolean(projectId) });
export const useCostItems = (projectId: string) => useQuery({ queryKey: ["projects", projectId, "costs"], queryFn: () => listCostItems(projectId), enabled: Boolean(projectId) });
export const useEstimates = (projectId: string) => useQuery({ queryKey: ["projects", projectId, "estimates"], queryFn: () => listEstimates(projectId), enabled: Boolean(projectId) });
export const useQuotations = (projectId: string) => useQuery({ queryKey: ["projects", projectId, "quotations"], queryFn: () => listQuotations(projectId), enabled: Boolean(projectId) });
export const useQuotationShareStatus = (quotationId: string) => useQuery({ queryKey: ["quotations", quotationId, "share-status"], queryFn: () => getQuotationShareStatus(quotationId), enabled: Boolean(quotationId), retry: false });
