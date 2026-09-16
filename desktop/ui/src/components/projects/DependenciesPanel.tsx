import { Network, Pencil, Plus, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type RepoDependency, type Repository } from "@/api";
import { DependencyFormDialog } from "@/components/projects/DependencyFormDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface DependenciesPanelProps {
  repositoryId: string;
  className?: string;
}

/**
 * A repository's outgoing dependency edges — another repository, a
 * sub-project of one, or a manually-recorded database — managed entirely
 * from this repo's own settings page. See `ProjectArchitectureSection` for
 * the project-wide read view these records feed.
 */
export function DependenciesPanel({ repositoryId, className }: DependenciesPanelProps) {
  const { t } = useI18n();
  const [dependencies, setDependencies] = useState<RepoDependency[]>([]);
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [loading, setLoading] = useState(true);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<RepoDependency | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<RepoDependency | null>(null);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [depsData, reposData] = await Promise.all([
        api.listRepoDependencies(repositoryId),
        api.listRepositories(),
      ]);
      setDependencies(depsData.dependencies ?? []);
      setRepositories(reposData.repositories ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.dependencies.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [repositoryId, t]);

  useEffect(() => {
    void load();
  }, [load]);

  const repoName = (id?: string) => repositories.find((r) => r.id === id)?.name ?? id ?? "";

  const subProjectLabel = (path: string) => (path === "." ? t("projectAdmin.initialSetup.subProjectRoot") : path);

  const describe = (dep: RepoDependency): string => {
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

  const openCreate = () => {
    setEditing(null);
    setFormOpen(true);
  };

  const openEdit = (dep: RepoDependency) => {
    setEditing(dep);
    setFormOpen(true);
  };

  const applySaved = (saved: RepoDependency) => {
    setDependencies((prev) => {
      const exists = prev.some((d) => d.id === saved.id);
      return exists ? prev.map((d) => (d.id === saved.id ? saved : d)) : [...prev, saved];
    });
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteRepoDependency(repositoryId, deleteTarget.id);
      setDependencies((prev) => prev.filter((d) => d.id !== deleteTarget.id));
      toast.success(t("projectAdmin.dependencies.deleted"));
      setDeleteTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.dependencies.deleteFailed"));
    } finally {
      setDeleting(false);
    }
  };

  if (loading) {
    return (
      <Card className={cn("w-full space-y-4 p-6", className)}>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-24 w-full" />
      </Card>
    );
  }

  return (
    <Card className={cn("w-full space-y-4 p-6", className)}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 font-semibold">
            <Network className="h-4 w-4" aria-hidden />
            {t("projectAdmin.dependencies.title")}
          </h3>
          <p className="text-sm text-muted-foreground">{t("projectAdmin.dependencies.subtitle")}</p>
        </div>
        <Button size="sm" onClick={openCreate}>
          <Plus className="mr-2 h-4 w-4" aria-hidden />
          {t("projectAdmin.dependencies.add")}
        </Button>
      </div>

      {dependencies.length === 0 ? (
        <EmptyState icon={Network} title={t("projectAdmin.dependencies.empty")} className="py-8" />
      ) : (
        <div className="divide-y divide-border rounded-md border border-border/60">
          {dependencies.map((dep) => (
            <div key={dep.id} className="flex flex-wrap items-center justify-between gap-3 px-3 py-2.5">
              <div className="flex min-w-0 items-center gap-2">
                <Badge variant="outline">{t(`projectAdmin.dependencies.targetKinds.${dep.target_kind}`)}</Badge>
                <span className="truncate text-sm">{describe(dep)}</span>
              </div>
              <div className="flex shrink-0 gap-1">
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7"
                  onClick={() => openEdit(dep)}
                  title={t("common.edit")}
                >
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 text-muted-foreground hover:text-destructive"
                  onClick={() => setDeleteTarget(dep)}
                  title={t("projectAdmin.dependencies.delete")}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      <DependencyFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        repositoryId={repositoryId}
        repositories={repositories}
        dependency={editing}
        onSaved={applySaved}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={t("projectAdmin.dependencies.deleteTitle")}
        description={t("projectAdmin.dependencies.deleteDescription", { name: deleteTarget ? describe(deleteTarget) : "" })}
        confirmLabel={t("projectAdmin.dependencies.delete")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </Card>
  );
}
