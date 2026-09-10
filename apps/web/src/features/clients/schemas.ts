import { z } from "zod";

// Mirrors CreateClientInputBody's required/optional shape (only name is
// required) for immediate field-level feedback. This is NOT the security
// boundary — the backend re-validates everything; frontend Zod exists only
// to catch obvious mistakes before a round-trip (architecture doc §16).
export const clientFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  email: z.string().trim().email("Enter a valid email").or(z.literal("")).optional(),
  phone: z.string().trim().optional(),
  address: z.string().trim().optional(),
  billingAddress: z.string().trim().optional(),
  notes: z.string().trim().optional(),
});

export type ClientFormValues = z.infer<typeof clientFormSchema>;
