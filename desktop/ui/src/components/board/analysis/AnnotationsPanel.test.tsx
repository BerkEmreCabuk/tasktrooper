import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { TaskAnnotation } from "@/api";
import { AnnotationsPanel } from "@/components/board/analysis/AnnotationsPanel";
import { I18nProvider } from "@/hooks/useI18n";

const { createTaskAnnotation, updateTaskAnnotation, deleteTaskAnnotation } = vi.hoisted(() => ({
  createTaskAnnotation: vi.fn(),
  updateTaskAnnotation: vi.fn(),
  deleteTaskAnnotation: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return { ...actual, api: { ...actual.api, createTaskAnnotation, updateTaskAnnotation, deleteTaskAnnotation } };
});

function makeAnnotation(overrides: Partial<TaskAnnotation> = {}): TaskAnnotation {
  return {
    id: "a1",
    task_id: "task-1",
    document_id: "doc-1",
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

function renderPanel(overrides: Partial<Parameters<typeof AnnotationsPanel>[0]> = {}) {
  const props = {
    repositoryId: "repo-1",
    taskId: "task-1",
    documentId: "doc-1",
    annotations: [] as TaskAnnotation[],
    anchored: {},
    activeId: null,
    pending: null,
    canComment: true,
    onActivate: vi.fn(),
    onCancelPending: vi.fn(),
    onUpsert: vi.fn(),
    onRemove: vi.fn(),
    ...overrides,
  };
  render(
    <I18nProvider>
      <AnnotationsPanel {...props} />
    </I18nProvider>,
  );
  return props;
}

function card(quote: string): HTMLElement {
  return screen.getByText(quote).closest("[data-annotation-id]") as HTMLElement;
}

describe("AnnotationsPanel", () => {
  beforeEach(() => {
    createTaskAnnotation.mockReset();
    updateTaskAnnotation.mockReset();
    deleteTaskAnnotation.mockReset();
  });

  it("turns a selection into a comment on this document", async () => {
    const created = makeAnnotation({ id: "a9", body: "Name the cache" });
    createTaskAnnotation.mockResolvedValue(created);
    const props = renderPanel({ pending: { quote: "the cache", prefix: "Use ", suffix: "." } });

    const add = screen.getByRole("button", { name: "Add comment" });
    expect(add).toBeDisabled();
    fireEvent.change(screen.getByLabelText("New comment"), { target: { value: "  Name the cache " } });
    fireEvent.click(add);

    await waitFor(() =>
      expect(createTaskAnnotation).toHaveBeenCalledWith("repo-1", "task-1", "doc-1", {
        quote: "the cache",
        prefix: "Use ",
        suffix: ".",
        body: "Name the cache",
      }),
    );
    expect(props.onUpsert).toHaveBeenCalledWith(created);
    expect(props.onCancelPending).toHaveBeenCalled();
    expect(props.onActivate).toHaveBeenCalledWith("a9");
  });

  it("refuses a selection longer than the server accepts", () => {
    renderPanel({ pending: { quote: "x".repeat(2001), prefix: "", suffix: "" } });
    fireEvent.change(screen.getByLabelText("New comment"), { target: { value: "too long" } });

    expect(screen.getByText(/The selection is too long/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add comment" })).toBeDisabled();
  });

  it("shows each status, the agent's reply, the not-found hint and the counts", () => {
    renderPanel({
      annotations: [
        makeAnnotation({ id: "a1", quote: "first passage" }),
        makeAnnotation({ id: "a2", quote: "second passage", status: "submitted" }),
        makeAnnotation({ id: "a3", quote: "third passage", status: "resolved", reply: "Renamed it to RedisCache." }),
      ],
      anchored: { a1: true, a2: true, a3: false },
    });

    expect(screen.getByText("1 open · 1 sent · 1 resolved")).toBeInTheDocument();
    expect(within(card("first passage")).getByText("Open")).toBeInTheDocument();
    expect(within(card("second passage")).getByText("Sent")).toBeInTheDocument();
    expect(within(card("third passage")).getByText("Resolved")).toBeInTheDocument();
    expect(within(card("third passage")).getByText("Renamed it to RedisCache.")).toBeInTheDocument();
    expect(within(card("third passage")).getByText("Not found in this version")).toBeInTheDocument();
    expect(within(card("first passage")).queryByText("Not found in this version")).not.toBeInTheDocument();

    expect(within(card("first passage")).getByRole("button", { name: /Edit/ })).toBeInTheDocument();
    expect(within(card("second passage")).queryByRole("button", { name: /Edit|Delete|Reopen/ })).toBeNull();
    expect(within(card("third passage")).queryByRole("button", { name: /Edit|Delete/ })).toBeNull();
  });

  it("activates an annotation when its passage is clicked", () => {
    const props = renderPanel({ annotations: [makeAnnotation()] });
    fireEvent.click(screen.getByRole("button", { name: "Show in document" }));
    expect(props.onActivate).toHaveBeenCalledWith("a1");
  });

  it("edits an open comment", async () => {
    const original = makeAnnotation();
    const saved = makeAnnotation({ body: "Which cache, Redis or in-memory?", updated_at: "2026-09-20T11:00:00Z" });
    updateTaskAnnotation.mockResolvedValue(saved);
    const props = renderPanel({ annotations: [original] });

    fireEvent.click(screen.getByRole("button", { name: /Edit/ }));
    fireEvent.change(screen.getByLabelText("Comment"), { target: { value: "Which cache, Redis or in-memory?" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(updateTaskAnnotation).toHaveBeenCalledWith("repo-1", "task-1", "a1", {
        body: "Which cache, Redis or in-memory?",
      }),
    );
    expect(props.onUpsert).toHaveBeenCalledWith({ ...original, body: "Which cache, Redis or in-memory?" });
    expect(props.onUpsert).toHaveBeenLastCalledWith(saved);
  });

  it("puts the old comment back when an edit is refused", async () => {
    const original = makeAnnotation();
    updateTaskAnnotation.mockRejectedValue(new Error("conflict"));
    const props = renderPanel({ annotations: [original] });

    fireEvent.click(screen.getByRole("button", { name: /Edit/ }));
    fireEvent.change(screen.getByLabelText("Comment"), { target: { value: "changed" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(props.onUpsert).toHaveBeenLastCalledWith(original));
  });

  it("deletes an open comment after confirmation", async () => {
    deleteTaskAnnotation.mockResolvedValue(undefined);
    const props = renderPanel({ annotations: [makeAnnotation()] });

    fireEvent.click(screen.getByRole("button", { name: /Delete/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(deleteTaskAnnotation).toHaveBeenCalledWith("repo-1", "task-1", "a1"));
    expect(props.onRemove).toHaveBeenCalledWith("a1");
  });

  it("reopens a resolved comment", async () => {
    const resolved = makeAnnotation({ status: "resolved", reply: "Done." });
    const reopened = makeAnnotation({ status: "open", reply: "Done." });
    updateTaskAnnotation.mockResolvedValue(reopened);
    const props = renderPanel({ annotations: [resolved] });

    fireEvent.click(screen.getByRole("button", { name: /Reopen/ }));

    await waitFor(() =>
      expect(updateTaskAnnotation).toHaveBeenCalledWith("repo-1", "task-1", "a1", { status: "open" }),
    );
    expect(props.onUpsert).toHaveBeenLastCalledWith(reopened);
  });

  it("hides the composer while the agent revises", () => {
    renderPanel({ pending: { quote: "the cache", prefix: "", suffix: "" }, canComment: false });
    expect(screen.queryByLabelText("New comment")).not.toBeInTheDocument();
    expect(screen.getByText(/Commenting is paused/)).toBeInTheDocument();
  });
});
