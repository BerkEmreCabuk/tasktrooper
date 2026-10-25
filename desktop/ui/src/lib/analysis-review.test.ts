import { describe, expect, it } from "vitest";
import type { BoardTask, TaskAnnotation, TaskDocument, TaskQuestion } from "@/api";
import {
  analysisReviewPath,
  answeredUnsubmittedQuestions,
  annotationCounts,
  canSendAnswers,
  isQuestionAnswerEditable,
  isRevising,
  pendingBlockingQuestions,
  pickReviewDocument,
  visibleQuestions,
} from "@/lib/analysis-review";

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

function question(overrides: Partial<TaskQuestion> = {}): TaskQuestion {
  return {
    id: "q1",
    task_id: "task-1",
    key: "Q1",
    prompt: "Which cache?",
    kind: "technical",
    blocking: false,
    recommended_answer: "The checkout cache",
    status: "open",
    answer: "",
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    ...overrides,
  };
}

function boardTask(overrides: Partial<BoardTask> = {}): Pick<BoardTask, "column" | "blocked_resource"> {
  return { column: "analiz_review", blocked_resource: undefined, ...overrides };
}

describe("open questions helpers", () => {
  it("hides withdrawn questions", () => {
    const questions = [question(), question({ id: "q2", status: "withdrawn" })];
    expect(visibleQuestions(questions).map((q) => q.id)).toEqual(["q1"]);
  });

  it("finds pending-blocking questions: blocking and still open", () => {
    const questions = [
      question({ id: "q1", blocking: true, status: "open" }),
      question({ id: "q2", blocking: true, status: "answered" }),
      question({ id: "q3", blocking: false, status: "open" }),
    ];
    expect(pendingBlockingQuestions(questions).map((q) => q.id)).toEqual(["q1"]);
  });

  it("enables Send answers only once every blocking question is answered", () => {
    const unanswered = [question({ blocking: true, status: "open" })];
    const answered = [question({ blocking: true, status: "answered", answer: "Redis" })];
    const noBlocking = [question({ blocking: false, status: "open" })];
    expect(canSendAnswers(unanswered)).toBe(false);
    expect(canSendAnswers(answered)).toBe(true);
    expect(canSendAnswers(noBlocking)).toBe(true);
    expect(canSendAnswers([])).toBe(true);
  });

  it("finds answered questions not yet delivered to the agent", () => {
    const questions = [
      question({ id: "q1", status: "answered", answer: "Redis", submitted_at: null }),
      question({ id: "q2", status: "answered", answer: "Memcached", submitted_at: "2026-09-21T00:00:00Z" }),
      question({ id: "q3", status: "open" }),
    ];
    expect(answeredUnsubmittedQuestions(questions).map((q) => q.id)).toEqual(["q1"]);
  });

  it("makes the answer editable while blocked on analysis_questions or in analiz_review, never once withdrawn", () => {
    const blocked = boardTask({ column: "blocked", blocked_resource: "analysis_questions" });
    const otherBlock = boardTask({ column: "blocked", blocked_resource: "human_decision" });
    const review = boardTask({ column: "analiz_review" });
    const inProgress = boardTask({ column: "in_progress" });

    expect(isQuestionAnswerEditable(question(), blocked)).toBe(true);
    expect(isQuestionAnswerEditable(question(), otherBlock)).toBe(false);
    expect(isQuestionAnswerEditable(question(), review)).toBe(true);
    expect(isQuestionAnswerEditable(question(), inProgress)).toBe(false);
    expect(isQuestionAnswerEditable(question({ status: "withdrawn" }), review)).toBe(false);
  });
});
