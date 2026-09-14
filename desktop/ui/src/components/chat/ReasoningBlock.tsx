import { Brain, ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface ReasoningBlockProps {
  /** Closed reasoning stretches, oldest first. Empty renders nothing. */
  segments: string[];
  /**
   * Open on mount. The live bubble passes true so a working run is not silent;
   * a finished message leaves it false, because by then the answer is the thing
   * being read and the reasoning is there only for whoever wants it.
   */
  defaultOpen?: boolean;
  className?: string;
}

/**
 * The model's reasoning ahead of its tool calls, folded away. Deliberately not a
 * message bubble: this text explains why a call was made, and rendering it like
 * a reply is what made the agent look as if it had answered twice — once with
 * its thinking, then again for real.
 */
export function ReasoningBlock({ segments, defaultOpen = false, className }: ReasoningBlockProps) {
  const { t } = useI18n();
  const [open, setOpen] = useState(defaultOpen);
  if (segments.length === 0) return null;

  return (
    // min-w-0: this block is dropped straight into a `flex justify-start` row
    // by MessageList. A flex item's default min-width is its content size, so
    // without this a wide table/URL inside a reasoning segment would grow the
    // whole block past its `max-w-[85%]` instead of letting the table's own
    // overflow-x:auto (below) do the clipping.
    <div className={cn("min-w-0 rounded-xl border border-dashed border-border/70 bg-muted/40", className)}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs text-muted-foreground transition-colors hover:text-foreground"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0" />
        )}
        <Brain className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{t("chatArea.chat.reasoning.title", { count: segments.length })}</span>
      </button>
      {open && (
        <div className="space-y-3 border-t border-dashed border-border/70 px-3 py-2">
          {segments.map((segment, i) => (
            <MarkdownContent key={`${i}-${segment.slice(0, 24)}`} content={segment} className="text-xs opacity-80" />
          ))}
        </div>
      )}
    </div>
  );
}
