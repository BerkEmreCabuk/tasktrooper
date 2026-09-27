import { useState, type KeyboardEvent } from "react";
import type { UsageByDay } from "@/api";
import { formatCompact, formatDay, formatInteger, niceCeil } from "@/lib/usage";
import { cn } from "@/lib/utils";

export interface DailySeries {
  key: string;
  label: string;
  swatch: string;
  value: (d: UsageByDay) => number;
}

interface UsageDailyChartProps {
  days: UsageByDay[];
  // Bottom of the stack first.
  series: DailySeries[];
  lang: string;
  ariaLabel: string;
  totalLabel: string;
  callsLabel: (count: string) => string;
}

// Segments thinner than this share of their column are left to the tooltip:
// the 2px gap around a sliver would draw more ink than the value itself.
const MIN_SEGMENT_SHARE = 0.01;

function tickIndices(n: number): Set<number> {
  if (n <= 10) return new Set(Array.from({ length: n }, (_, i) => i));
  const step = n <= 31 ? 7 : 15;
  const out = new Set<number>();
  for (let i = n - 1; i >= 0; i -= step) out.add(i);
  return out;
}

export function UsageDailyChart({ days, series, lang, ariaLabel, totalLabel, callsLabel }: UsageDailyChartProps) {
  const [active, setActive] = useState<number | null>(null);

  const totals = days.map((d) => series.reduce((sum, s) => sum + s.value(d), 0));
  const max = niceCeil(Math.max(0, ...totals));
  const ticks = tickIndices(days.length);
  const n = days.length;

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (n === 0) return;
    const cur = active ?? n - 1;
    let next = cur;
    if (e.key === "ArrowLeft") next = Math.max(0, cur - 1);
    else if (e.key === "ArrowRight") next = Math.min(n - 1, cur + 1);
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = n - 1;
    else if (e.key === "Escape") {
      setActive(null);
      return;
    } else return;
    e.preventDefault();
    setActive(next);
  }

  const activeDay = active !== null ? days[active] : null;
  // Beside the hovered column, never over it: to its right in the left part
  // of the plot, to its left after that.
  const tooltipAfter = active !== null && active < n * 0.6;

  return (
    <div className="flex gap-3">
      <div className="relative h-56 w-10 shrink-0 text-right text-micro tabular-nums text-muted-foreground">
        {[max, max / 2, 0].map((v, i) => (
          <span key={i} className="absolute right-0 -translate-y-1/2" style={{ top: `${(i / 2) * 100}%` }}>
            {formatCompact(v)}
          </span>
        ))}
      </div>

      <div className="min-w-0 flex-1">
        <div
          role="img"
          aria-label={ariaLabel}
          tabIndex={0}
          onKeyDown={onKeyDown}
          onFocus={() => setActive((a) => a ?? n - 1)}
          onBlur={() => setActive(null)}
          onPointerLeave={() => setActive(null)}
          className="relative h-56 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-4 focus-visible:ring-offset-card"
        >
          {[0, 0.5, 1].map((f) => (
            <div
              key={f}
              aria-hidden
              className={cn("absolute inset-x-0 h-px", f === 1 ? "bg-border" : "bg-border/50")}
              style={{ top: `${f * 100}%` }}
            />
          ))}

          <div aria-hidden className="absolute inset-0 flex items-stretch gap-[2px]">
            {days.map((d, i) => {
              const total = totals[i];
              const visible = series.filter((s) => total > 0 && s.value(d) / total >= MIN_SEGMENT_SHARE);
              return (
                <div
                  key={d.day}
                  onPointerEnter={() => setActive(i)}
                  className={cn(
                    "flex min-w-0 flex-1 flex-col items-center justify-end transition-opacity duration-150",
                    active !== null && active !== i && "opacity-40",
                  )}
                >
                  {total > 0 && (
                    <div
                      className="flex w-full max-w-6 flex-col-reverse gap-[2px] overflow-hidden rounded-t-[4px]"
                      style={{ height: `max(2px, ${(total / max) * 100}%)` }}
                    >
                      {visible.map((s) => (
                        <div key={s.key} className={s.swatch} style={{ flex: `${s.value(d)} 1 0px`, minHeight: 1 }} />
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          {activeDay && active !== null && (
            <div
              aria-hidden
              className={cn(
                "pointer-events-none absolute top-2 z-10 min-w-44 rounded-lg border border-border bg-popover px-3 py-2 text-caption text-popover-foreground shadow-[var(--shadow-overlay)]",
                tooltipAfter ? "ml-2" : "-ml-2 -translate-x-full",
              )}
              style={{ left: `${((tooltipAfter ? active + 1 : active) / n) * 100}%` }}
            >
              <p className="mb-1.5 font-medium">
                {formatDay(activeDay.day, lang, { weekday: "short", day: "numeric", month: "long" })}
              </p>
              <ul className="space-y-1">
                {[...series].reverse().map((s) => (
                  <li key={s.key} className="flex items-center gap-2">
                    <span className={cn("h-0.5 w-3 shrink-0 rounded-full", s.swatch)} />
                    <span className="font-semibold tabular-nums">{formatInteger(s.value(activeDay), lang)}</span>
                    <span className="text-muted-foreground">{s.label}</span>
                  </li>
                ))}
              </ul>
              {series.length > 1 && (
                <p className="mt-1.5 border-t border-border pt-1.5">
                  <span className="font-semibold tabular-nums">{formatInteger(totals[active], lang)}</span>{" "}
                  <span className="text-muted-foreground">{totalLabel}</span>
                </p>
              )}
              <p className={cn("text-muted-foreground", series.length === 1 && "mt-1.5 border-t border-border pt-1.5")}>
                {callsLabel(formatInteger(activeDay.calls, lang))}
              </p>
            </div>
          )}
        </div>

        <div aria-hidden className="relative mt-2 h-4 text-micro text-muted-foreground">
          {days.map((d, i) =>
            ticks.has(i) ? (
              <span
                key={d.day}
                className={cn(
                  "absolute whitespace-nowrap",
                  i === 0 ? "" : i === n - 1 ? "-translate-x-full" : "-translate-x-1/2",
                )}
                style={{ left: `${((i === 0 ? 0 : i === n - 1 ? n : i + 0.5) / n) * 100}%` }}
              >
                {formatDay(d.day, lang, { day: "numeric", month: "short" })}
              </span>
            ) : null,
          )}
        </div>
      </div>

      <table className="sr-only">
        <caption>{ariaLabel}</caption>
        <thead>
          <tr>
            <th scope="col" />
            {series.map((s) => (
              <th key={s.key} scope="col">
                {s.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {days.map((d) => (
            <tr key={d.day}>
              <th scope="row">{d.day}</th>
              {series.map((s) => (
                <td key={s.key}>{formatInteger(s.value(d), lang)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
