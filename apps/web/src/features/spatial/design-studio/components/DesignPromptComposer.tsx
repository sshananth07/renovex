import { useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { WandSparkles } from "lucide-react";

const MIN_HEIGHT_PX = 72;
const MAX_HEIGHT_PX = 132;

// Compact multiline prompt with auto-grow ONLY between 72-132px (M8.5C
// plan's exact bound) — Enter inserts a newline, Ctrl/Cmd+Enter submits.
// The primary action lives OUTSIDE the textarea (never an inline send
// icon), and this never renders an avatar, chat bubble, or transcript.
export function DesignPromptComposer({
  value,
  onChange,
  onSubmit,
  disabled,
  placeholder = "Try a curved back in deep green fabric…",
}: {
  value: string;
  onChange: (value: string) => void;
  onSubmit: () => void;
  disabled?: boolean;
  placeholder?: string;
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    const next = Math.min(MAX_HEIGHT_PX, Math.max(MIN_HEIGHT_PX, el.scrollHeight));
    el.style.height = `${next}px`;
  }, [value]);

  function handleKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      if (!disabled && value.trim().length > 0) onSubmit();
    }
    // A plain Enter is left to the textarea's default newline behavior.
  }

  return (
    <div className="grid gap-2">
      <Textarea
        ref={textareaRef}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        disabled={disabled}
        rows={3}
        className="resize-none overflow-y-auto text-sm leading-5"
        style={{ minHeight: MIN_HEIGHT_PX, maxHeight: MAX_HEIGHT_PX }}
      />
      <div className="flex justify-end">
        <Button size="sm" onClick={onSubmit} disabled={disabled || value.trim().length === 0}>
          <WandSparkles className="size-4 stroke-[1.75]" />
          Imagine changes
        </Button>
      </div>
    </div>
  );
}
