import { z } from "zod";

// Mirrors CreateProjectInputBody (clientId + name both required).
export const projectFormSchema = z.object({
  clientId: z.string().trim().min(1, "Select a client"),
  name: z.string().trim().min(1, "Name is required"),
});

export type ProjectFormValues = z.infer<typeof projectFormSchema>;

// PATCH /projects/{projectId} renames only — a single always-required field,
// not a genuine partial PATCH like Clients (see UpdateProjectNameInputBody).
export const projectRenameSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
});

export type ProjectRenameValues = z.infer<typeof projectRenameSchema>;
