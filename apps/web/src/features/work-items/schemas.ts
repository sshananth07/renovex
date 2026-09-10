import { z } from "zod";

// Mirrors CreateWorkItemInputBody: description/quantityValue/quantityUnit
// required, spaceId/workType optional. quantityValue is validated as a
// positive decimal string client-side for fast feedback, but the backend
// (shopspring/decimal, IsPositive()) is the authority — see work-items/api.ts.
export const workItemFormSchema = z.object({
  description: z.string().trim().min(1, "Description is required"),
  quantityValue: z
    .string()
    .trim()
    .min(1, "Quantity is required")
    .refine((value) => {
      const parsed = Number(value);
      return Number.isFinite(parsed) && parsed > 0;
    }, "Enter a positive number"),
  quantityUnit: z.string().trim().min(1, "Unit is required"),
  spaceId: z.string().optional(),
  workType: z.string().trim().optional(),
});

export type WorkItemFormValues = z.infer<typeof workItemFormSchema>;
