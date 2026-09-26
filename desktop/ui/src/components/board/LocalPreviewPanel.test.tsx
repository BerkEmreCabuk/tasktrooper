import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask, LocalPreview } from "@/api";
import { LocalPreviewPanel } from "@/components/board/LocalPreviewPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { getLocalPreview, startLocalPreview } = vi.hoisted(() => ({
  getLocalPreview: vi.fn(),
  startLocalPreview: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, getLocalPreview, startLocalPreview } };
});

const task = { id: "task-1" } as unknown as BoardTask;

function makePreview(overrides: Partial<LocalPreview> = {}): LocalPreview {
  return {
    repository_id: "repo-1",
    task_id: "task-1",
    branch: "task/T-64",
    command: "npm run dev",
    status: "starting",
    started_at: "2026-09-27T00:00:00Z",
    ...overrides,
  };
}

function renderPanel() {
  return render(
    <I18nProvider>
      <LocalPreviewPanel task={task} repositoryId="repo-1" />
    </I18nProvider>,
  );
}

describe("LocalPreviewPanel", () => {
  beforeEach(() => {
    getLocalPreview.mockReset();
    startLocalPreview.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows why this task's preview died and offers to run it again", async () => {
    getLocalPreview.mockResolvedValue({
      active: true,
      preview: makePreview({
        status: "failed",
        detail: "exit status 1",
        log_tail: ["> next dev", "⨯ Another next dev server is already running."],
      }),
    });
    renderPanel();

    expect(await screen.findByText("exit status 1")).toBeInTheDocument();
    expect(screen.getByText(/Another next dev server is already running/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Run locally/ })).toBeEnabled();
    expect(screen.queryByRole("button", { name: /Stop/ })).not.toBeInTheDocument();
  });

  it("opens the site once the preview it was asked to start is running", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    getLocalPreview.mockResolvedValueOnce({ active: false });
    startLocalPreview.mockResolvedValue(makePreview({ status: "running", url: "http://localhost:3000" }));
    renderPanel();

    fireEvent.click(await screen.findByRole("button", { name: /Run locally/ }));

    await waitFor(() => expect(open).toHaveBeenCalledWith("http://localhost:3000", "_blank", "noopener,noreferrer"));
    expect(open).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("link", { name: /localhost:3000/ })).toHaveAttribute("href", "http://localhost:3000");
  });

  it("does not open a preview it did not start", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    getLocalPreview.mockResolvedValue({ active: true, preview: makePreview({ status: "running", url: "http://localhost:3000" }) });
    renderPanel();

    expect(await screen.findByRole("link", { name: /localhost:3000/ })).toBeInTheDocument();
    expect(open).not.toHaveBeenCalled();
  });
});
