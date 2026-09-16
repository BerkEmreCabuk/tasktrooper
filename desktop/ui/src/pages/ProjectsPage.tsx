import { FolderKanban, Plus } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type InitiativeProject, type Repository } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { ProjectArchitectureSection } from "@/components/projects/ProjectArchitectureSection";
import { ProjectFormDialog } from "@/components/projects/ProjectFormDialog";
import { ProjectRepositoriesSection } from "@/components/projects/ProjectRepositoriesSection";
import { RepositoryRow } from "@/components/projects/RepositoryRow";
import {
  RepositoryImportDialogs,
  RepositoryImportErrorNotice,
  useRepositoryImport,
} from "@/components/projects/useRepositoryImport";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useCachedState, useFirstLoad } from "@/hooks/useCachedState";
import { useI18n } from "@/hooks/useI18n";
import { CACHE_PROJECTS, CACHE_REPOS } from "@/lib/project-board";

/**
 * Projects, and — since the standalone Repositories page is gone — the
 * repositories under each of them. A repository is added from inside the
 * project it belongs to, so the link is automatic rather than a separate
 * step; see `ProjectRepositoriesSection` and `useRepositoryImport`, which the
 * guided first-run sequence drives with the same code for the first repo.
 */
export function ProjectsPage() {
  const { t } = useI18n();
  const [projects, setProjects] = useCachedState<InitiativeProject[]>(CACHE_PROJECTS, []);
  const [repositories, setRepositories] = useCachedState<Repository[]>(CACHE_REPOS, []);
  const [loading, setLoading] = useFirstLoad(CACHE_PROJECTS, CACHE_REPOS);

  // Project (initiative) CRUD — unchanged from before the merge.
  const [projectDialogOpen, setProjectDialogOpen] = useState(false);
  const [editProject, setEditProject] = useState<InitiativeProject | null>(null);
  const [deleteProjectTarget, setDeleteProjectTarget] = useState<InitiativeProject | null>(null);
  const [deletingProject, setDeletingProject] = useState(false);
  const [deleteRepoTarget, setDeleteRepoTarget] = useState<Repository | null>(null);
  const [deletingRepo, setDeletingRepo] = useState(false);

  const load = useCallback(async () => {
    try {
      const [projectData, repoData] = await Promise.all([api.listInitiativeProjects(), api.listRepositories()]);
      setProjects(projectData.projects ?? []);
      setRepositories(repoData.repositories ?? []);
    } catch (e) {
      // Keep the last good lists rather than blanking the page on a failure.
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projects.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t, setProjects, setRepositories, setLoading]);

  useEffect(() => {
    load();
  }, [load]);

  // The one-repository-at-a-time import wizard. One instance for the whole
  // page (not one per project card) so "the next import is only offered after
  // the current repo's questions are answered" holds across every section.
  const repoImport = useRepositoryImport(load);

  const projectNameById = useMemo(
    () => Object.fromEntries(projects.map((p) => [p.id, p.name])),
    [projects],
  );

  // Repositories grouped by the project(s) they're linked to, plus everything
  // with no live link — either never linked, or linked to a project that was
  // since deleted. Nothing from the old page's list is allowed to just vanish.
  const { byProject, unassigned } = useMemo(() => {
    const map = new Map<string, Repository[]>();
    for (const p of projects) map.set(p.id, []);
    const rest: Repository[] = [];
    for (const repo of repositories) {
      const ids = (repo.project_ids ?? []).filter((id) => map.has(id));
      if (ids.length === 0) {
        rest.push(repo);
        continue;
      }
      for (const id of ids) map.get(id)!.push(repo);
    }
    return { byProject: map, unassigned: rest };
  }, [projects, repositories]);

  const openCreateProject = () => {
    setEditProject(null);
    setProjectDialogOpen(true);
  };

  const openEditProject = (project: InitiativeProject) => {
    setEditProject(project);
    setProjectDialogOpen(true);
  };

  const handleDeleteProject = async () => {
    if (!deleteProjectTarget) return;
    setDeletingProject(true);
    try {
      await api.deleteInitiativeProject(deleteProjectTarget.id);
      toast.success(t("projectAdmin.projects.deleted"));
      setDeleteProjectTarget(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("projectAdmin.projects.deleteFailed"));
    } finally {
      setDeletingProject(false);
    }
  };

  const handleDeleteRepository = async () => {
    if (!deleteRepoTarget) return;
    setDeletingRepo(true);
    try {
      await api.deleteRepository(deleteRepoTarget.id);
      toast.success(t("boardArea.repos.deleted"));
      setDeleteRepoTarget(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.repos.deleteFailed"));
    } finally {
      setDeletingRepo(false);
    }
  };

  const noProjects = projects.length === 0;

  return (
    <>
      <PageHeader
        title={t("projectAdmin.projects.title")}
        description={t("projectAdmin.projects.subtitle")}
        action={
          <Button onClick={openCreateProject} className="gap-2">
            <Plus className="h-4 w-4" />
            {t("projectAdmin.projects.addProject")}
          </Button>
        }
      />

      <RepositoryImportErrorNotice state={repoImport} className="mb-4" />

      {loading ? (
        <div className="space-y-4">
          <Skeleton className="h-40 rounded-xl" />
          <Skeleton className="h-40 rounded-xl" />
        </div>
      ) : (
        <div className="space-y-4">
          {noProjects && (
            <Card className="border-dashed">
              <EmptyState
                icon={FolderKanban}
                title={t("projectAdmin.projects.emptyTitle")}
                description={t("projectAdmin.projects.emptyDesc")}
                action={<Button onClick={openCreateProject}>{t("projectAdmin.projects.addFirstProject")}</Button>}
                className="py-16"
              />
            </Card>
          )}

          {projects.map((project) => (
            <div key={project.id} className="space-y-4">
              <ProjectRepositoriesSection
                project={project}
                repositories={byProject.get(project.id) ?? []}
                projectNameById={projectNameById}
                onEditProject={() => openEditProject(project)}
                onDeleteProject={() => setDeleteProjectTarget(project)}
                onDeleteRepository={setDeleteRepoTarget}
                onRestored={load}
                onAddRepository={(method) => repoImport.start(project.id, method)}
                addDisabled={repoImport.pendingSetup}
              />
              <ProjectArchitectureSection project={project} repositories={repositories} projects={projects} />
            </div>
          ))}

          {unassigned.length > 0 && (
            <Card className="overflow-hidden p-0">
              <div className="border-b border-border px-4 py-3">
                <h3 className="font-semibold text-muted-foreground">{t("projectAdmin.projects.unassignedTitle")}</h3>
                <p className="mt-0.5 text-sm text-muted-foreground">
                  {t("projectAdmin.projects.unassignedDescription")}
                </p>
              </div>
              <div className="divide-y divide-border">
                {unassigned.map((repo) => (
                  <RepositoryRow
                    key={repo.id}
                    repository={repo}
                    projectNameById={projectNameById}
                    onDeleteRequest={setDeleteRepoTarget}
                    onRestored={load}
                  />
                ))}
              </div>
            </Card>
          )}
        </div>
      )}

      <ProjectFormDialog
        open={projectDialogOpen}
        onOpenChange={setProjectDialogOpen}
        project={editProject}
        onSaved={() => void load()}
      />

      {/* Repository add wizard: clone/import/create → mandatory setup questions. */}
      <RepositoryImportDialogs state={repoImport} />

      <ConfirmDialog
        open={deleteProjectTarget !== null}
        onOpenChange={(open) => !open && setDeleteProjectTarget(null)}
        title={t("projectAdmin.projects.deleteTitle")}
        description={t("projectAdmin.projects.deleteConfirm", { name: deleteProjectTarget?.name ?? "" })}
        confirmLabel={t("projectAdmin.projects.delete")}
        loading={deletingProject}
        onConfirm={handleDeleteProject}
      />

      <ConfirmDialog
        open={deleteRepoTarget !== null}
        onOpenChange={(open) => !open && setDeleteRepoTarget(null)}
        title={t("boardArea.repos.deleteTitle")}
        description={t("boardArea.repos.deleteConfirm", { name: deleteRepoTarget?.name ?? "" })}
        confirmLabel={t("boardArea.repos.delete")}
        loading={deletingRepo}
        onConfirm={handleDeleteRepository}
      />
    </>
  );
}
