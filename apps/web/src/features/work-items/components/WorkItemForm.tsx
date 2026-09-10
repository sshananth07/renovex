"use client";

import { Controller, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { workItemFormSchema, type WorkItemFormValues } from "../schemas";
import type { Space } from "@/features/spaces/api";

const NO_SPACE_VALUE = "__no_space__";

interface WorkItemFormProps {
  spaces: Space[];
  defaultValues?: Partial<WorkItemFormValues>;
  onSubmit: (values: WorkItemFormValues) => void;
  submitting?: boolean;
}

// Create and edit share this form. spaceId is optional here (an empty
// selection submits as undefined, i.e. "omit" on create, or explicit
// clearing is handled by the caller translating "" to null on edit — see
// WorkItemList's tri-state mapping into UpdateWorkItemInput).
export function WorkItemForm({ spaces, defaultValues, onSubmit, submitting }: WorkItemFormProps) {
  const {
    control,
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<WorkItemFormValues>({
    resolver: zodResolver(workItemFormSchema),
    defaultValues: {
      description: "",
      quantityValue: "",
      quantityUnit: "",
      workType: "",
      spaceId: "",
      ...defaultValues,
    },
  });

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="description">Description</Label>
        <Textarea id="description" rows={2} {...register("description")} />
        {errors.description && (
          <p role="alert" className="text-sm text-destructive">
            {errors.description.message}
          </p>
        )}
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="quantityValue">Quantity</Label>
          <Input id="quantityValue" inputMode="decimal" placeholder="e.g. 12.5" {...register("quantityValue")} />
          {errors.quantityValue && (
            <p role="alert" className="text-sm text-destructive">
              {errors.quantityValue.message}
            </p>
          )}
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="quantityUnit">Unit</Label>
          <Input id="quantityUnit" placeholder="e.g. sqft, pcs, lot" {...register("quantityUnit")} />
          {errors.quantityUnit && (
            <p role="alert" className="text-sm text-destructive">
              {errors.quantityUnit.message}
            </p>
          )}
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="workType">Work type</Label>
        <Input id="workType" placeholder="e.g. Carpentry, Plumbing, Electrical" {...register("workType")} />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="spaceId">Space</Label>
        <Controller
          control={control}
          name="spaceId"
          render={({ field }) => (
            <Select
              value={field.value || NO_SPACE_VALUE}
              onValueChange={(value) => field.onChange(value === NO_SPACE_VALUE ? "" : value)}
            >
              <SelectTrigger id="spaceId" aria-label="Space" className="w-full">
                <SelectValue>
                  {field.value
                    ? spaces.find((space) => space.id === field.value)?.name ?? "Unknown space"
                    : "No space assigned"}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_SPACE_VALUE}>No space assigned</SelectItem>
                {spaces.map((space) => (
                  <SelectItem key={space.id} value={space.id}>
                    {space.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
      </div>

      <Button type="submit" disabled={submitting} className="self-start">
        {submitting ? "Saving…" : "Save"}
      </Button>
    </form>
  );
}
