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

/**
 * Read-only view of every dependency edge touching this project's
 * repositories — both what they depend on and what depends on them — fed by
 * the records managed in each repository's own `DependenciesPanel`.
 */
export function ProjectArchitectureSection({ project, repositories, projects }: ProjectArchitectureSectionProps) {
  const { t } = useI18n();
  const [edges, setEdges] = useState<RepoDependency[] | null>(null);

  const load = useCallback(async () => {
    try {
      const data = await api.listProjectDependencies(project.id);
      setEdges([...(data.outgoing ?? []), ...(data.incoming ?? [])]);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.dependencies.loadFailed"));
      setEdges([]);
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

  // The target repo's own linked project(s) rarely include this one when the
  // edge crosses a project boundary — that mismatch is the signal, not a flag
  // stored anywhere.
  const otherProjectBadge = (dep: RepoDependency): string | null => {
    if (dep.target_kind === "database" || !dep.target_repository_id) return null;
    const target = repoById(dep.target_repository_id);
    if (!target) return null;
    if ((target.project_ids ?? []).includes(project.id)) return null;
    const otherId = target.project_ids?.[0];
    return projects.find((p) => p.id === otherId)?.name ?? null;
  };

  if (edges === null) {
    return (
      <Card className="w-full space-y-3 p-4">
        <Skeleton className="h-4 w-40" />
        <Skeleton className="h-16 w-full" />
      </Card>
    );
  }

  return (
    <Card className="w-full space-y-3 p-4">
      <h4 className="flex items-center gap-2 text-sm font-semibold">
        <Network className="h-4 w-4" aria-hidden />
        {t("projectAdmin.dependencies.architectureTitle")}
      </h4>

      {edges.length === 0 ? (
        <EmptyState icon={Network} title={t("projectAdmin.dependencies.architectureEmpty")} className="py-6" />
      ) : (
        <div className="divide-y divide-border rounded-md border border-border/60">
          {edges.map((dep) => {
            const badge = otherProjectBadge(dep);
            return (
              <div key={dep.id} className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm">
                <span className="font-medium">{repoName(dep.repository_id)}</span>
                <span className="text-muted-foreground" aria-hidden>
                  →
                </span>
                <span className="truncate">{targetLabel(dep)}</span>
                {badge && <Badge variant="outline">{badge}</Badge>}
              </div>
            );
          })}
        </div>
      )}
    </Card>
  );
}
