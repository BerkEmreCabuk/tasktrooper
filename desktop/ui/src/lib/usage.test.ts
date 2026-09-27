import { describe, expect, it } from "vitest";
import type { UsageByModel } from "@/api";
import {
  EMPTY_TOTALS,
  breakdown,
  cacheHitRate,
  fillDays,
  formatCompact,
  formatDay,
  formatPercent,
  niceCeil,
  rowsOfKind,
  sumTotals,
} from "@/lib/usage";

describe("breakdown", () => {
  it("takes both cache shares out of the prompt total instead of adding them", () => {
    const b = breakdown({
      calls: 1,
      prompt_tokens: 1_000,
      completion_tokens: 50,
      cache_read_tokens: 700,
      cache_write_tokens: 200,
    });
    expect(b).toEqual({ fresh: 100, cacheWrite: 200, cacheRead: 700, output: 50, total: 1_050 });
  });

  it("never reports negative fresh input for a row with inconsistent cache counts", () => {
    expect(breakdown({ ...EMPTY_TOTALS, prompt_tokens: 10, cache_read_tokens: 20 }).fresh).toBe(0);
  });
});

describe("cacheHitRate", () => {
  it("is null with no input, so the page can show a dash instead of 0%", () => {
    expect(cacheHitRate(EMPTY_TOTALS)).toBeNull();
    expect(cacheHitRate({ ...EMPTY_TOTALS, prompt_tokens: 200, cache_read_tokens: 150 })).toBe(0.75);
  });
});

describe("fillDays", () => {
  it("fills every calendar day of the window, keeping the server's rows", () => {
    const got = fillDays("2026-09-25", "2026-09-27", [{ day: "2026-09-26", ...EMPTY_TOTALS, calls: 3 }]);
    expect(got.map((d) => [d.day, d.calls])).toEqual([
      ["2026-09-25", 0],
      ["2026-09-26", 3],
      ["2026-09-27", 0],
    ]);
  });

  it("crosses a DST change without skipping or repeating a day", () => {
    const got = fillDays("2026-10-24", "2026-10-27", []);
    expect(got.map((d) => d.day)).toEqual(["2026-10-24", "2026-10-25", "2026-10-26", "2026-10-27"]);
  });
});

describe("kind helpers", () => {
  const row = (kind: UsageByModel["kind"], calls: number): UsageByModel => ({
    kind,
    provider: "p",
    model: "m",
    ...EMPTY_TOTALS,
    calls,
  });

  it("splits rows by kind and sums them", () => {
    const rows = [row("cli", 2), row("api", 3), row("embedding", 100), row("cli", 4)];
    expect(sumTotals(rowsOfKind(rows, "cli")).calls).toBe(6);
    expect(sumTotals(rowsOfKind(rows, "cli", "api")).calls).toBe(9);
  });
});

describe("formatting", () => {
  it("keeps K/M/B compact suffixes whatever the app language", () => {
    expect(formatCompact(7_800)).toBe("7.8K");
    expect(formatCompact(48_765_110)).toBe("48.8M");
    expect(formatCompact(1_118_158_216)).toBe("1.1B");
  });

  it("formats a bucket day as that calendar date regardless of the viewer's offset", () => {
    expect(formatDay("2026-09-27", "en", { day: "numeric", month: "short" })).toBe("Sep 27");
  });

  it("rounds an axis maximum up to a clean step", () => {
    expect(niceCeil(0)).toBe(1);
    expect(niceCeil(8_800_000)).toBe(10_000_000);
    expect(niceCeil(2_300)).toBe(3_000);
    expect(niceCeil(2_450_000)).toBe(3_000_000);
  });

  it("never rounds a real but tiny share down to zero", () => {
    expect(formatPercent(541_900 / 1_118_158_216, "en")).toBe("<0.1%");
    expect(formatPercent(0.031, "tr")).toBe("%3,1");
    expect(formatPercent(0, "en")).toBe("0%");
  });
});
