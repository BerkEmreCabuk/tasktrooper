import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ProjectScan, ScanStatus } from "@/api";

const { getLatestRepositoryScan, getRepositoryModel } = vi.hoisted(() => ({
  getLatestRepositoryScan: vi.fn(),
  getRepositoryModel: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getLatestRepositoryScan, getRepositoryModel },
  };
});

import { ScanStep } from "@/components/projects/add/ScanStep";
import type { PendingRepo } from "@/components/projects/add/flow-types";
import { I18nProvider } from "@/hooks/useI18n";

function makeRepo(overrides: Partial<PendingRepo> = {}): PendingRepo {
  return {
    localId: "l1",
    label: "platform",
    recipe: { method: "github", owner: "acme", name: "platform" },
    status: "ready",
    repositoryId: "repo-1",
    ...overrides,
  };
}

function makeScan(status: ScanStatus): { scan: ProjectScan } {
  return {
    scan: {
      id: "scan-1",
      repository_id: "repo-1",
      trigger: "import",
      status,
      events: [],
      review_count: 0,
      started_at: "2026-01-01T00:00:00Z",
      finished_at: status === "succeeded" || status === "failed" ? "2026-01-01T00:00:05Z" : undefined,
    },
  };
}

function renderStep(repos: PendingRepo[], extra: { noScanTimeoutMs?: number; scanCapMs?: number } = {}) {
  const onContinue = vi.fn();
  render(
    <I18nProvider>
      <ScanStep repos={repos} onRetry={vi.fn()} onContinue={onContinue} {...extra} />
    </I18nProvider>,
  );
  return onContinue;
}

const continueButton = () => screen.getByRole("button", { name: /Continue to review/i });

describe("ScanStep", () => {
  beforeEach(() => {
    getLatestRepositoryScan.mockReset();
    getRepositoryModel.mockReset().mockResolvedValue({
      repository: { id: "repo-1", name: "platform", description: "", root_path: "/repos/platform", created_at: "", updated_at: "" },
      shape: "single",
      components: [],
      checks: [],
      links: [],
      incoming_links: [],
      resources: [],
      linked_components: [],
      environments: [],
      review: [],
    });
  });

  it("enables Continue once the polled scan comes back succeeded", async () => {
    getLatestRepositoryScan.mockResolvedValue(makeScan("succeeded"));
    const onContinue = renderStep([makeRepo()]);

    expect(continueButton()).toBeDisabled();
    await waitFor(() => expect(continueButton()).toBeEnabled());
    fireEvent.click(continueButton());
    expect(onContinue).toHaveBeenCalledWith({ l1: "succeeded" });
  });

  it("keeps Continue disabled while a repo is still importing", () => {
    renderStep([makeRepo({ status: "importing", repositoryId: undefined })]);
    expect(continueButton()).toBeDisabled();
  });

  it("treats a failed import as settled, not blocking Continue", async () => {
    renderStep([makeRepo({ status: "import_failed", repositoryId: undefined, error: "clone failed" })]);
    await waitFor(() => expect(continueButton()).toBeEnabled());
  });

  it("settles a failed scan and says the repo can go on without analysis", async () => {
    getLatestRepositoryScan.mockResolvedValue(makeScan("failed"));
    const onContinue = renderStep([makeRepo()]);

    expect(await screen.findByText(/Couldn't analyze — you can continue without analyzing this repository/)).toBeInTheDocument();
    await waitFor(() => expect(continueButton()).toBeEnabled());
    expect(screen.queryByRole("button", { name: "Continue without waiting" })).not.toBeInTheDocument();
    fireEvent.click(continueButton());
    expect(onContinue).toHaveBeenCalledWith({ l1: "failed" });
    expect(getRepositoryModel).not.toHaveBeenCalled();
  });

  it("lets the user continue without waiting while a scan still runs", async () => {
    getLatestRepositoryScan.mockResolvedValue(makeScan("running"));
    const onContinue = renderStep([makeRepo()]);

    await waitFor(() => expect(getLatestRepositoryScan).toHaveBeenCalled());
    expect(continueButton()).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Continue without waiting" }));
    expect(onContinue).toHaveBeenCalledWith({ l1: "pending" });
  });

  it("settles a row whose scan never appears", async () => {
    getLatestRepositoryScan.mockResolvedValue({ scan: null });
    const onContinue = renderStep([makeRepo()], { noScanTimeoutMs: 30 });

    expect(await screen.findByText(/Analysis didn't start/)).toBeInTheDocument();
    await waitFor(() => expect(continueButton()).toBeEnabled());
    const polls = getLatestRepositoryScan.mock.calls.length;
    fireEvent.click(continueButton());
    expect(onContinue).toHaveBeenCalledWith({ l1: "not_started" });
    await new Promise((resolve) => setTimeout(resolve, 1100));
    expect(getLatestRepositoryScan.mock.calls.length).toBe(polls);
  });

  it("settles a scan that runs past the client cap", async () => {
    getLatestRepositoryScan.mockResolvedValue(makeScan("running"));
    const onContinue = renderStep([makeRepo()], { scanCapMs: 30 });

    expect(await screen.findByText(/Analysis is taking a while/)).toBeInTheDocument();
    await waitFor(() => expect(continueButton()).toBeEnabled());
    fireEvent.click(continueButton());
    expect(onContinue).toHaveBeenCalledWith({ l1: "slow" });
  });
});
