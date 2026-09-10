import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { projectStatusLabel, projectStatusTone } from "../statusPresentation";

const TONE_CLASSES: Record<string, string> = {
  neutral: "",
  active: "border border-primary/20 bg-primary/8 text-primary",
  success: "border-transparent bg-emerald-600/10 text-emerald-700 dark:bg-emerald-400/10 dark:text-emerald-400",
};

// Three-tone status indicator (architecture doc §13/§21: accent reserved
// for state indication, not decoration). "neutral" reuses Badge's own
// outline variant; "active"/"success" apply a restrained tint on top.
export function StatusBadge({ status }: { status: string }) {
  const tone = projectStatusTone(status);
  return (
    <Badge variant={tone === "neutral" ? "outline" : "default"} className={cn(TONE_CLASSES[tone])}>
      {projectStatusLabel(status)}
    </Badge>
  );
}
