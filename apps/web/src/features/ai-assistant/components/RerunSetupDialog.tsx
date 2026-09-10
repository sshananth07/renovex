"use client";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

interface RerunSetupDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}

// Shown only when the brief is unchanged since the last generation (T1.5
// PART C §14) — a changed brief skips this and goes straight to "Generate
// updates" instead. The comparison workflow itself lives inline on the
// Project Setup page, not in this modal.
export function RerunSetupDialog({ open, onOpenChange, onConfirm }: RerunSetupDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Re-run AI setup?</DialogTitle>
          <DialogDescription>
            Your project and previous setup decisions will be preserved. Renovex will compare the new
            suggestions with the existing project and show only meaningful differences.
            <br />
            <br />
            The brief has not changed, so another run may produce slightly different suggestions.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={() => {
              onConfirm();
              onOpenChange(false);
            }}
          >
            Re-run and compare
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
