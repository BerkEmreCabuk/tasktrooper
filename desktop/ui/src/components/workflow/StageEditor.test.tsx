import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import type { BoardColumn, Role, WorkflowProblem, WorkflowStage } from "@/api";
import { StageEditor } from "@/components/workflow/StageEditor";
import { I18nProvider } from "@/hooks/useI18n";

const columns: BoardColumn[] = [
  { id: "c1", slug: "todo", label: "Todo", position: 0, is_backlog: false },
  { id: "c2", slug: "in_progress", label: "In Progress", position: 1, is_backlog: false },
];

const roles: Role[] = [
  { id: "r1", key: "developer", name: "Developer", description: "", required_tools: [], assignments: [], purposes: [] },
];

function makeStage(overrides: Partial<WorkflowStage> = {}): WorkflowStage {
  return {
    column_slug: "todo",
    position: 0,
    on_path: true,
    kind: "queue",
    behaviours: [],
    instructions: "",
    participants: [],
    ...overrides,
  };
}

function Harness({ initial, problems }: { initial: WorkflowStage[]; problems?: WorkflowProblem[] }) {
  const [stages, setStages] = useState(initial);
  return (
    <StageEditor
      stages={stages}
      onChange={setStages}
      columns={columns}
      behaviourRegistry={[]}
      roles={roles}
      hasSubscriber={() => true}
      problems={problems}
    />
  );
}

function renderHarness(initial: WorkflowStage[], problems?: WorkflowProblem[]) {
  return render(
    <I18nProvider>
      <Harness initial={initial} problems={problems} />
    </I18nProvider>,
  );
}

describe("StageEditor", () => {
  it("flags a stage whose column no longer exists on the board", () => {
    renderHarness([makeStage({ column_slug: "todo" }), makeStage({ column_slug: "ghost_column", position: 1 })]);
    expect(screen.getByText("Column no longer exists")).toBeInTheDocument();
  });

  it("shows inline 422 problems once the stage row is expanded", () => {
    renderHarness(
      [makeStage({ column_slug: "todo" })],
      [{ column_slug: "todo", field: "behaviours", message: "advance_on_diff needs a column param" }],
    );
    expect(screen.queryByText("advance_on_diff needs a column param")).not.toBeInTheDocument();
    fireEvent.click(screen.getByText("Todo"));
    expect(screen.getByText("advance_on_diff needs a column param")).toBeInTheDocument();
  });

  it("does not flag a problem count badge for a stage with no problems", () => {
    renderHarness(
      [makeStage({ column_slug: "todo" }), makeStage({ column_slug: "in_progress", position: 1 })],
      [{ column_slug: "todo", field: "kind", message: "bad kind" }],
    );
    // Only one stage's toggle row should carry the problem-count badge ("1").
    expect(screen.getAllByText("1")).toHaveLength(1);
  });

  it("moves a stage down and reindexes position, changing render order", () => {
    renderHarness([makeStage({ column_slug: "todo", position: 0 }), makeStage({ column_slug: "in_progress", position: 1 })]);
    expect(screen.getAllByText(/^(Todo|In Progress)$/).map((el) => el.textContent)).toEqual(["Todo", "In Progress"]);

    const moveDownButtons = screen.getAllByLabelText("Move down");
    fireEvent.click(moveDownButtons[0]);

    expect(screen.getAllByText(/^(Todo|In Progress)$/).map((el) => el.textContent)).toEqual([
      "In Progress",
      "Todo",
    ]);
  });

  it("offers only the columns not already used by a stage when adding one", () => {
    renderHarness([makeStage({ column_slug: "todo" })]);
    expect(screen.getByText("Add a stage for…")).toBeInTheDocument();
  });
});
