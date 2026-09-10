"use client";

import { Controller, useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { projectFormSchema, type ProjectFormValues } from "../schemas";
import type { Client } from "@/features/clients/api";

interface ProjectFormProps {
  clients: Client[];
  onSubmit: (values: ProjectFormValues) => void;
  submitting?: boolean;
  defaultClientId?: string;
}

// Project creation: a client picker plus a name. This form is create-only —
// PATCH /projects/{projectId} is a separate, single-field rename handled by
// ProjectOverview's inline rename affordance, not this component.
export function ProjectForm({ clients, onSubmit, submitting, defaultClientId }: ProjectFormProps) {
  const {
    control,
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<ProjectFormValues>({
    resolver: zodResolver(projectFormSchema),
    defaultValues: { clientId: defaultClientId ?? "", name: "" },
  });

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="clientId">Client</Label>
        <Controller
          control={control}
          name="clientId"
          render={({ field }) => (
            <Select value={field.value} onValueChange={field.onChange}>
              <SelectTrigger id="clientId" aria-label="Client" className="w-full">
                <SelectValue placeholder="Select a client">
                  {(value) => clients.find((client) => client.id === value)?.name ?? "Select a client"}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {clients.map((client) => (
                  <SelectItem key={client.id} value={client.id}>
                    {client.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        />
        {errors.clientId && (
          <p role="alert" className="text-sm text-destructive">
            {errors.clientId.message}
          </p>
        )}
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="name">Name</Label>
        <Input id="name" {...register("name")} />
        {errors.name && (
          <p role="alert" className="text-sm text-destructive">
            {errors.name.message}
          </p>
        )}
      </div>
      <Button type="submit" disabled={submitting}>
        {submitting ? "Saving…" : "Save"}
      </Button>
    </form>
  );
}
