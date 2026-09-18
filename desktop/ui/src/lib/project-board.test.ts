import { describe, expect, it } from "vitest";
import type { TaskTypeDef } from "@/api";
import {
  pipelineStatusVariant,
  runStatusVariant,
  taskPipelineCardIcon,
  taskTypeLabel,
  taskTypeOptions,
  workOrderBlockerLabel,
} from "@/lib/project-board";

describe("workOrderBlockerLabel", () => {
  it("shows the single blocker's task key", () => {
    expect(workOrderBlockerLabel("waiting for T-12 (API migration) [in_progress] to finish")).toBe("T-12");
  });

  it("shows the first key with a +N count for multiple blockers", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-12 (API migration) [in_progress], T-15 (schema) [code_review] to finish",
      ),
    ).toBe("T-12 +1");
  });

  it("falls back to the generic label when blocked_question is empty", () => {
    expect(workOrderBlockerLabel("")).toBe("Waiting for blocking tasks");
  });

  it("does not pick up a key-shaped token from the blocker's title", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-8 (Fix: schema drift (T-9 ile ilişkili)) [in_progress] to finish",
      ),
    ).toBe("T-8");
  });

  it("does not miscount blockers when a title contains a key-shaped token", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-8 (Fix: schema drift (T-9 ile ilişkili)) [in_progress], T-15 (schema) [code_review] to finish",
      ),
    ).toBe("T-8 +1");
  });

  it("does not miscount a single blocker whose title contains a literal comma", () => {
    expect(
      workOrderBlockerLabel("waiting for T-3 (Refactor, cleanup) [todo] to finish"),
    ).toBe("T-3");
  });

  it("splits blockers correctly when an earlier title contains a literal comma", () => {
    expect(
      workOrderBlockerLabel(
        "waiting for T-3 (Refactor, cleanup) [todo], T-15 (schema) [code_review] to finish",
      ),
    ).toBe("T-3 +1");
  });
});

describe("pipelineStatusVariant", () => {
  it("renders running and pending as the info variant, distinct from a primary action", () => {
    expect(pipelineStatusVariant("running")).toBe("info");
    expect(pipelineStatusVariant("pending")).toBe("info");
  });

  it("still renders success/failed/skipped in their existing variants", () => {
    expect(pipelineStatusVariant("success")).toBe("success");
    expect(pipelineStatusVariant("failed")).toBe("destructive");
    expect(pipelineStatusVariant("skipped")).toBe("secondary");
  });
});

describe("runStatusVariant", () => {
  it("renders running and pending as the info variant, not warning, matching pipelineStatusVariant", () => {
    expect(runStatusVariant("running")).toBe("info");
    expect(runStatusVariant("pending")).toBe("info");
  });

  it("renders completed as success, failed as destructive and cancelled as secondary", () => {
    expect(runStatusVariant("completed")).toBe("success");
    expect(runStatusVariant("failed")).toBe("destructive");
    expect(runStatusVariant("cancelled")).toBe("secondary");
  });

  it("falls back to secondary for an unknown status", () => {
    expect(runStatusVariant("unknown")).toBe("secondary");
  });
});

describe("taskTypeOptions / taskTypeLabel", () => {
  it("falls back to the four built-ins when no task_types list has loaded", () => {
    expect(taskTypeOptions(undefined).map((o) => o.value)).toEqual(["task", "analiz", "bug", "technical"]);
    expect(taskTypeOptions([]).map((o) => o.value)).toEqual(["task", "analiz", "bug", "technical"]);
  });

  it("labels a built-in type from the locale when no list is loaded", () => {
    expect(taskTypeLabel("technical")).toBe("Technical");
  });

  it("orders options by the server list's position and prefers its labels", () => {
    const types: TaskTypeDef[] = [
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
        key: "spike",
        label: "Spike",
        key_prefix: "SP",
        position: 2,
        is_default: false,
        is_defect: false,
        assignee_mode: "none",
        behaviours: [],
        built_in: false,
        task_count: 0,
      },
    ];
    expect(taskTypeOptions(types).map((o) => o.value)).toEqual(["task", "bug", "spike"]);
    expect(taskTypeOptions(types).map((o) => o.label)).toEqual(["Task", "Bug", "Spike"]);
  });

  it("prefers the server's custom label over the locale once a built-in type is renamed", () => {
    const types: TaskTypeDef[] = [
      {
        key: "technical",
        label: "Ops",
        key_prefix: "TC",
        position: 3,
        is_default: false,
        is_defect: false,
        assignee_mode: "none",
        behaviours: [],
        built_in: true,
        task_count: 0,
      },
    ];
    expect(taskTypeLabel("technical", types)).toBe("Ops");
  });

  it("translates a built-in type whose server label is still the untouched English default", () => {
    const types: TaskTypeDef[] = [
      {
        key: "technical",
        label: "Technical",
        key_prefix: "TC",
        position: 3,
        is_default: false,
        is_defect: false,
        assignee_mode: "none",
        behaviours: [],
        built_in: true,
        task_count: 0,
      },
    ];
    expect(taskTypeLabel("technical", types)).toBe("Technical");
  });
});

describe("taskPipelineCardIcon", () => {
  it("colors the running/pending board-card icon with the info hue, not warning", () => {
    expect(taskPipelineCardIcon("running")?.className).toBe("text-info animate-spin");
    expect(taskPipelineCardIcon("pending")?.className).toBe("text-info animate-spin");
  });
});
