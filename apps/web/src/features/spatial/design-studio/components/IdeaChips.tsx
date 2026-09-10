// Small checked-in category -> chip map (M8.5C plan: "Chips are derived
// from a small checked-in category map with a neutral fallback... Do not
// call another AI service to generate chips."). Each chip is under 28
// characters (plan's exact bound) and inserts/edits prompt text — it never
// submits automatically.
const CHIPS_BY_CATEGORY: Record<string, string[]> = {
  sofa: ["Make it softer", "Try warm timber legs", "Curve the back", "Move it off the wall"],
  chair: ["Make it softer", "Try a curved back", "Change the finish", "Move it away from the wall"],
  table: ["Try a rounded top", "Change the finish", "Make it narrower", "Move it to the center"],
  cabinet: ["Try warm timber", "Add a matte finish", "Make it narrower", "Move it flush to the wall"],
  ac: ["Recess it into the wall", "Move it higher"],
  boiler: ["Move it closer to the wall"],
};

const NEUTRAL_FALLBACK_CHIPS = ["Change the finish", "Try a different shape", "Move it slightly", "Make it more compact"];

export function chipsForCategory(category: string): string[] {
  const chips = CHIPS_BY_CATEGORY[category.toLowerCase()] ?? NEUTRAL_FALLBACK_CHIPS;
  return chips.slice(0, 5);
}

export function IdeaChips({ category, onSelect }: { category: string; onSelect: (text: string) => void }) {
  const chips = chipsForCategory(category);
  return (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label="Idea suggestions">
      {chips.map((chip) => (
        <button
          key={chip}
          type="button"
          onClick={() => onSelect(chip)}
          className="rounded-full border border-border/70 bg-accent/40 px-3 py-1 text-xs font-medium text-foreground transition-transform duration-100 hover:bg-accent/70 active:scale-[.98]"
        >
          {chip}
        </button>
      ))}
    </div>
  );
}
