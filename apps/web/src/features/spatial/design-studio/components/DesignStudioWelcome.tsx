import { IdeaChips } from "./IdeaChips";

// Exact copy contract (M8.5C plan):
//   Selected: Sofa
//   Heading: "What should we try with this sofa?"
//   Support: "Describe the shape, finish, or placement you have in mind."
//   Chips: derived from the category map, 3-5 chips.
export function DesignStudioWelcome({ category, onSelectChip }: { category: string; onSelectChip: (text: string) => void }) {
  return (
    <div className="grid gap-3">
      <div className="grid gap-1">
        <h2 className="text-xl font-semibold text-foreground sm:text-2xl">What should we try with this {category.toLowerCase()}?</h2>
        <p className="text-sm leading-5 text-muted-foreground">Describe the shape, finish, or placement you have in mind.</p>
      </div>
      <IdeaChips category={category} onSelect={onSelectChip} />
    </div>
  );
}
