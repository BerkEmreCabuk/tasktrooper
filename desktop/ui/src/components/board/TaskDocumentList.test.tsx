import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { TaskDocument } from "@/api";
import { TaskDocumentList } from "@/components/board/TaskDocumentList";
import { I18nProvider } from "@/hooks/useI18n";

function makeDoc(overrides: Partial<TaskDocument> = {}): TaskDocument {
  return {
    id: "doc-1",
    task_id: "task-1",
    title: "spec: checkout",
    content: "# Checkout\n\nCache the **cart**.",
    position: 0,
    created_by_type: "user",
    created_by_id: "",
    created_at: "2026-09-20T00:00:00Z",
    updated_at: "2026-09-20T00:00:00Z",
    ...overrides,
  };
}

const htmlDoc = makeDoc({
  id: "doc-2",
  title: "analiz: 2026-09-20 checkout",
  format: "html",
  content: "<html><head><style>h1{color:red}</style></head><body><h1>Checkout</h1><p>Cache the cart.</p></body></html>",
});

function renderList(documents: TaskDocument[], onOpenReview = vi.fn()) {
  render(
    <I18nProvider>
      <TaskDocumentList documents={documents} agentNameMap={{}} onDelete={vi.fn()} onOpenReview={onOpenReview} />
    </I18nProvider>,
  );
  return onOpenReview;
}

describe("TaskDocumentList", () => {
  it("sends an HTML document to the review page instead of the reader dialog", () => {
    const onOpenReview = renderList([htmlDoc]);

    expect(screen.getByText("HTML")).toBeInTheDocument();
    expect(screen.getByText("Checkout Cache the cart.")).toBeInTheDocument();
    fireEvent.click(screen.getByText("analiz: 2026-09-20 checkout"));

    expect(onOpenReview).toHaveBeenCalledWith(htmlDoc);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("keeps opening a markdown document in the reader dialog", async () => {
    const onOpenReview = renderList([makeDoc()]);
    fireEvent.click(screen.getByText("spec: checkout"));

    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Cache the cart.");
    expect(onOpenReview).not.toHaveBeenCalled();
  });
});
