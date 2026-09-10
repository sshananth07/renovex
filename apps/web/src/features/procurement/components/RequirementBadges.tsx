import { Badge } from "@/components/ui/badge";
import type { RequirementBadge, RequirementBadgeType } from "../requirementPresentation";

const VARIANT_BY_TYPE: Record<RequirementBadgeType, "default" | "secondary" | "outline" | "destructive"> = {
  archived: "outline",
  split: "outline",
  "split-incomplete": "destructive",
  superseded: "outline",
  claimed: "secondary",
  draft: "outline",
  reviewed: "secondary",
  "rfq-ready": "default",
  "unit-mismatch": "destructive",
  "source-changed": "destructive",
  "source-removed": "destructive",
  unavailable: "outline",
};

export function RequirementBadges({ badges }: { badges: RequirementBadge[] }) {
  if (badges.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1.5">
      {badges.map((badge) => (
        <Badge key={badge.type} variant={VARIANT_BY_TYPE[badge.type]}>
          {badge.label}
        </Badge>
      ))}
    </div>
  );
}
