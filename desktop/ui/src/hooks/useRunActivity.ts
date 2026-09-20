import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type OrchestrationPlan, type SessionStep } from "@/api";
import { getLiveStepSummary } from "@/lib/sessionGraph";
import { tStatic } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";

const TERMINAL_STEP_TYPES = new Set([
  "orchestration_complete",
  "verification_complete",
  "verification_failed",
  // A Claude Code run's own terminal steps. Without them a finished CLI run
  // whose row status we do not have (the board passes one, the chat page does
  // not) kept polling and kept the "Live" badge up forever, because its last
  // step is neither an assistant_message nor any of the loop's endings.
  "claude_code_result",
  "llm_provider_code_quota_park",
]);

// Statuses the run row itself reports as over. The step stream cannot be
// trusted alone: a run killed mid-flight (pod terminated, reconciler stale
// sweep) simply stops emitting steps, so its last step is an ordinary one and
// the graph showed "Live" with a spinning subtask forever.
const TERMINAL_RUN_STATUSES = new Set(["completed", "failed", "cancelled", "canceled", "error"]);

// Statuses that mean the run row itself is still in flight. The row is the
// authority when we have it: the orchestration plan settles as soon as the last
// subtask returns, but a board run keeps going after that — build/vet
// verification, up to N LLM fix rounds, the commit and push. Treating the
// settled plan as the end of the run stopped the poll mid-work and, because
// every still-running subtask is rewritten as "interrupted" once the run is not
// live, painted a working agent as failed.
const LIVE_RUN_STATUSES = new Set(["running", "pending", "queued", "in_progress"]);

function isRunComplete(steps: SessionStep[], plan: OrchestrationPlan | null): boolean {
  if (plan) {
    const status = plan.status.toLowerCase();
    if (status === "completed" || status === "failed") return true;
  }
  if (steps.length === 0) return false;
  const lastType = steps[steps.length - 1].step_type;
  if (TERMINAL_STEP_TYPES.has(lastType)) return true;
  if (!plan && lastType === "assistant_message") return true;
  return false;
}

export function useRunActivity(runId: string | null, enabled: boolean, runStatus?: string | null) {
  const [steps, setSteps] = useState<SessionStep[]>([]);
  const [plan, setPlan] = useState<OrchestrationPlan | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    if (!runId) return;
    try {
      const [stepsRes, planRes] = await Promise.all([
        api.runSteps(runId),
        api.getRunPlan(runId).catch(() => null),
      ]);
      setSteps(stepsRes.steps ?? []);
      setPlan(planRes);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : tStatic("chatArea.chat.activityPanel.runLoadFailed"));
    } finally {
      setLoading(false);
    }
  }, [runId]);

  const complete = useMemo(() => {
    const status = (runStatus ?? "").toLowerCase();
    if (TERMINAL_RUN_STATUSES.has(status)) return true;
    // A run row that says it is still running outranks anything the step stream
    // or the settled plan suggests — see LIVE_RUN_STATUSES.
    if (LIVE_RUN_STATUSES.has(status)) return false;
    return isRunComplete(steps, plan);
  }, [runStatus, steps, plan]);
  const isLive = enabled && !!runId && !complete;
  const liveSummary = getLiveStepSummary(steps, isLive);

  useEffect(() => {
    if (!runId || !enabled) {
      setSteps([]);
      setPlan(null);
      setError(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    void fetchData();
  }, [runId, enabled, fetchData]);

  usePolling(fetchData, 2000, isLive);

  return { steps, plan, liveSummary, isLive, loading, error };
}
