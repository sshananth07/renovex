"use client";

import { useState } from "react";
import { ChevronDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useUpdateProjectStatus } from "../mutations";
import {
  PROJECT_STATUSES,
  projectStatusLabel,
  type ProjectStatus,
} from "../statusPresentation";
import { StatusBadge } from "./StatusBadge";

interface BackendProblem {
  title?: string;
  detail?: string;
  errors?: Array<{ message?: string }>;
}

function projectStatusErrorMessage(error: unknown): string {
  const problem = error as BackendProblem | null;
  return problem?.detail
    ?? problem?.errors?.find((item) => item.message)?.message
    ?? problem?.title
    ?? "The project status could not be updated.";
}

export function ProjectStatusDialog({
  projectId,
  currentStatus,
}: {
  projectId: string;
  currentStatus: string;
}) {
  const [open, setOpen] = useState(false);
  const [selectedStatus, setSelectedStatus] = useState<ProjectStatus>(currentStatus as ProjectStatus);
  const updateStatus = useUpdateProjectStatus(projectId);

  function handleOpenChange(nextOpen: boolean) {
    setOpen(nextOpen);
    if (nextOpen) {
      setSelectedStatus(currentStatus as ProjectStatus);
      updateStatus.reset();
    }
  }

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-auto gap-1.5 p-0 hover:bg-transparent"
        aria-label={`Change project status, current status ${projectStatusLabel(currentStatus)}`}
        onClick={() => handleOpenChange(true)}
      >
        <StatusBadge status={currentStatus} />
        <ChevronDown className="size-3.5 text-muted-foreground" />
      </Button>

      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Change project status</DialogTitle>
            <DialogDescription>
              Update the project lifecycle independently from its setup checklist.
            </DialogDescription>
          </DialogHeader>

          <div className="grid gap-5">
            <div className="grid gap-2">
              <Label>Current status</Label>
              <StatusBadge status={currentStatus} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="project-new-status">New status</Label>
              <Select
                value={selectedStatus}
                onValueChange={(value) => {
                  if (value) setSelectedStatus(value as ProjectStatus);
                }}
              >
                <SelectTrigger id="project-new-status" className="w-full">
                  <SelectValue>
                    {(value) => projectStatusLabel(String(value))}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {PROJECT_STATUSES.map((status) => (
                    <SelectItem key={status} value={status}>
                      {projectStatusLabel(status)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <p className="text-sm text-muted-foreground">
              Changing the lifecycle status does not alter project setup progress.
            </p>
            {updateStatus.error && (
              <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">
                {projectStatusErrorMessage(updateStatus.error)}
              </p>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button
              type="button"
              disabled={selectedStatus === currentStatus || updateStatus.isPending}
              onClick={() => updateStatus.mutate(selectedStatus, { onSuccess: () => setOpen(false) })}
            >
              Update status
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
