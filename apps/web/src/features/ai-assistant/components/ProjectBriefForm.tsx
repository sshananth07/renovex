"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { useUpdateProjectScopeBrief } from "../mutations";

const briefSchema = z.object({
  scopeBrief: z.string().max(5000, "Project brief must be at most 5000 characters."),
});

type BriefFormValues = z.infer<typeof briefSchema>;

interface BackendProblem {
  title?: string;
  detail?: string;
  errors?: Array<{ message?: string }>;
}

function briefErrorMessage(error: unknown): string {
  const problem = error as BackendProblem | null;
  return (
    problem?.detail ??
    problem?.errors?.find((item) => item.message)?.message ??
    problem?.title ??
    "Could not save the project brief."
  );
}

interface ProjectBriefFormProps {
  projectId: string;
  initialBrief: string;
}

export function ProjectBriefForm({ projectId, initialBrief }: ProjectBriefFormProps) {
  const mutation = useUpdateProjectScopeBrief(projectId);
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<BriefFormValues>({
    resolver: zodResolver(briefSchema),
    defaultValues: { scopeBrief: initialBrief },
  });

  const onSubmit = handleSubmit((values) => {
    mutation.mutate(values.scopeBrief);
  });

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-3">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="scopeBrief">Project brief</Label>
        <textarea
          id="scopeBrief"
          rows={5}
          className="w-full resize-y rounded-lg border border-input bg-background px-3 py-2 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/20"
          placeholder="Describe the renovation in your own words — e.g. Full renovation of a 3-bedroom condominium. Redo the kitchen and two bathrooms, replace flooring, and repaint the whole unit."
          {...register("scopeBrief")}
        />
        {errors.scopeBrief && (
          <p role="alert" className="text-sm text-destructive">
            {errors.scopeBrief.message}
          </p>
        )}
      </div>
      {mutation.isError && (
        <p role="alert" className="text-sm text-destructive">
          {briefErrorMessage(mutation.error)}
        </p>
      )}
      <div>
        <Button type="submit" disabled={mutation.isPending}>
          {mutation.isPending ? "Saving…" : "Save"}
        </Button>
      </div>
    </form>
  );
}
