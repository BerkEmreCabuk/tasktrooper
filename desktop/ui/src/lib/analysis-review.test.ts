import { describe, expect, it } from "vitest";
import type { TaskAnnotation, TaskDocument } from "@/api";
import { analysisReviewPath, annotationCounts, isRevising, pickReviewDocument } from "@/lib/analysis-review";

function doc(id: string, overrides: Partial<TaskDocument> = {}): TaskDocument {
  return {
    id,
    task_id: "task-1",
    title: id,
    content: "",
    position: 0,
    created_by_type: "agent",
    created_by_id: "agent-1",
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    ...overrides,
  };
}

describe("pickReviewDocument", () => {
  it("prefers the analiz HTML document over other HTML and markdown documents", () => {
    const picked = pickReviewDocument([
      doc("spec", { title: "spec: old", position: 0 }),
      doc("notes", { title: "notes", format: "html", updated_at: "2026-09-22T00:00:00Z" }),
      doc("analiz", { title: "analiz: 2026-09-20 checkout", format: "html" }),
    ]);
    expect(picked?.id).toBe("analiz");
  });

  it("falls back to the newest HTML document, then the first markdown one", () => {
    expect(
      pickReviewDocument([
        doc("old", { format: "html", updated_at: "2026-09-19T00:00:00Z" }),
        doc("new", { format: "html", updated_at: "2026-09-21T00:00:00Z" }),
      ])?.id,
    ).toBe("new");
    expect(pickReviewDocument([doc("plan", { position: 2 }), doc("spec", { position: 1 })])?.id).toBe("spec");
    expect(pickReviewDocument([])).toBeNull();
  });
});

describe("analysis review helpers", () => {
  it("builds the review path, with the document when named", () => {
    expect(analysisReviewPath("repo-1", "task-1")).toBe("/repositories/repo-1/tasks/task-1/analysis");
    expect(analysisReviewPath("repo-1", "task-1", "doc-1")).toBe(
      "/repositories/repo-1/tasks/task-1/analysis?doc=doc-1",
    );
  });

  it("treats need_revision and in_progress as the agent revising", () => {
    expect(isRevising("need_revision")).toBe(true);
    expect(isRevising("in_progress")).toBe(true);
    expect(isRevising("analiz_review")).toBe(false);
    expect(isRevising(undefined)).toBe(false);
  });

  it("counts annotations per status", () => {
    const statuses: TaskAnnotation["status"][] = ["open", "open", "submitted", "resolved"];
    expect(annotationCounts(statuses.map((status) => ({ status }) as TaskAnnotation))).toEqual({
      open: 2,
      submitted: 1,
      resolved: 1,
    });
  });
});
