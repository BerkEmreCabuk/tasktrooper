import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import type { Agent, RoleAssignment } from "@/api";
import { RoleAssignmentEditor } from "@/components/workflow/RoleAssignmentEditor";
import { I18nProvider } from "@/hooks/useI18n";

const agents: Agent[] = [
  {
    id: "a1",
    name: "Backend Bot",
    description: "",
    subagent_type: "",
    system_prompt: "",
    provider_type: "",
    model: "",
    model_heavy: "",
    tool_policy: {},
    skill_ids: [],
    enabled: true,
    self_evolution_enabled: false,
    created_at: "",
  },
  {
    id: "a2",
    name: "Frontend Bot",
    description: "",
    subagent_type: "",
    system_prompt: "",
    provider_type: "",
    model: "",
    model_heavy: "",
    tool_policy: {},
    skill_ids: [],
    enabled: true,
    self_evolution_enabled: false,
    created_at: "",
  },
];

let latest: RoleAssignment[] = [];

function Harness({ initial }: { initial: RoleAssignment[] }) {
  const [assignments, setAssignments] = useState(initial);
  latest = assignments;
  return (
    <RoleAssignmentEditor
      agents={agents}
      assignments={assignments}
      onChange={(next) => {
        setAssignments(next);
        latest = next;
      }}
    />
  );
}

function renderHarness(initial: RoleAssignment[]) {
  return render(
    <I18nProvider>
      <Harness initial={initial} />
    </I18nProvider>,
  );
}

describe("RoleAssignmentEditor", () => {
  it("defaults a new row's scope to any area, shown as a checked 'Any area' box", () => {
    renderHarness([{ agent_id: "a1", agent_name: "Backend Bot", areas: null, priority: 0 }]);
    const anyBox = screen.getByText("Any area").closest("label")!.querySelector("button");
    expect(anyBox).toHaveAttribute("data-state", "checked");
  });

  it("narrows an assignment to backend only when a specific area is checked", () => {
    renderHarness([{ agent_id: "a1", agent_name: "Backend Bot", areas: null, priority: 0 }]);
    const backendBox = screen.getByText("Backend").closest("label")!.querySelector("button")!;
    fireEvent.click(backendBox);
    expect(latest).toEqual([{ agent_id: "a1", agent_name: "Backend Bot", areas: ["backend"], priority: 0 }]);
  });

  it("returns to any-area once every specific area is unchecked", () => {
    renderHarness([{ agent_id: "a1", agent_name: "Backend Bot", areas: ["backend"], priority: 0 }]);
    const backendBox = screen.getByText("Backend").closest("label")!.querySelector("button")!;
    fireEvent.click(backendBox);
    expect(latest[0].areas).toBeNull();
  });

  it("updates priority from the number input", () => {
    renderHarness([{ agent_id: "a1", agent_name: "Backend Bot", areas: null, priority: 0 }]);
    const priorityInput = screen.getByDisplayValue("0");
    fireEvent.change(priorityInput, { target: { value: "5" } });
    expect(latest[0].priority).toBe(5);
  });

  it("removes an assignment row", () => {
    renderHarness([
      { agent_id: "a1", agent_name: "Backend Bot", areas: null, priority: 0 },
      { agent_id: "a2", agent_name: "Frontend Bot", areas: null, priority: 0 },
    ]);
    expect(screen.getAllByText(/Bot$/)).toHaveLength(2);
    const removeButtons = screen.getAllByTitle("Remove");
    fireEvent.click(removeButtons[0]);
    expect(latest).toEqual([{ agent_id: "a2", agent_name: "Frontend Bot", areas: null, priority: 0 }]);
  });

  it("shows the empty state and no rows when there are no assignments", () => {
    renderHarness([]);
    expect(screen.getByText("No agents assigned yet.")).toBeInTheDocument();
  });
});
