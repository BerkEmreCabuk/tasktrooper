import { useCallback, useEffect, useState } from "react";
import { api, type Session } from "@/api";

export function useAgentSessions(agentId: string | undefined) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(true);
  // Distinguishes "the list is genuinely empty" from "the request failed".
  // Callers navigate away when the open session is absent from the list, so a
  // failed load degraded to [] used to eject the user from a live conversation.
  const [failed, setFailed] = useState(false);

  const refresh = useCallback(async () => {
    if (!agentId) {
      setSessions([]);
      return [];
    }
    const data = await api.listAgentSessions(agentId);
    setSessions(data.sessions ?? []);
    setFailed(false);
    return data.sessions ?? [];
  }, [agentId]);

  useEffect(() => {
    setLoading(true);
    refresh()
      .catch(() => setFailed(true))
      .finally(() => setLoading(false));
  }, [refresh]);

  const createSession = useCallback(
    async (title = "New chat", projectId?: string) => {
      if (!agentId) throw new Error("agent required");
      const session = await api.createSession({
        title,
        agent_id: agentId,
        ...(projectId ? { project_id: projectId } : {}),
      });
      await refresh();
      return session;
    },
    [agentId, refresh],
  );

  const deleteSession = useCallback(
    async (id: string) => {
      await api.deleteSession(id);
      await refresh();
    },
    [refresh],
  );

  return { sessions, loading, failed, refresh, createSession, deleteSession };
}
