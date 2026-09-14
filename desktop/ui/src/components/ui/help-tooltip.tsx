import { Info } from "lucide-react";
import { cn } from "@/lib/utils";

interface HelpTooltipProps {
  text: string;
  className?: string;
}

/** A hover-only explanation for a control whose full description is too long
 * to sit on the page permanently. Native `title` — no extra dependency. */
export function HelpTooltip({ text, className }: HelpTooltipProps) {
  return (
    <span
      title={text}
      tabIndex={0}
      className={cn("inline-flex shrink-0 cursor-help text-muted-foreground outline-none", className)}
    >
      <Info className="h-3.5 w-3.5" aria-hidden />
      <span className="sr-only">{text}</span>
    </span>
  );
}
