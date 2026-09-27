import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { CloudDeployment, EnvironmentSummary, ProjectsOverview, Release, RepositorySummary } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { DeploymentsPage } from "@/pages/DeploymentsPage";

const { getProjectsOverview, listAllReleases, getDeployMatrix, getEnvironmentDeployments } = vi.hoisted(() => ({
  getProjectsOverview: vi.fn(),
  listAllReleases: vi.fn(),
  getDeployMatrix: vi.fn(),
  getEnvironmentDeployments: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: { ...actual.api, getProjectsOverview, listAllReleases, getDeployMatrix, getEnvironmentDeployments },
  };
});

function env(id: string, componentId: string, url: string): EnvironmentSummary {
  return {
    id,
    component_id: componentId,
    environment: "production",
    provider: "vercel",
    url,
    status: "confirmed",
    health: "healthy",
    error_count_24h: 0,
  };
}

function repo(id: string, name: string, environment: EnvironmentSummary): RepositorySummary {
  return {
    id,
    name,
    description: "",
    shape: "single",
    components: [{ id: environment.component_id, path: ".", name, role: "frontend", stack_summary: "", checks: 0, required_checks: 0 }],
    review_count: 0,
    environments: [environment],
    updated_at: "2026-09-27T00:00:00Z",
  } as RepositorySummary;
}

const overview = {
  projects: [
    { id: "p-tt", name: "Task Trooper", repositories: [repo("repo-site", "tasktrooper-site", env("env-site", "comp-site", "https://www.tasktrooper.ai"))] },
    { id: "p-pishio", name: "Pishio", repositories: [repo("repo-pishio", "pishio-web", env("env-pishio", "comp-pishio", "https://www.pishio.app"))] },
  ],
  unassigned: [],
} as unknown as ProjectsOverview;

const siteRelease = {
  id: "rel-1",
  repository_id: "repo-site",
  component_id: "comp-site",
  version: "9bf8db5dd66e",
  mode: "on_merge",
  executor: "vercel",
  status: "released",
  commit_sha: "9bf8db5dd66e9e424f626b1fa88fdd6c146a85ed",
  profile: {},
  checks: {},
  tasks: [{ id: "t1", key: "TT-12", title: "Landing hero" }],
  created_at: "2026-09-27T01:41:13Z",
  updated_at: "2026-09-27T01:51:00Z",
  deploy_started_at: "2026-09-27T01:41:13Z",
  finished_at: "2026-09-27T01:51:38Z",
} as unknown as Release;

function deployment(overrides: Partial<CloudDeployment>): CloudDeployment {
  return { id: "d", status: "ready", environment: "production", created_at: "2026-09-26T10:00:00Z", ...overrides };
}

const providerDeploys: Record<string, CloudDeployment[]> = {
  "env-site": [
    deployment({ id: "site-1", commit_sha: siteRelease.commit_sha, commit_message: "feat: landing hero", created_at: "2026-09-27T01:41:20Z" }),
  ],
  "env-pishio": [
    deployment({ id: "pishio-2", commit_sha: "2222222bbbb", commit_message: "fix: checkout", status: "building", created_at: "2026-09-27T09:00:00Z" }),
    deployment({ id: "pishio-1", commit_sha: "1111111aaaa", commit_message: "feat: onboarding", created_at: "2026-09-26T09:00:00Z" }),
  ],
};

function renderPage() {
  return render(
    <MemoryRouter>
      <I18nProvider>
        <DeploymentsPage />
      </I18nProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  sessionStorage.clear();
  getProjectsOverview.mockResolvedValue(overview);
  listAllReleases.mockResolvedValue({ releases: [siteRelease] });
  getDeployMatrix.mockResolvedValue({ repos: [], envs: [] });
  getEnvironmentDeployments.mockImplementation(async (envId: string) => ({ deployments: providerDeploys[envId] ?? [] }));
});

describe("DeploymentsPage", () => {
  it("shows what each environment serves, what is deploying and the history across repositories", async () => {
    renderPage();

    const history = (await screen.findByText("Deploy history")).closest("div.rounded-xl") as HTMLElement;
    await waitFor(() => expect(within(history).getByText("feat: onboarding")).toBeInTheDocument());
    expect(within(history).getByText("Release 9bf8db5dd66e")).toBeInTheDocument();
    expect(within(history).getByText("TT-12")).toBeInTheDocument();
    expect(within(history).queryByText("fix: checkout")).not.toBeInTheDocument();

    const inProgress = screen.getByText("In progress").closest("div.rounded-xl") as HTMLElement;
    expect(within(inProgress).getByText("fix: checkout")).toBeInTheDocument();

    const siteCard = screen.getByText("tasktrooper-site", { selector: "p.font-semibold" }).closest("div.rounded-xl") as HTMLElement;
    expect(within(siteCard).getByText(/release 9bf8db5dd66e/)).toBeInTheDocument();
    expect(within(siteCard).getByText("feat: landing hero")).toBeInTheDocument();
    const pishioCard = screen.getByText("pishio-web", { selector: "p.font-semibold" }).closest("div.rounded-xl") as HTMLElement;
    expect(within(pishioCard).getByText("feat: onboarding")).toBeInTheDocument();
    expect(within(pishioCard).getByText("A new deploy is in progress")).toBeInTheDocument();
    expect(screen.queryByText("Manual deploy targets")).not.toBeInTheDocument();
  });

  it("narrows everything to one repository from its environment card", async () => {
    renderPage();
    const history = (await screen.findByText("Deploy history")).closest("div.rounded-xl") as HTMLElement;
    await waitFor(() => expect(within(history).getByText("feat: onboarding")).toBeInTheDocument());

    const pishioCard = screen.getByText("pishio-web", { selector: "p.font-semibold" }).closest("div.rounded-xl") as HTMLElement;
    fireEvent.click(within(pishioCard).getByRole("button", { name: "Show history" }));

    await waitFor(() => expect(within(history).queryByText("Release 9bf8db5dd66e")).not.toBeInTheDocument());
    expect(within(history).getByText("feat: onboarding")).toBeInTheDocument();
    expect(screen.queryByText("tasktrooper-site", { selector: "p.font-semibold" })).not.toBeInTheDocument();
  });

  it("asks for every repository's releases, not one repository's", async () => {
    renderPage();
    await screen.findByText("Deploy history");
    expect(listAllReleases).toHaveBeenCalledWith({ limit: 100 });
  });
});
