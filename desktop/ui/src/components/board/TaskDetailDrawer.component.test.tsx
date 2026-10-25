import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask, Component, RepositoryModel } from "@/api";
import { TaskDetailDrawer } from "@/components/board/TaskDetailDrawer";
import { I18nProvider } from "@/hooks/useI18n";

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const calls = vi.hoisted(() => ({
  getRepositoryModel: vi.fn(),
  updateRepositoryTask: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  const pending = new Proxy(actual.api, {
    get: (target, key: string) =>
      key in calls ? calls[key as keyof typeof calls] : typeof (target as Record<string, unknown>)[key] === "function"
        ? () => new Promise(() => {})
        : (target as Record<string, unknown>)[key],
  });
  return { ...actual, api: pending };
});

const stamp = "2026-01-01T00:00:00Z";
const mergedTask: BoardTask = {
  id: "t-1",
  key: "SHOP-1",
  title: "Update the docs",
  repository_id: "repo-1",
  task_number: 1,
  task_type: "task",
  description: "",
  technical_description: "",
  column: "done",
  position: 0,
  priority: "medium",
  created_by: "me",
  created_at: stamp,
  updated_at: stamp,
};

function component(id: string, path: string, status: Component["status"] = "active"): Component {
  return { id, repository_id: "repo-1", path, status, name: {} } as unknown as Component;
}

function model(components: Component[]): RepositoryModel {
  return { repository: { id: "repo-1", name: "shop" }, components } as unknown as RepositoryModel;
}

function renderDrawer(task: BoardTask) {
  return render(
    <I18nProvider>
      <MemoryRouter>
        <TaskDetailDrawer
          open
          onOpenChange={() => {}}
          repositoryId="repo-1"
          task={task}
          columns={[]}
          members={[]}
          agents={[]}
          initiativeProjects={[]}
          repositories={[]}
          onUpdated={() => {}}
        />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("TaskDetailDrawer component field", () => {
  beforeEach(() => {
    localStorage.clear();
    calls.getRepositoryModel.mockReset();
    calls.updateRepositoryTask.mockReset();
    calls.updateRepositoryTask.mockResolvedValue(mergedTask);
  });

  it("lets a person name the component of a task that has none", async () => {
    calls.getRepositoryModel.mockResolvedValue(
      model([component("c-api", "services/api"), component("c-web", "apps/web"), component("c-old", "old", "dismissed")]),
    );
    renderDrawer(mergedTask);

    fireEvent.click(await screen.findByRole("combobox", { name: "Component" }));
    expect(screen.queryByRole("option", { name: "old" })).not.toBeInTheDocument();
    fireEvent.click(await screen.findByRole("option", { name: "api" }));

    await waitFor(() =>
      expect(calls.updateRepositoryTask).toHaveBeenCalledWith("repo-1", "t-1", { component_id: "c-api" }),
    );
  });

  it("clears the component with None", async () => {
    calls.getRepositoryModel.mockResolvedValue(model([component("c-api", "services/api"), component("c-web", "apps/web")]));
    renderDrawer({ ...mergedTask, component_id: "c-web" });

    const field = await screen.findByRole("combobox", { name: "Component" });
    expect(field).toHaveTextContent("web");
    fireEvent.click(field);
    fireEvent.click(await screen.findByRole("option", { name: "None" }));

    await waitFor(() =>
      expect(calls.updateRepositoryTask).toHaveBeenCalledWith("repo-1", "t-1", { component_id: null }),
    );
  });

  it("shows no component field for a repository without a model", async () => {
    calls.getRepositoryModel.mockResolvedValue(model([]));
    renderDrawer(mergedTask);

    await waitFor(() => expect(calls.getRepositoryModel).toHaveBeenCalled());
    expect(screen.queryByRole("combobox", { name: "Component" })).not.toBeInTheDocument();
  });
});
