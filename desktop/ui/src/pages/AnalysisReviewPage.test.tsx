import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { BoardTask, TaskAnnotation, TaskDocument } from "@/api";
import { I18nProvider } from "@/hooks/useI18n";
import { ThemeProvider } from "@/hooks/useTheme";
import { AnalysisReviewPage } from "@/pages/AnalysisReviewPage";

const { listRepositoryTasks, listTaskDocuments, listTaskAnnotations, submitTaskAnnotations, updateRepositoryTask } =
  vi.hoisted(() => ({
    listRepositoryTasks: vi.fn(),
    listTaskDocuments: vi.fn(),
    listTaskAnnotations: vi.fn(),
    submitTaskAnnotations: vi.fn(),
    updateRepositoryTask: vi.fn(),
  }));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listRepositoryTasks,
      listTaskDocuments,
      listTaskAnnotations,
      submitTaskAnnotations,
      updateRepositoryTask,
    },
  };
});

function makeTask(overrides: Partial<BoardTask> = {}): BoardTask {
  return {
    id: "task-1",
    repository_id: "repo-1",
    key: "A-30",
    task_number: 30,
    title: "Checkout caching",
    task_type: "analiz",
    description: "",
    technical_description: "",
    column: "analiz_review",
    position: 0,
    priority: "medium",
    created_by: "human",
    created_at: "2026-09-17T00:00:00Z",
    updated_at: "2026-09-17T00:00:00Z",
    ...overrides,
  };
}

function makeDoc(overrides: Partial<TaskDocument> = {}): TaskDocument {
  return {
    id: "doc-html",
    task_id: "task-1",
    title: "analiz: 2026-09-20 checkout caching",
    content: "<html><body><h1>Checkout caching</h1><p>Use the cache.</p></body></html>",
    format: "html",
    position: 1,
    created_by_type: "agent",
    created_by_id: "agent-1",
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    ...overrides,
  };
}

function makeAnnotation(overrides: Partial<TaskAnnotation> = {}): TaskAnnotation {
  return {
    id: "a1",
    task_id: "task-1",
    document_id: "doc-html",
    quote: "the cache",
    prefix: "Use ",
    suffix: ".",
    body: "Which cache?",
    status: "open",
    reply: "",
    created_by_type: "user",
    created_at: "2026-09-20T10:00:00Z",
    updated_at: "2026-09-20T10:00:00Z",
    ...overrides,
  };
}

function BoardProbe() {
  const location = useLocation();
  return <p>board at {location.pathname + location.search}</p>;
}

function renderPage(path = "/repositories/repo-1/tasks/task-1/analysis") {
  return render(
    <ThemeProvider>
      <I18nProvider>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/repositories/:repositoryId/tasks/:taskId/analysis" element={<AnalysisReviewPage />} />
            <Route path="/board" element={<BoardProbe />} />
          </Routes>
        </MemoryRouter>
      </I18nProvider>
    </ThemeProvider>,
  );
}

async function srcdoc(): Promise<string> {
  return waitFor(() => {
    const value = document.querySelector("iframe")?.getAttribute("srcdoc");
    if (!value) throw new Error("no frame yet");
    return value;
  });
}

describe("AnalysisReviewPage", () => {
  beforeEach(() => {
    listRepositoryTasks.mockReset().mockResolvedValue({ tasks: [makeTask()] });
    listTaskDocuments.mockReset().mockResolvedValue({
      documents: [
        makeDoc({ id: "doc-spec", title: "spec: old notes", content: "# Old spec", format: undefined, position: 0 }),
        makeDoc(),
      ],
    });
    listTaskAnnotations.mockReset().mockResolvedValue({
      annotations: [
        makeAnnotation(),
        makeAnnotation({ id: "a2", quote: "Checkout caching", body: "Scope?" }),
        makeAnnotation({ id: "a3", quote: "Use", status: "resolved", reply: "Clarified." }),
      ],
    });
    submitTaskAnnotations.mockReset();
    updateRepositoryTask.mockReset();
  });

  it("opens the analiz HTML document in the sandboxed frame with its comments", async () => {
    renderPage();

    expect(await screen.findByRole("heading", { name: "Checkout caching" })).toBeInTheDocument();
    expect(screen.getByText("A-30")).toBeInTheDocument();
    expect(await srcdoc()).toContain("<p>Use the cache.</p>");
    expect(document.querySelector("iframe")?.getAttribute("sandbox")).toBe("allow-scripts");
    expect(screen.getByText("2 open · 0 sent · 1 resolved")).toBeInTheDocument();
    expect(screen.getByText("Clarified.")).toBeInTheDocument();
    expect(listTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1");
  });

  it("opens the document the URL names", async () => {
    renderPage("/repositories/repo-1/tasks/task-1/analysis?doc=doc-spec");
    expect(await srcdoc()).toContain("<h1>Old spec</h1>");
  });

  it("sends every open comment with the note and goes back to the task on the board", async () => {
    submitTaskAnnotations.mockResolvedValue({ submitted: 2, task: makeTask({ column: "need_revision" }) });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Send comments (2)" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Note (optional)"), { target: { value: " Keep it short " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Send comments" }));

    await waitFor(() => expect(submitTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1", "Keep it short"));
    expect(await screen.findByText("board at /board?task=task-1")).toBeInTheDocument();
  });

  it("keeps the dialog open when the server refuses the submit", async () => {
    submitTaskAnnotations.mockRejectedValue(new Error("task is not in analiz_review"));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Send comments (2)" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Send comments" }));

    await waitFor(() => expect(submitTaskAnnotations).toHaveBeenCalledWith("repo-1", "task-1", undefined));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.queryByText(/board at/)).not.toBeInTheDocument();
  });

  it("asks before approving over unsent comments, then moves the task to done", async () => {
    updateRepositoryTask.mockResolvedValue(makeTask({ column: "done" }));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Approve" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Approve with unsent comments?")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Approve" }));

    await waitFor(() => expect(updateRepositoryTask).toHaveBeenCalledWith("repo-1", "task-1", { column: "done" }));
    expect(await screen.findByText("board at /board?task=task-1")).toBeInTheDocument();
  });

  it("shows the revision banner and holds the submit while the agent revises", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [makeTask({ column: "need_revision" })] });
    listTaskAnnotations.mockResolvedValue({ annotations: [makeAnnotation({ status: "submitted" })] });
    renderPage();

    expect(await screen.findByText("The agent is revising the analysis…")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Send comments (0)" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
    expect(screen.getByText(/Commenting is paused/)).toBeInTheDocument();
  });

  it("says so when the task no longer exists", async () => {
    listRepositoryTasks.mockResolvedValue({ tasks: [] });
    renderPage();
    expect(await screen.findByText("Task not found")).toBeInTheDocument();
  });
});
