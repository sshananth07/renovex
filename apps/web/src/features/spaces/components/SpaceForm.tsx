"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { spaceFormSchema, type SpaceFormValues } from "../schemas";

interface SpaceFormProps {
  defaultValues?: Partial<SpaceFormValues>;
  onSubmit: (values: SpaceFormValues) => void;
  submitting?: boolean;
}

// Create and edit share this form. SpaceDetail always resubmits name/type/
// description in full — UpdateSpaceInputBody requires `name` and
// unconditionally overwrites `type`/`description`, not a genuine partial PATCH.
export function SpaceForm({ defaultValues, onSubmit, submitting }: SpaceFormProps) {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<SpaceFormValues>({
    resolver: zodResolver(spaceFormSchema),
    defaultValues: { name: "", type: "", description: "", ...defaultValues },
  });

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="name">Name</Label>
        <Input id="name" placeholder="e.g. Kitchen, Master Bedroom" {...register("name")} />
        {errors.name && (
          <p role="alert" className="text-sm text-destructive">
            {errors.name.message}
          </p>
        )}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="type">Type</Label>
        <Input id="type" placeholder="e.g. Kitchen, Bedroom, Bathroom" {...register("type")} />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="description">Description</Label>
        <Textarea id="description" rows={3} {...register("description")} />
      </div>
      <Button type="submit" disabled={submitting} className="self-start">
        {submitting ? "Saving…" : "Save"}
      </Button>
    </form>
  );
}
