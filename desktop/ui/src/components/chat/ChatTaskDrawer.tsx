import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import {
  api,
  type Agent,
  type BoardTask,
  type InitiativeProject,
  type Repository,
  type WorkspaceConfig,
} from "@/api";
import { TaskDetailDrawer } from "@/components/board/TaskDetailDrawer";
import { useI18n } from "@/hooks/useI18n";

interface ChatTaskDrawerProps {
  /** Board task to show; null keeps the drawer closed. */
  taskId: string | null;
  onClose: () => void;
}

/**
 * Opens the board's own task drawer from a chat transcript.
 *
 * `TaskDetailDrawer` needs the whole board context (columns, members, agents,
 * projects, repositories) that `BoardPage` already has loaded. Chat does not,
 * so this fetches it on first open and keeps it — the alternative would be a
 * second, thinner task view that drifts from the board's.
 */
export function ChatTaskDrawer({ taskId, onClose }: ChatTaskDrawerProps) {
  const { t } = useI18n();
  const [task, setTask] = useState<BoardTask | null>(null);
  const [config, setConfig] = useState<WorkspaceConfig | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [projects, setProjects] = useState<InitiativeProject[]>([]);
  const [repositories, setRepositories] = useState<Repository[]>([]);

  const load = useCallback(async () => {
    if (!taskId) return;
    const [taskData, cfg, repoData, projectData, agentData] = await Promise.allSettled([
      api.listAllTasks(),
      api.getWorkspaceConfig(),
      api.listRepositories(),
      api.listInitiativeProjects(),
      api.listAgents(),
    ]);
    if (cfg.status === "fulfilled") setConfig(cfg.value);
    if (repoData.status === "fulfilled") setRepositories(repoData.value.repositories ?? []);
    if (projectData.status === "fulfilled") setProjects(projectData.value.projects ?? []);
    if (agentData.status === "fulfilled") setAgents(agentData.value.agents ?? []);
    if (taskData.status === "fulfilled") {
      const found = (taskData.value.tasks ?? []).find((item) => item.id === taskId) ?? null;
      setTask(found);
      if (!found) {
        // The action ledger outlives the task: a deleted task must say so
        // rather than open an empty drawer.
        toast.error(t("chatArea.chat.actions.taskGone"));
        onClose();
      }
      return;
    }
    toast.error(t("chatArea.chat.actions.taskLoadFailed"));
    onClose();
  }, [taskId, onClose, t]);

  useEffect(() => {
    if (!taskId) {
      setTask(null);
      return;
    }
    void load();
  }, [taskId, load]);

  if (!taskId || !task) return null;

  const enabledAgentIds = agents.filter((a) => a.enabled).map((a) => a.id);

  return (
    <TaskDetailDrawer
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      repositoryId={task.repository_id}
      task={task}
      columns={config?.columns ?? []}
      members={enabledAgentIds.map((id) => ({ agent_id: id }))}
      agents={agents}
      initiativeProjects={projects}
      repositories={repositories}
      onUpdated={load}
    />
  );
}
