import { apiClient } from "@/lib/api/client";
import { unwrapOrThrow } from "@/lib/api/errors";
import type { components } from "@/lib/api/generated/schema";

export type Material = components["schemas"]["MaterialDTO"];
export type Worker = components["schemas"]["WorkerDTO"];
export type LabourEntry = components["schemas"]["LabourEntryDTO"];
export type CostItem = components["schemas"]["CostItemDTO"];
export type Estimate = components["schemas"]["EstimateDTO"];
export type Quotation = components["schemas"]["QuotationDTO"];

export async function listMaterials() {
  const { data, error } = await apiClient.GET("/materials");
  if (error) throw error;
  return data.materials ?? [];
}
export async function createMaterial(body: components["schemas"]["CreateMaterialInputBody"]) {
  const { data, error } = await apiClient.POST("/materials", { body });
  if (error) throw error;
  return data;
}
export async function updateMaterial(id: string, body: components["schemas"]["UpdateMaterialInputBody"]) {
  const { data, error } = await apiClient.PATCH("/materials/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data;
}
export async function listWorkers() {
  const { data, error } = await apiClient.GET("/workers");
  if (error) throw error;
  return data.workers ?? [];
}
export async function createWorker(body: components["schemas"]["CreateWorkerInputBody"]) {
  const { data, error } = await apiClient.POST("/workers", { body });
  if (error) throw error;
  return data;
}
export async function updateWorker(id: string, body: components["schemas"]["UpdateWorkerInputBody"]) {
  const { data, error } = await apiClient.PATCH("/workers/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data;
}
export async function listLabourEntries(projectId: string) {
  const { data, error } = await apiClient.GET("/labour-entries", { params: { query: { projectId } } });
  if (error) throw error;
  return data.labourEntries ?? [];
}
export async function createLabourEntry(body: components["schemas"]["CreateLabourEntryInputBody"]) {
  return unwrapOrThrow(await apiClient.POST("/labour-entries", { body }));
}
export async function listCostItems(projectId: string) {
  const { data, error } = await apiClient.GET("/cost-items", { params: { query: { projectId } } });
  if (error) throw error;
  return data.costItems ?? [];
}
export async function createCostItem(body: components["schemas"]["CreateCostItemInputBody"]) {
  return unwrapOrThrow(await apiClient.POST("/cost-items", { body }));
}
export async function updateCostLifecycle(id: string, body: components["schemas"]["UpdateCostItemLifecycleInputBody"]) {
  const { data, error } = await apiClient.PATCH("/cost-items/{id}/lifecycle", { params: { path: { id } }, body });
  if (error) throw error;
  return data;
}
export async function recordCostItemActual(id: string, body: components["schemas"]["RecordCostItemActualInputBody"]) {
  return unwrapOrThrow(await apiClient.POST("/cost-items/{id}/record-actual", { params: { path: { id } }, body }));
}
export async function correctCostItemActual(id: string, body: components["schemas"]["CorrectCostItemActualInputBody"]) {
  return unwrapOrThrow(await apiClient.POST("/cost-items/{id}/correct-actual", { params: { path: { id } }, body }));
}
export async function listEstimates(projectId: string) {
  const { data, error } = await apiClient.GET("/estimates", { params: { query: { projectId } } });
  if (error) throw error;
  return data.estimates ?? [];
}
export async function createEstimate(body: components["schemas"]["CreateEstimateInputBody"]) {
  return unwrapOrThrow(await apiClient.POST("/estimates", { body }));
}
export async function updateEstimatePricing(id: string, body: components["schemas"]["RecalculatePricingInputBody"]) {
  const { data, error } = await apiClient.PATCH("/estimates/{id}/pricing", { params: { path: { id } }, body });
  if (error) throw error;
  return data;
}
export async function refreshEstimate(id: string, expectedRevision: number) {
  const { data, error } = await apiClient.POST("/estimates/{id}/refresh", { params: { path: { id } }, body: { expectedRevision } });
  if (error) throw error;
  return data;
}
export async function finalizeEstimate(id: string, expectedRevision: number) {
  const { data, error } = await apiClient.POST("/estimates/{id}/finalize", { params: { path: { id } }, body: { expectedRevision } });
  if (error) throw error;
  return data;
}
export async function createEstimateVersion(id: string, body: components["schemas"]["CreateNewVersionInputBody"]) {
  const { data, error } = await apiClient.POST("/estimates/{id}/versions", { params: { path: { id } }, body });
  if (error) throw error;
  return data;
}
export async function listQuotations(projectId: string) {
  const { data, error } = await apiClient.GET("/quotations", { params: { query: { projectId } } });
  if (error) throw error;
  return data.quotations ?? [];
}
export async function createQuotation(body: components["schemas"]["CreateQuotationInputBody"]) {
  const { data, error } = await apiClient.POST("/quotations", { body });
  if (error) throw error;
  return data;
}
export async function updateQuotationTerms(id: string, body: components["schemas"]["UpdateTermsInputBody"]) {
  const { data, error } = await apiClient.PATCH("/quotations/{id}/terms", { params: { path: { id } }, body });
  if (error) throw error;
  return data;
}
export async function updateQuotationTax(id: string, body: components["schemas"]["UpdateTaxInputBody"]) {
  const { data, error } = await apiClient.PATCH("/quotations/{id}/tax", { params: { path: { id } }, body });
  if (error) throw error;
  return data;
}
export async function finalizeQuotation(id: string, expectedRevision: number) {
  const { data, error } = await apiClient.POST("/quotations/{id}/finalize", { params: { path: { id } }, body: { expectedRevision } });
  if (error) throw error;
  return data;
}
export async function createQuotationVersion(id: string, estimateId: string) {
  const { data, error } = await apiClient.POST("/quotations/{id}/versions", { params: { path: { id } }, body: { estimateId } });
  if (error) throw error;
  return data;
}
export async function shareQuotation(id: string) {
  const { data, error } = await apiClient.POST("/quotations/{id}/share", { params: { path: { id } } });
  if (error) throw error;
  return data;
}
export async function getQuotationShareStatus(id: string) {
  const { data, error } = await apiClient.GET("/quotations/{id}/share", { params: { path: { id } } });
  if (error) throw error;
  return data;
}
