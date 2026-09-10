export const procurementKeys = {
  requirements: (projectId: string) => ["projects", projectId, "requirements"] as const,
  rfqs: (projectId: string) => ["projects", projectId, "rfqs"] as const,
  rfq: (rfqId: string) => ["rfqs", rfqId] as const,
  rfqVersions: (rfqChainId: string) => ["rfq-chains", rfqChainId, "versions"] as const,
  sourceDiscrepancy: (requirementId: string) => ["material-requirements", requirementId, "source-discrepancy"] as const,
};
