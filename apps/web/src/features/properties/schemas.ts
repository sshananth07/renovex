import { z } from "zod";

// address is the only required field on both create and update
// (CreatePropertyInputBody / UpdatePropertyInputBody both require it).
export const propertyFormSchema = z.object({
  address: z.string().trim().min(1, "Address is required"),
  propertyType: z.string().trim().optional(),
  notes: z.string().trim().optional(),
});

export type PropertyFormValues = z.infer<typeof propertyFormSchema>;
