import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { UsageSummary } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { EMPTY_TOTALS } from "@/lib/usage";
import { UsageSettingsPage } from "@/pages/UsageSettingsPage";

const { usageSummary } = vi.hoisted(() => ({ usageSummary: vi.fn() }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, usageSummary } };
});

const summary: UsageSummary = {
  days: 7,
  timezone: "Europe/Istanbul",
  from: "2026-09-21",
  to: "2026-09-27",
  generation: {
    calls: 12,
    prompt_tokens: 1_118_158_216,
    completion_tokens: 5_550_364,
    cache_read_tokens: 1_082_966_876,
    cache_write_tokens: 34_649_488,
  },
  embedding: { ...EMPTY_TOTALS, calls: 305_613, prompt_tokens: 48_765_110 },
  by_model: [
    {
      kind: "cli",
      provider: "claude_code",
      model: "claude-opus-5-5",
      calls: 10,
      prompt_tokens: 1_000_000_000,
      completion_tokens: 5_000_000,
      cache_read_tokens: 990_000_000,
      cache_write_tokens: 9_000_000,
    },
    {
      kind: "cli",
      provider: "claude_code",
      model: "(unrecorded)",
      calls: 2,
      prompt_tokens: 118_158_216,
      completion_tokens: 550_364,
      cache_read_tokens: 92_966_876,
      cache_write_tokens: 25_649_488,
    },
    { kind: "embedding", provider: "local", model: "(default)", ...EMPTY_TOTALS, calls: 305_613, prompt_tokens: 48_765_110 },
  ],
  daily: [
    {
      day: "2026-09-26",
      calls: 12,
      prompt_tokens: 1_118_158_216,
      completion_tokens: 5_550_364,
      cache_read_tokens: 1_082_966_876,
      cache_write_tokens: 34_649_488,
    },
  ],
};

function renderPage() {
  return render(
    <I18nProvider>
      <UsageSettingsPage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  usageSummary.mockReset();
  sessionStorage.clear();
});

describe("UsageSettingsPage", () => {
  it("reports agent CLI tokens as input/output and keeps embeddings apart", async () => {
    usageSummary.mockResolvedValue(summary);
    renderPage();

    const input = (await screen.findByText("Input tokens")).closest("div")!;
    expect(within(input).getByText("1.1B")).toBeInTheDocument();
    expect(within(input).getByText("93.2M per session on average")).toBeInTheDocument();
    const output = screen.getByText("Output tokens").closest("div")!;
    expect(within(output).getByText("5.6M")).toBeInTheDocument();

    expect(screen.getByText("claude-opus-5-5")).toBeInTheDocument();
    expect(screen.getByText("Not recorded")).toBeInTheDocument();
    expect(screen.getByText("12 agent sessions · 0 API calls")).toBeInTheDocument();

    const embeddings = screen.getByText("Embeddings").closest("div")!.parentElement!;
    expect(within(embeddings).getByText("~48.8M")).toBeInTheDocument();
    expect(screen.queryByText("(default)")).not.toBeInTheDocument();
  });

  it("asks for the range in the viewer's timezone and refetches on a new range", async () => {
    usageSummary.mockResolvedValue(summary);
    renderPage();
    await screen.findByText("Input tokens");

    expect(usageSummary).toHaveBeenLastCalledWith(30, Intl.DateTimeFormat().resolvedOptions().timeZone);
    fireEvent.click(screen.getByRole("tab", { name: "Last 7 days" }));
    expect(usageSummary).toHaveBeenLastCalledWith(7, expect.any(String));
  });

  it("shows an empty state when nothing was recorded", async () => {
    usageSummary.mockResolvedValue({
      ...summary,
      generation: EMPTY_TOTALS,
      embedding: EMPTY_TOTALS,
      by_model: [],
      daily: [],
    });
    renderPage();

    expect(await screen.findByText("No usage in this range")).toBeInTheDocument();
  });
});
