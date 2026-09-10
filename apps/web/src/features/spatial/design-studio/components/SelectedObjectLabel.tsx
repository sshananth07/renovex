// A restrained DOM label mirroring the selected element's canonical
// identity — Apple RoomPlan's "recognized-element feedback" principle
// borrowed narrowly (never its AR chrome/translucent materials). Floats
// 16px from the canvas top-left edge per the plan's exact layout contract;
// positioning is the parent SpatialCanvasStage's concern (this component is
// unpositioned, absolute placement applied by the caller). Never displays a
// raw RoomDraft id — only the human-readable category.
export function SelectedObjectLabel({ category, disambiguator }: { category: string; disambiguator?: string }) {
  return (
    <div
      role="status"
      aria-live="off"
      className="pointer-events-none flex items-center gap-2 rounded-full border border-border/70 bg-card/90 px-3 py-1.5 text-xs font-medium text-foreground shadow-sm backdrop-blur-sm"
    >
      <span className="inline-block size-1.5 rounded-full bg-primary" aria-hidden="true" />
      <span className="capitalize">{category}</span>
      {disambiguator && <span className="text-muted-foreground">· {disambiguator}</span>}
    </div>
  );
}
