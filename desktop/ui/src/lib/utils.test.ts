import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cn, formatRelativeDate } from "@/lib/utils";

describe("cn", () => {
  it("keeps a text-color utility next to one of our custom text-size utilities", () => {
    // Regression: tailwind-merge doesn't know our globals.css `--text-*` scale
    // (text-caption, text-micro, …), so it used to misclassify these as
    // text-color and silently drop `text-primary-foreground` — every
    // `size="sm"` Button (bg-primary text-primary-foreground ... text-caption)
    // lost its text color this way and rendered unreadable.
    for (const size of ["text-display", "text-title", "text-heading", "text-body", "text-caption", "text-micro"]) {
      const merged = cn("text-primary-foreground", size);
      expect(merged.split(" ")).toEqual(expect.arrayContaining(["text-primary-foreground", size]));
    }
  });

  it("still lets a real text-color utility override an earlier one", () => {
    expect(cn("text-primary-foreground", "text-destructive")).toBe("text-destructive");
  });
});

describe("formatRelativeDate", () => {
  const now = new Date("2026-09-27T01:44:00Z");
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(now);
  });
  afterEach(() => {
    vi.useRealTimers();
  });
  const at = (offsetMs: number) => new Date(now.getTime() + offsetMs).toISOString();

  it("reads a past time as elapsed", () => {
    expect(formatRelativeDate(at(-30_000))).toBe("Just now");
    expect(formatRelativeDate(at(-2 * 60_000))).toBe("2m ago");
  });

  it("reads a future time as remaining instead of 'Just now'", () => {
    // Regression: a release's verify_until sits in the future for the whole
    // soak window and used to render as "Just now", which read as finished.
    expect(formatRelativeDate(at(7 * 60_000 + 38_000))).toBe("in 8m");
    expect(formatRelativeDate(at(3 * 3_600_000))).toBe("in 3h");
  });
});
