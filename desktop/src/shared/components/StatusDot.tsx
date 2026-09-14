import { cn } from "../lib/utils.js";

/**
 * The one status indicator this app has.
 *
 * Four tones, and each one is a dot plus a label — never a bare colour. Every
 * place this appears, the reader has to know WHICH thing is wrong, and a wall
 * of coloured dots with no words is a wall that has to be decoded. The colour
 * is the fast path; the label is the answer.
 */
export type Tone = "ok" | "warn" | "bad" | "idle";

const TONE_CLASS: Record<Tone, string> = {
  ok: "bg-success",
  warn: "bg-warning",
  bad: "bg-destructive",
  idle: "bg-muted-foreground/50",
};

export function StatusDot({ tone, pulse = false, className }: { tone: Tone; pulse?: boolean; className?: string }) {
  return (
    <span className={cn("relative inline-flex size-2.5 shrink-0", className)}>
      {pulse ? (
        <span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", TONE_CLASS[tone])} />
      ) : null}
      <span className={cn("relative inline-flex size-2.5 rounded-full", TONE_CLASS[tone])} />
    </span>
  );
}

export function StatusRow({
  tone,
  label,
  value,
  detail,
  pulse,
  action,
}: {
  tone: Tone;
  label: string;
  value: string;
  detail?: string | undefined;
  pulse?: boolean;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex items-start gap-3 py-2.5">
      <StatusDot tone={tone} pulse={pulse ?? false} className="mt-1.5" />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-medium">{label}</span>
          <span className="truncate text-sm text-muted-foreground">{value}</span>
        </div>
        {detail ? <p className="selectable mt-0.5 text-xs leading-relaxed text-muted-foreground">{detail}</p> : null}
      </div>
      {action ? <div className="no-drag shrink-0">{action}</div> : null}
    </div>
  );
}
