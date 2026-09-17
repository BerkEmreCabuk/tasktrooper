import { Archive, ArrowLeft, LayoutGrid, Search } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
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
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { useCachedState } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";
import {
  CACHE_AGENTS,
  CACHE_CONFIG,
  CACHE_PROJECTS,
  CACHE_REPOS,
  taskPriorityLabel,
  taskTypeLabel,
} from "@/lib/project-board";
import { formatRelativeDate } from "@/lib/utils";

// The archive is a search box over work that is finished: typing is how it is
// used, so the query goes to the server on a debounce rather than on a button.
const SEARCH_DEBOUNCE_MS = 300;

export function ReleasedPage() {
  const { t } = useI18n();
  const [tasks, setTasks] = useState<BoardTask[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);

  // The drawer needs the same context the board gives it. Read from the shared
  // cache first so opening a released card does not wait on four more requests.
  const [config, setConfig] = useCachedState<WorkspaceConfig | null>(CACHE_CONFIG, null);
  const [repositories, setRepositories] = useCachedState<Repository[]>(CACHE_REPOS, []);
  const [agents, setAgents] = useCachedState<Agent[]>(CACHE_AGENTS, []);
  const [initiativeProjects, setInitiativeProjects] = useCachedState<InitiativeProject[]>(
    CACHE_PROJECTS,
    [],
  );

  const search = useCallback(
    async (term: string) => {
      try {
        const data = await api.listReleasedTasks(term);
        setTasks(data.tasks ?? []);
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("boardArea.released.loadFailed"));
      } finally {
        setLoading(false);
      }
    },
    [t],
  );

  useEffect(() => {
    const id = setTimeout(() => void search(query), query ? SEARCH_DEBOUNCE_MS : 0);
    return () => clearTimeout(id);
  }, [query, search]);

  // Loaded once, and only what the drawer needs; the list itself comes from the
  // archive endpoint above.
  useEffect(() => {
    void (async () => {
      const [cfg, repos, agentData, projects] = await Promise.allSettled([
        api.getWorkspaceConfig(),
        api.listRepositories(),
        api.listAgents(),
        api.listInitiativeProjects(),
      ]);
      if (cfg.status === "fulfilled") setConfig(cfg.value);
      if (repos.status === "fulfilled") setRepositories(repos.value.repositories ?? []);
      if (agentData.status === "fulfilled") setAgents(agentData.value.agents ?? []);
      if (projects.status === "fulfilled") setInitiativeProjects(projects.value.projects ?? []);
    })();
  }, [setConfig, setRepositories, setAgents, setInitiativeProjects]);

  const repositoryName = useCallback(
    (id: string) =>
      repositories.find((r) => r.id === id)?.name ?? t("boardArea.released.repoFallback"),
    [repositories, t],
  );

  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === selectedId) ?? null,
    [tasks, selectedId],
  );

  const memberList = useMemo(
    () => agents.filter((a) => a.enabled).map((a) => ({ agent_id: a.id })),
    [agents],
  );

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <div className="shrink-0 border-b border-border px-6 py-4">
        <PageHeader
          title={t("boardArea.released.title")}
          description={t("boardArea.released.description")}
          action={
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" asChild className="gap-2">
                <Link to="/backlog">
                  <ArrowLeft className="h-4 w-4" />
                  {t("boardArea.released.backToBacklog")}
                </Link>
              </Button>
              <Button variant="outline" asChild className="gap-2">
                <Link to="/board">
                  <LayoutGrid className="h-4 w-4" />
                  {t("boardArea.released.board")}
                </Link>
              </Button>
            </div>
          }
        />
        <div className="relative mt-4 max-w-md">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("boardArea.released.searchPlaceholder")}
            className="pl-9"
          />
        </div>
      </div>

      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-3 p-6">
          {loading ? (
            <>
              <Skeleton className="h-20 w-full rounded-xl" />
              <Skeleton className="h-20 w-full rounded-xl" />
              <Skeleton className="h-20 w-full rounded-xl" />
            </>
          ) : tasks.length === 0 ? (
            <Card className="border-dashed">
              <EmptyState
                icon={Archive}
                title={
                  query
                    ? t("boardArea.released.emptySearch", { query })
                    : t("boardArea.released.empty")
                }
                className="py-16"
              />
            </Card>
          ) : (
            <>
              <p className="text-sm text-muted-foreground">
                {t("boardArea.released.count", { count: tasks.length })}
              </p>
              {tasks.map((task) => (
                <Card
                  key={task.id}
                  className="cursor-pointer p-4 transition-all hover:border-primary/30 hover:shadow-[var(--shadow-overlay)]"
                  onClick={() => {
                    setSelectedId(task.id);
                    setDrawerOpen(true);
                  }}
                >
                  <div className="flex flex-wrap items-center gap-1">
                    <Badge variant="outline" className="font-mono text-micro">
                      {task.key}
                    </Badge>
                    <Badge variant="secondary" className="text-micro">
                      {taskTypeLabel(task.task_type)}
                    </Badge>
                    <Badge variant="outline" className="text-micro">
                      {taskPriorityLabel(task.priority)}
                    </Badge>
                    <Badge variant="outline" className="text-micro">
                      {repositoryName(task.repository_id)}
                    </Badge>
                  </div>
                  <h3 className="mt-2 font-semibold leading-snug">{task.title}</h3>
                  {task.description && (
                    <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">
                      {task.description}
                    </p>
                  )}
                  <p className="mt-3 text-micro text-muted-foreground">
                    {t("boardArea.released.releasedAt", {
                      value: formatRelativeDate(task.column_entered_at ?? task.updated_at),
                    })}
                  </p>
                </Card>
              ))}
            </>
          )}
        </div>
      </ScrollArea>

      {config && selectedTask && (
        <TaskDetailDrawer
          open={drawerOpen}
          onOpenChange={setDrawerOpen}
          repositoryId={selectedTask.repository_id}
          task={selectedTask}
          columns={config.columns}
          members={memberList}
          agents={agents}
          initiativeProjects={initiativeProjects}
          repositories={repositories}
          onUpdated={() => void search(query)}
        />
      )}
    </div>
  );
}
