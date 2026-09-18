import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, AgentSubscriptionsResponse, BoardColumn, TaskTypeDef } from "@/api";
import { AgentColumnsPage } from "@/pages/AgentColumnsPage";
import { I18nProvider } from "@/hooks/useI18n";

const {
  listBoardColumns,
  getAgentSubscriptions,
  setAgentSubscriptions,
  getAgent,
  listTaskTypes,
  listRoles,
  getAgentRoles,
} = vi.hoisted(() => ({
  listBoardColumns: vi.fn(),
  getAgentSubscriptions: vi.fn(),
  setAgentSubscriptions: vi.fn(),
  getAgent: vi.fn(),
  listTaskTypes: vi.fn(),
  listRoles: vi.fn(),
  getAgentRoles: vi.fn(),
}));

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  return {
    ...actual,
    api: {
      ...actual.api,
      listBoardColumns,
      getAgentSubscriptions,
      setAgentSubscriptions,
      getAgent,
      listTaskTypes,
      listRoles,
      getAgentRoles,
    },
  };
});

const columns: BoardColumn[] = [
  { id: "c0", slug: "backlog", label: "Backlog", position: 0, is_backlog: true },
  { id: "c1", slug: "todo", label: "Todo", position: 1, is_backlog: false },
  { id: "c2", slug: "in_progress", label: "In Progress", position: 2, is_backlog: false },
];

const taskTypes: TaskTypeDef[] = [
  {
    key: "task",
    label: "Task",
    key_prefix: "T",
    position: 0,
    is_default: true,
    is_defect: false,
    assignee_mode: "none",
    behaviours: [],
    built_in: true,
    task_count: 0,
  },
  {
    key: "bug",
    label: "Bug",
    key_prefix: "B",
    position: 1,
    is_default: false,
    is_defect: true,
    assignee_mode: "none",
    behaviours: [],
    built_in: true,
    task_count: 0,
  },
];

const agent: Agent = {
  id: "agent-1",
  name: "Quinn",
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
};

function renderPage() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={["/agents/agent-1/columns"]}>
        <Routes>
          <Route path="agents/:agentId/columns" element={<AgentColumnsPage />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe("AgentColumnsPage — task-type filter round trip", () => {
  beforeEach(() => {
    listBoardColumns.mockReset().mockResolvedValue({ columns });
    getAgent.mockReset().mockResolvedValue(agent);
    listTaskTypes.mockReset().mockResolvedValue({ task_types: taskTypes });
    listRoles.mockReset().mockResolvedValue({ roles: [] });
    getAgentRoles.mockReset().mockResolvedValue({ roles: [] });
    setAgentSubscriptions.mockReset().mockResolvedValue(undefined);
  });

  it("loads an existing filtered subscription and round-trips it unchanged", async () => {
    const existing: AgentSubscriptionsResponse = {
      column_slugs: ["todo"],
      subscriptions: [{ column_slug: "todo", task_types: ["bug"] }],
    };
    getAgentSubscriptions.mockReset().mockResolvedValue(existing);

    renderPage();

    await waitFor(() => expect(screen.getByText("Todo")).toBeInTheDocument());
    // The column is checked and its filter chip shows the previously saved type.
    expect(screen.getByText("Bug")).toBeInTheDocument();

    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);

    await waitFor(() => expect(setAgentSubscriptions).toHaveBeenCalledTimes(1));
    expect(setAgentSubscriptions).toHaveBeenCalledWith("agent-1", [{ column_slug: "todo", task_types: ["bug"] }]);
  });

  it("clearing every selected type falls back to 'every task type', sent as task_types: null", async () => {
    const existing: AgentSubscriptionsResponse = {
      column_slugs: ["todo"],
      subscriptions: [{ column_slug: "todo", task_types: ["bug"] }],
    };
    getAgentSubscriptions.mockReset().mockResolvedValue(existing);

    renderPage();
    await waitFor(() => expect(screen.getByText("Bug")).toBeInTheDocument());

    // Remove the "Bug" chip from the multi-select picker.
    fireEvent.click(screen.getByLabelText("Remove Bug"));

    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);

    await waitFor(() => expect(setAgentSubscriptions).toHaveBeenCalledTimes(1));
    expect(setAgentSubscriptions).toHaveBeenCalledWith("agent-1", [{ column_slug: "todo", task_types: null }]);
  });

  it("toggling a new column on subscribes it with every task type (null)", async () => {
    getAgentSubscriptions.mockReset().mockResolvedValue({ column_slugs: [], subscriptions: [] });

    renderPage();
    await waitFor(() => expect(screen.getByText("In Progress")).toBeInTheDocument());

    const row = screen.getByText("In Progress").closest("label")!;
    fireEvent.click(row.querySelector("button")!);

    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0]);

    await waitFor(() => expect(setAgentSubscriptions).toHaveBeenCalledTimes(1));
    expect(setAgentSubscriptions).toHaveBeenCalledWith("agent-1", [{ column_slug: "in_progress", task_types: null }]);
  });
});
