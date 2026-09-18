import { useCallback, useEffect, useState } from "react";
import { api, type TaskTypeDef } from "@/api";
import { useCachedState } from "@/hooks/useCachedState";

export const CACHE_TASK_TYPES = "board.taskTypes";

/**
 * The task_types list (lib/project-board's TASK_TYPE_OPTIONS/taskTypeLabel
 * feed on it). Cached like the rest of the board payloads so a page that
 * needs it paints immediately from the last snapshot, then refreshes.
 *
 * A failed refresh keeps the last good list — CreateTaskDialog/TaskDetailDrawer
 * fall back to the four built-ins only on a genuinely empty/unloaded list, so
 * a transient network error must never blank the picker.
 */
export function useTaskTypes() {
  const [taskTypes, setTaskTypes] = useCachedState<TaskTypeDef[]>(CACHE_TASK_TYPES, []);
  const [loading, setLoading] = useState(taskTypes.length === 0);

  const reload = useCallback(async () => {
    try {
      const res = await api.listTaskTypes();
      setTaskTypes(res.task_types ?? []);
    } catch {
      // Keep the cached list; the caller decides whether to surface the error.
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  return { taskTypes, loading, reload };
}
