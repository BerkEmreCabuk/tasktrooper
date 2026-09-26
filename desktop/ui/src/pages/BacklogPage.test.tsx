import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardColumn, BoardTask, InitiativeProject, Repository, WorkspaceConfig } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { PROJECT_SCOPE_STORAGE_KEY } from "@/hooks/useProjectScope";
import { CACHE_AGENTS, CACHE_CONFIG, CACHE_PROJECTS, CACHE_REPOS, CACHE_TASKS } from "@/lib/project-board";
import { writeCache } from "@/lib/uiCache";
import { BacklogPage } from "@/pages/BacklogPage";

// jsdom has no layout, so Radix Select's scroll-into-view-on-open crashes without it.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {};
}

const api = vi.hoisted(() => ({
  listAllTasks: vi.fn(),
  getWorkspaceConfig: vi.fn(),
  listRepositories: vi.fn(),
  listInitiativeProjects: vi.fn(),
  listAgents: vi.fn(),
  listTaskTypes: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, ...api } };
});

const stamp = "2026-01-01T00:00:00Z";

const projects: InitiativeProject[] = [
  { id: "proj-shop", name: "Shop", description: "", created_at: stamp, updated_at: stamp },
  { id: "proj-ops", name: "Ops", description: "", created_at: stamp, updated_at: stamp },
];

const repo = (id: string, name: string, project_ids: string[]): Repository => ({
  id,
  name,
  description: "",
  root_path: `/repos/${name}`,
  project_ids,
  created_at: stamp,
  updated_at: stamp,
});

const repositories = [
  repo("repo-shop", "shop-web", ["proj-shop"]),
  repo("repo-ops", "ops-infra", ["proj-ops"]),
  repo("repo-loose", "scratch", []),
];

const column = (slug: string, position: number, is_backlog = false): BoardColumn => ({
  id: slug,
  slug,
  label: slug,
  position,
  is_backlog,
});

const config = { columns: [column("backlog", 0, true), column("todo", 1)] } as WorkspaceConfig;

const task = (id: string, title: string, repository_id: string, extra: Partial<BoardTask> = {}): BoardTask => ({
  id,
  key: id.toUpperCase(),
  title,
  repository_id,
  task_number: 1,
  task_type: "task",
  description: "",
  technical_description: "",
  column: "backlog",
  position: 0,
  priority: "medium",
  created_by: "me",
  created_at: stamp,
  updated_at: stamp,
  ...extra,
});

const tasks = [
  task("b-1", "Wishlist", "repo-shop"),
  task("b-2", "Tidy scripts", "repo-loose"),
  task("b-3", "Promo copy", "repo-loose", { initiative_project_id: "proj-shop" }),
  task("t-1", "Rotate keys", "repo-ops", { column: "todo" }),
];

let location = "";
function LocationProbe() {
  const current = useLocation();
  location = current.pathname + current.search;
  return null;
}

function renderBacklog(url: string) {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/backlog" element={<BacklogPage />} />
        </Routes>
        <LocationProbe />
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("BacklogPage project scope", () => {
  beforeEach(() => {
    window.localStorage.clear();
    writeCache(CACHE_TASKS, tasks);
    writeCache(CACHE_REPOS, repositories);
    writeCache(CACHE_PROJECTS, projects);
    writeCache(CACHE_CONFIG, config);
    writeCache(CACHE_AGENTS, []);
    api.listAllTasks.mockReset().mockResolvedValue({ tasks });
    api.getWorkspaceConfig.mockReset().mockResolvedValue(config);
    api.listRepositories.mockReset().mockResolvedValue({ repositories });
    api.listInitiativeProjects.mockReset().mockResolvedValue({ projects });
    api.listAgents.mockReset().mockResolvedValue({ agents: [] });
    api.listTaskTypes.mockReset().mockResolvedValue({ task_types: [] });
  });

  it("carries the scope picked on the board and narrows by task or repository project", async () => {
    window.localStorage.setItem(PROJECT_SCOPE_STORAGE_KEY, "proj-shop");
    renderBacklog("/backlog");

    expect(await screen.findByText("Wishlist")).toBeInTheDocument();
    expect(screen.getByText("Promo copy")).toBeInTheDocument();
    expect(screen.queryByText("Tidy scripts")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("combobox", { name: "Project" }));
    expect(within(await screen.findByRole("option", { name: /Shop/ })).getByText("2")).toBeInTheDocument();
    expect(within(screen.getByRole("option", { name: /Ops/ })).getByText("0")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("option", { name: /No project/ }));

    expect(await screen.findByText("Tidy scripts")).toBeInTheDocument();
    expect(screen.queryByText("Wishlist")).not.toBeInTheDocument();
    expect(location).toBe("/backlog?project=none");
    expect(window.localStorage.getItem(PROJECT_SCOPE_STORAGE_KEY)).toBe("none");
  });

  it("shows the picker even before any project exists", async () => {
    writeCache(CACHE_PROJECTS, []);
    api.listInitiativeProjects.mockResolvedValue({ projects: [] });
    renderBacklog("/backlog");

    expect(await screen.findByRole("combobox", { name: "Project" })).toHaveTextContent("All projects");
    expect(screen.getByText("Tidy scripts")).toBeInTheDocument();
    expect(screen.getByText("Wishlist")).toBeInTheDocument();
  });

  it("says the project has nothing yet, and still offers to add a task", async () => {
    renderBacklog("/backlog?project=proj-ops");

    expect(await screen.findByText("No tasks in this project yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add Task" })).toBeInTheDocument();
    expect(screen.queryByText("Wishlist")).not.toBeInTheDocument();
  });
});
