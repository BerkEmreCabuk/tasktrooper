import type { PlanTask } from "@/api";
import { tStatic } from "@/hooks/useI18n";

export function groupTasksByWaves(tasks: PlanTask[]): PlanTask[][] {
  const keyOf = (t: PlanTask) => t.task_key || t.id;
  const keys = new Set(tasks.map(keyOf));
  const assigned = new Set<string>();
  const remaining = [...tasks];
  const waves: PlanTask[][] = [];

  while (remaining.length > 0) {
    const wave = remaining.filter((t) =>
      (t.depends_on ?? []).every((dep) => assigned.has(dep) || !keys.has(dep)),
    );
    if (wave.length === 0) {
      waves.push([...remaining]);
      break;
    }
    waves.push(wave);
    wave.forEach((t) => assigned.add(keyOf(t)));
    const waveSet = new Set(wave);
    remaining.splice(0, remaining.length, ...remaining.filter((t) => !waveSet.has(t)));
  }

  return waves;
}

// Shared by subtask statuses, plan statuses and run statuses — they use the same
// vocabulary and must not be coloured differently for it.
export function taskStatusVariant(status: string): "default" | "success" | "warning" | "destructive" | "secondary" {
  const normalized = status.toLowerCase();
  if (normalized === "running" || normalized === "in_progress") return "warning";
  if (normalized === "completed" || normalized === "done" || normalized === "success") return "success";
  if (normalized === "failed" || normalized === "error") return "destructive";
  // "incomplete" at either level means the same thing: it finished, and nothing
  // confirmed it. A subtask that produced output without doing what it was for;
  // a plan that ran to the end with the verifier still objecting. Neither is an
  // error — a red cross claims the run broke when it did not — and neither is a
  // success, so it gets the warning colour of its own.
  if (normalized === "incomplete") return "warning";
  return "secondary";
}

// planStatusLabel turns a plan status into localised text. The badge used to
// print the raw wire value, which is survivable for "failed" and unreadable for
// "incomplete" — the one status whose whole point is explaining itself. Unknown
// values fall back to the raw string rather than a missing-key placeholder, so a
// status added on the backend before the web catches up still renders.
export function planStatusLabel(status: string): string {
  const normalized = status.toLowerCase();
  const key = `chatArea.chat.plan.status.${normalized}`;
  const label = tStatic(key);
  return label === key ? status : label;
}

export function stepLabel(type: string): string {
  const key = `lib.plan.step.${type}`;
  const label = tStatic(key);
  return label === key ? type : label;
}
