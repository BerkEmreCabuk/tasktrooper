import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { importGitHubRepository, createInitiativeProject, openRepository, createRepository, createNewRepository } =
  vi.hoisted(() => ({
    importGitHubRepository: vi.fn(),
    createInitiativeProject: vi.fn(),
    openRepository: vi.fn(),
    createRepository: vi.fn(),
    createNewRepository: vi.fn(),
  }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      importGitHubRepository,
      createInitiativeProject,
      openRepository,
      createRepository,
      createNewRepository,
    },
  };
});

import type { NewRepositoryInput } from "@/components/projects/add/flow-types";
import { DONE_STEP, useAddRepositoryFlow } from "@/components/projects/add/useAddRepositoryFlow";

const newRepoInput: NewRepositoryInput = {
  name: "acme-web",
  owner: "acme",
  description: "The web shop",
  role: "frontend",
  stack: "Next.js (TypeScript)",
  scaffold: true,
  docs: ["coding_standards", "local_run"],
};

const createdResult = {
  repository: { id: "repo-new", name: "acme-web", description: "", root_path: "/code/acme-web", created_at: "", updated_at: "" },
  component_id: "comp-1",
  task: { id: "task-1", key: "TT-12", title: "Set up acme-web" },
};

describe("useAddRepositoryFlow", () => {
  beforeEach(() => {
    importGitHubRepository.mockReset();
    createInitiativeProject.mockReset();
    openRepository.mockReset();
    createRepository.mockReset();
    createNewRepository.mockReset();
  });

  it("imports every selected GitHub repo once, in order, with project_ids and no description", async () => {
    importGitHubRepository.mockResolvedValueOnce({ id: "repo-1" }).mockResolvedValueOnce({ id: "repo-2" });

    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.startScan(
        { mode: "existing", projectId: "proj-1", projectName: "Acme" },
        {
          github: {
            owner: "acme",
            repos: [
              { name: "platform", cloneUrl: "https://example.com/acme/platform.git" },
              { name: "infra" },
            ],
          },
          folderPath: "",
        },
      );
    });

    await waitFor(() => expect(importGitHubRepository).toHaveBeenCalledTimes(2));

    expect(importGitHubRepository).toHaveBeenNthCalledWith(1, {
      owner: "acme",
      name: "platform",
      clone_url: "https://example.com/acme/platform.git",
      project_ids: ["proj-1"],
    });
    expect(importGitHubRepository).toHaveBeenNthCalledWith(2, {
      owner: "acme",
      name: "infra",
      clone_url: undefined,
      project_ids: ["proj-1"],
    });
    for (const call of importGitHubRepository.mock.calls) {
      expect(call[0]).not.toHaveProperty("description");
    }
    expect(createInitiativeProject).not.toHaveBeenCalled();

    expect(result.current.state.step).toBe(1);
    expect(result.current.state.repos.map((r) => r.label)).toEqual(["platform", "infra"]);
    await waitFor(() => expect(result.current.state.repos.every((r) => r.status === "ready")).toBe(true));
    expect(result.current.state.repos.map((r) => r.repositoryId)).toEqual(["repo-1", "repo-2"]);
  });

  it("creates a new project first, then imports into it", async () => {
    createInitiativeProject.mockResolvedValue({ id: "new-proj", name: "Acme Shop" });
    openRepository.mockResolvedValue({ id: "repo-folder" });

    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.startScan({ mode: "new", name: "Acme Shop" }, { github: null, folderPath: "/Users/me/code/acme" });
    });

    expect(createInitiativeProject).toHaveBeenCalledWith({ name: "Acme Shop" });
    await waitFor(() => expect(openRepository).toHaveBeenCalledWith("/Users/me/code/acme", undefined, ["new-proj"]));
    expect(result.current.state.projectId).toBe("new-proj");
    expect(result.current.state.projectName).toBe("Acme Shop");
  });

  it("retries only the failed repo, reusing its original recipe", async () => {
    openRepository.mockRejectedValueOnce(new Error("boom")).mockResolvedValueOnce({ id: "repo-folder" });

    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.startScan(
        { mode: "existing", projectId: "proj-1", projectName: "Acme" },
        { github: null, folderPath: "/Users/me/code/acme" },
      );
    });

    await waitFor(() => expect(result.current.state.repos[0].status).toBe("import_failed"));
    const localId = result.current.state.repos[0].localId;

    act(() => {
      result.current.retryImport(localId);
    });

    await waitFor(() => expect(result.current.state.repos[0].status).toBe("ready"));
    expect(openRepository).toHaveBeenCalledTimes(2);
    expect(openRepository).toHaveBeenNthCalledWith(2, "/Users/me/code/acme", undefined, ["proj-1"]);
    expect(createRepository).not.toHaveBeenCalled();
  });

  it("carries each row's scan outcome into Review", async () => {
    openRepository.mockResolvedValue({ id: "repo-folder" });
    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.startScan(
        { mode: "existing", projectId: "proj-1", projectName: "Acme" },
        { github: null, folderPath: "/Users/me/code/acme" },
      );
    });
    const localId = result.current.state.repos[0].localId;

    act(() => {
      result.current.continueToReview({ [localId]: "failed" });
    });

    expect(result.current.state.step).toBe(2);
    expect(result.current.state.repos[0].scanOutcome).toBe("failed");
  });

  it("creates a new repository with the exact body and skips Scan and Review", async () => {
    createNewRepository.mockResolvedValue(createdResult);
    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await result.current.createNewRepository({ mode: "existing", projectId: "proj-1", projectName: "Acme" }, newRepoInput);
    });

    expect(createNewRepository).toHaveBeenCalledTimes(1);
    expect(createNewRepository).toHaveBeenCalledWith({ ...newRepoInput, project_ids: ["proj-1"] });
    expect(createRepository).not.toHaveBeenCalled();
    expect(openRepository).not.toHaveBeenCalled();
    expect(result.current.state.step).toBe(DONE_STEP);
    expect(result.current.state.created).toEqual(createdResult);
    expect(result.current.state.repos).toEqual([]);
    expect(result.current.state.projectId).toBe("proj-1");
  });

  it("rethrows a failed create and reuses the project it already made on retry", async () => {
    createInitiativeProject.mockResolvedValue({ id: "new-proj", name: "Acme Shop" });
    createNewRepository.mockRejectedValueOnce(new Error("directory already exists")).mockResolvedValueOnce(createdResult);
    const { result } = renderHook(() => useAddRepositoryFlow());

    await act(async () => {
      await expect(result.current.createNewRepository({ mode: "new", name: "Acme Shop" }, newRepoInput)).rejects.toThrow(
        "directory already exists",
      );
    });
    expect(result.current.state.step).toBe(0);

    await act(async () => {
      await result.current.createNewRepository({ mode: "new", name: "Acme Shop" }, newRepoInput);
    });

    expect(createInitiativeProject).toHaveBeenCalledTimes(1);
    expect(createNewRepository).toHaveBeenNthCalledWith(2, { ...newRepoInput, project_ids: ["new-proj"] });
    expect(result.current.state.step).toBe(DONE_STEP);
  });
});
