import { z } from "zod";

// name is the only required field (CreateSpaceInputBody/UpdateSpaceInputBody
// both require it); type/description are free text with no backend enum.
export const spaceFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  type: z.string().trim().optional(),
  description: z.string().trim().optional(),
});

export type SpaceFormValues = z.infer<typeof spaceFormSchema>;
