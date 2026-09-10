"use client";

import { useId, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
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
import type { Material, NewMaterialFormInput } from "./types";

const newMaterialSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  category: z.string().trim().optional(),
  specification: z.string().trim().optional(),
  unit: z.string().trim().min(1, "Unit is required"),
  referencePriceAmount: z
    .string()
    .trim()
    .min(1, "Reference price is required")
    .refine((v) => Number.isFinite(Number(v)) && Number(v) >= 0, "Enter a non-negative amount"),
});

type NewMaterialFormValues = z.infer<typeof newMaterialSchema>;

interface MaterialResourceAcceptDialogProps {
  suggestedName: string;
  candidate: Material | null;
  otherMaterials: Material[];
  onAcceptExisting: (materialId: string) => void;
  onCreateAndAdd: (input: NewMaterialFormInput) => void;
  onReject: () => void;
  submitting: boolean;
  currency?: string;
}

type Mode = "review" | "chooseAnother" | "createAndAdd";

export function MaterialResourceAcceptDialog({
  suggestedName,
  candidate,
  otherMaterials,
  onAcceptExisting,
  onCreateAndAdd,
  onReject,
  submitting,
  currency = "USD",
}: MaterialResourceAcceptDialogProps) {
  const instanceId = useId();
  const [mode, setMode] = useState<Mode>("review");
  const [chosenId, setChosenId] = useState<string>("");
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<NewMaterialFormValues>({
    resolver: zodResolver(newMaterialSchema),
    defaultValues: { name: suggestedName, category: "", specification: "", unit: "", referencePriceAmount: "" },
  });

  const onSubmitNewMaterial = handleSubmit((values) => {
    onCreateAndAdd({
      name: values.name,
      category: values.category ?? "",
      specification: values.specification ?? "",
      unit: values.unit,
      referencePriceAmount: Math.round(Number(values.referencePriceAmount) * 100),
      referencePriceCurrency: currency,
    });
  });

  if (mode === "createAndAdd") {
    return (
      <form onSubmit={onSubmitNewMaterial} className="surface-card flex flex-col gap-3 p-4">
        <p className="text-sm font-semibold">{suggestedName}</p>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${instanceId}-name`}>Name</Label>
          <Input id={`${instanceId}-name`} {...register("name")} />
          {errors.name && <p role="alert" className="text-xs text-destructive">{errors.name.message}</p>}
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${instanceId}-category`}>Category</Label>
          <Input id={`${instanceId}-category`} {...register("category")} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${instanceId}-specification`}>Specification</Label>
          <Input id={`${instanceId}-specification`} {...register("specification")} />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${instanceId}-unit`}>Unit</Label>
            <Input id={`${instanceId}-unit`} {...register("unit")} />
            {errors.unit && <p role="alert" className="text-xs text-destructive">{errors.unit.message}</p>}
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${instanceId}-referencePriceAmount`}>Reference price</Label>
            <Input id={`${instanceId}-referencePriceAmount`} inputMode="decimal" {...register("referencePriceAmount")} />
            {errors.referencePriceAmount && (
              <p role="alert" className="text-xs text-destructive">{errors.referencePriceAmount.message}</p>
            )}
          </div>
        </div>
        <div className="flex gap-2">
          <Button type="submit" size="sm" disabled={submitting}>
            {submitting ? "Adding…" : "Add"}
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={() => setMode("review")}>
            Cancel
          </Button>
        </div>
      </form>
    );
  }

  if (mode === "chooseAnother") {
    return (
      <div className="surface-card flex flex-col gap-3 p-4">
        <p className="text-sm font-semibold">{suggestedName}</p>
        <Select value={chosenId} onValueChange={(value) => setChosenId(value as string)}>
          <SelectTrigger id={`${instanceId}-chooseAnother`} aria-label="Material" className="w-full">
            <SelectValue>
              {chosenId ? otherMaterials.find((m) => m.id === chosenId)?.name : "Select a material"}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {otherMaterials.map((m) => (
              <SelectItem key={m.id} value={m.id}>
                {m.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="flex gap-2">
          <Button
            size="sm"
            disabled={!chosenId || submitting}
            onClick={() => chosenId && onAcceptExisting(chosenId)}
          >
            Use selected
          </Button>
          <Button size="sm" variant="outline" onClick={() => setMode("review")}>
            Cancel
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="surface-card flex flex-col gap-3 p-4">
      <div>
        <p className="text-sm font-semibold">{suggestedName}</p>
        {candidate ? (
          <p className="mt-0.5 text-xs text-muted-foreground">Possible catalog match: {candidate.name}</p>
        ) : (
          <p className="mt-0.5 text-xs text-muted-foreground">No catalog match</p>
        )}
      </div>
      <div className="flex flex-wrap gap-2">
        {candidate && (
          <>
            <Button size="sm" disabled={submitting} onClick={() => onAcceptExisting(candidate.id)}>
              Use Existing
            </Button>
            {otherMaterials.length > 0 && (
              <Button size="sm" variant="outline" onClick={() => setMode("chooseAnother")}>
                Choose Another
              </Button>
            )}
          </>
        )}
        {!candidate && (
          <Button size="sm" onClick={() => setMode("createAndAdd")}>
            Create & Add
          </Button>
        )}
        <Button size="sm" variant="outline" disabled={submitting} onClick={onReject}>
          Reject
        </Button>
      </div>
    </div>
  );
}
