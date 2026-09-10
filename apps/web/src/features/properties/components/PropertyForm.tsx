"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { propertyFormSchema, type PropertyFormValues } from "../schemas";

interface PropertyFormProps {
  defaultValues?: Partial<PropertyFormValues>;
  onSubmit: (values: PropertyFormValues) => void;
  submitting?: boolean;
}

// Create and edit share this form. Unlike ClientForm, PropertyDetail always
// resubmits `address` in full — UpdatePropertyInputBody requires it on
// every update, it is not a genuine partial PATCH.
export function PropertyForm({ defaultValues, onSubmit, submitting }: PropertyFormProps) {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<PropertyFormValues>({
    resolver: zodResolver(propertyFormSchema),
    defaultValues: { address: "", propertyType: "", notes: "", ...defaultValues },
  });

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="address">Address</Label>
        <Textarea id="address" rows={2} {...register("address")} />
        {errors.address && (
          <p role="alert" className="text-sm text-destructive">
            {errors.address.message}
          </p>
        )}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="propertyType">Property type</Label>
        <Input
          id="propertyType"
          placeholder="e.g. Landed, Condominium, Commercial"
          {...register("propertyType")}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="notes">Notes</Label>
        <Textarea id="notes" rows={3} {...register("notes")} />
      </div>
      <Button type="submit" disabled={submitting} className="self-start">
        {submitting ? "Saving…" : "Save"}
      </Button>
    </form>
  );
}
