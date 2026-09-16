import { Network } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type InitiativeProject, type RepoDependency, type Repository } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

interface ProjectArchitectureSectionProps {
  project: InitiativeProject;
  repositories: Repository[];
  projects: InitiativeProject[];
}

interface DependencyGroup {
  sourceRepositoryId: string;
  deps: RepoDependency[];
}

function groupBySource(deps: RepoDependency[]): DependencyGroup[] {
  const groups: DependencyGroup[] = [];
  const index = new Map<string, DependencyGroup>();
  for (const dep of deps) {
    let group = index.get(dep.repository_id);
    if (!group) {
      group = { sourceRepositoryId: dep.repository_id, deps: [] };
      index.set(dep.repository_id, group);
      groups.push(group);
    }
    group.deps.push(dep);
  }
  return groups;
}

/**
 * Read-only view of every dependency edge touching this project's
 * repositories — both what they depend on and what depends on them — fed by
 * the records managed in each repository's own `DependenciesPanel`. Outgoing
 * and incoming edges are kept in separate sections (the source repo is
 * always inside this project for one, always outside for the other), each
 * grouped by source repository so "kaynak repo → hedef" reads as one block
 * per repo rather than a flat, undifferentiated list.
 */
export function ProjectArchitectureSection({ project, repositories, projects }: ProjectArchitectureSectionProps) {
  const { t } = useI18n();
  const [outgoing, setOutgoing] = useState<RepoDependency[] | null>(null);
  const [incoming, setIncoming] = useState<RepoDependency[] | null>(null);

  const load = useCallback(async () => {
    try {
      const data = await api.listProjectDependencies(project.id);
      setOutgoing(data.outgoing ?? []);
      setIncoming(data.incoming ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.dependencies.loadFailed"));
      setOutgoing([]);
      setIncoming([]);
    }
  }, [project.id, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const repoById = (id?: string) => repositories.find((r) => r.id === id);
  const repoName = (id?: string) => repoById(id)?.name ?? id ?? "";
  const subProjectLabel = (path: string) => (path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : path);

  const targetLabel = (dep: RepoDependency): string => {
    switch (dep.target_kind) {
      case "sub_repo":
        return t("projectAdmin.dependencies.subRepoSummary", {
          repo: repoName(dep.target_repository_id),
          path: subProjectLabel(dep.target_sub_project_path ?? ""),
        });
      case "repo":
        return repoName(dep.target_repository_id);
      case "database":
        return dep.database_label ?? "";
      default:
        return "";
    }
  };

  // A repository whose own linked project(s) don't include this one is the
  // signal that an edge crosses a project boundary — nothing stores that as
  // a flag.
  const projectBadgeFor = (repo: Repository | undefined): string | null => {
    if (!repo) return null;
    if ((repo.project_ids ?? []).includes(project.id)) return null;
    const otherId = repo.project_ids?.[0];
    return projects.find((p) => p.id === otherId)?.name ?? null;
  };

  const targetBadge = (dep: RepoDependency): string | null => {
    if (dep.target_kind === "database" || !dep.target_repository_id) return null;
    return projectBadgeFor(repoById(dep.target_repository_id));
  };

  if (outgoing === null || incoming === null) {
    return (
      <Card className="w-full space-y-3 p-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-16 w-full" />
      </Card>
    );
  }

  const isEmpty = outgoing.length === 0 && incoming.length === 0;

  const renderGroups = (groups: DependencyGroup[], rowBadge: (dep: RepoDependency) => string | null) => (
    <div className="divide-y divide-border rounded-md border border-border/60">
      {groups.map((group) => {
        const sourceRepo = repoById(group.sourceRepositoryId);
        const headerBadge = projectBadgeFor(sourceRepo);
        return (
          <div key={group.sourceRepositoryId} className="px-3 py-2">
            <div className="flex items-center gap-2 text-sm font-medium">
              <span>{repoName(group.sourceRepositoryId)}</span>
              {headerBadge && <Badge variant="outline">{headerBadge}</Badge>}
            </div>
            <div className="mt-1 space-y-1 pl-4">
              {group.deps.map((dep) => (
                <div key={dep.id} className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                  <span aria-hidden>→</span>
                  <span className="truncate text-foreground">{targetLabel(dep)}</span>
                  {rowBadge(dep) && <Badge variant="outline">{rowBadge(dep)}</Badge>}
                </div>
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );

  return (
    <Card className="w-full space-y-3 p-4">
      <h4 className="flex items-center gap-2 text-sm font-semibold">
        <Network className="h-4 w-4" aria-hidden />
        {t("projectAdmin.dependencies.architectureTitle")}
      </h4>

      {isEmpty ? (
        <EmptyState icon={Network} title={t("projectAdmin.dependencies.architectureEmpty")} className="py-6" />
      ) : (
        <>
          <div className="space-y-2">
            <h5 className="text-xs font-semibold uppercase text-muted-foreground">
              {t("projectAdmin.dependencies.architectureOutgoing")}
            </h5>
            {outgoing.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("projectAdmin.dependencies.architectureEmpty")}</p>
            ) : (
              renderGroups(groupBySource(outgoing), targetBadge)
            )}
          </div>

          <div className="space-y-2">
            <h5 className="text-xs font-semibold uppercase text-muted-foreground">
              {t("projectAdmin.dependencies.architectureIncoming")}
            </h5>
            {incoming.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("projectAdmin.dependencies.architectureEmpty")}</p>
            ) : (
              renderGroups(groupBySource(incoming), () => null)
            )}
          </div>
        </>
      )}
    </Card>
  );
}
