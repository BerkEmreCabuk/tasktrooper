import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Component } from "@/api";

const { getRepoDocsTask, updateComponent, createRepoDocsBundleTask } = vi.hoisted(() => ({
  getRepoDocsTask: vi.fn(),
  updateComponent: vi.fn(),
  createRepoDocsBundleTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getRepoDocsTask, updateComponent, createRepoDocsBundleTask },
  };
});

import { ComponentDocsCard } from "@/components/projects/repository/ComponentDocsCard";
import { I18nProvider } from "@/hooks/useI18n";

const component: Component = {
  id: "comp-1",
  repository_id: "repo-1",
  path: ".",
  name: {},
  role: { detected: "backend", confidence: "exact" },
  stack: {},
  commands: [],
  docs: {},
  gates: {},
  status: "active",
  manually_added: false,
  needs_review: false,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("ComponentDocsCard", () => {
  beforeEach(() => {
    getRepoDocsTask.mockReset().mockResolvedValue({ task_id: "", column: "" });
    updateComponent.mockReset().mockResolvedValue(component);
    createRepoDocsBundleTask.mockReset().mockResolvedValue({ task_id: "t1" });
  });

  it("defaults the local run doc to the server's scripts/dev.sh", async () => {
    render(
      <I18nProvider>
        <ComponentDocsCard repositoryId="repo-1" component={component} onReload={vi.fn()} />
      </I18nProvider>,
    );

    expect(screen.getByPlaceholderText("scripts/dev.sh")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText(".ai/local-deploy.md")).not.toBeInTheDocument();

    const queueButtons = screen.getAllByRole("button", { name: "Queue" });
    fireEvent.click(queueButtons[queueButtons.length - 1]);
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));

    await waitFor(() =>
      expect(createRepoDocsBundleTask).toHaveBeenCalledWith("repo-1", [{ kind: "local_run", component_id: "comp-1", path: undefined }]),
    );
    await waitFor(() => expect(screen.getByDisplayValue("scripts/dev.sh")).toBeInTheDocument());
  });
});
