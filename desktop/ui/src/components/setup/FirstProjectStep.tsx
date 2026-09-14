import { FolderPlus } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type InitiativeProject, type Repository } from "@/api";
import { ProjectFormDialog } from "@/components/projects/ProjectFormDialog";
import { ProjectRepositoriesSection } from "@/components/projects/ProjectRepositoriesSection";
import {
  RepositoryImportDialogs,
  RepositoryImportErrorNotice,
  useRepositoryImport,
} from "@/components/projects/useRepositoryImport";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useSetup } from "@/hooks/useSetup";

/**
 * Step 4: a project, and the first repository imported into it.
 *
 * Both halves run the code the Projects page runs: `ProjectFormDialog` creates
 * the project, and `useRepositoryImport` + `ProjectRepositoriesSection` drive
 * the one-repository-at-a-time wizard — clone or import or create, then the
 * mandatory repo kind / deploy / command questions, then indexing. Nothing
 * about importing is reimplemented here, so the questions cannot go out of
 * step between the two screens that ask them.
 *
 * The step is done when a project has at least one repository linked to it,
 * which `useSetup` derives; this component only has to get the user there.
 */
export function FirstProjectStep() {
  const { t } = useI18n();
  const { steps, refresh } = useSetup();
  const step = steps.project;

  const [projects, setProjects] = useState<InitiativeProject[] | null>(null);
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [loadError, setLoadError] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);

  // Its own read rather than the provider's: this screen renders the project
  // ROW — name, repositories, the add buttons — and needs the objects, where
  // the provider only keeps the yes/no the step's state is derived from.
  const load = useCallback(async () => {
    try {
      const [p, r] = await Promise.all([api.listInitiativeProjects(), api.listRepositories()]);
      setProjects(p.projects ?? []);
      setRepositories(r.repositories ?? []);
      setLoadError("");
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : t("setup.project.loadFailed"));
    }
    // The verdict this step is gated on is the provider's, not this component's.
    await refresh();
  }, [t, refresh]);

  useEffect(() => {
    void load();
  }, [load]);

  const repoImport = useRepositoryImport(load);

  const projectNameById = useMemo(
    () => Object.fromEntries((projects ?? []).map((p) => [p.id, p.name])),
    [projects],
  );

  // The first project is the one the sequence is about. Everything else on
  // this screen — editing, deleting, the other projects — belongs on the
  // Projects page, which is one click away once this is done.
  const project = projects?.[0] ?? null;
  const projectRepos = project
    ? repositories.filter((repo) => (repo.project_ids ?? []).includes(project.id))
    : [];

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">{t("setup.project.description")}</p>

      {step.state === "unknown" && (
        <Notice variant="warning" title={t("setup.state.unknown")}>
          <p>{step.error ?? t("setup.unknownHint")}</p>
          {step.error && <p className="mt-1">{t("setup.unknownHint")}</p>}
        </Notice>
      )}

      {loadError && (
        <Notice variant="error" title={t("setup.project.loadFailed")}>
          <p>{loadError}</p>
          <Button variant="outline" size="sm" className="mt-2" onClick={() => void load()}>
            {t("common.refresh")}
          </Button>
        </Notice>
      )}

      <RepositoryImportErrorNotice state={repoImport} />

      {projects === null && !loadError && <Skeleton className="h-32 rounded-xl" />}

      {projects !== null && project === null && (
        <Button className="gap-2" onClick={() => setDialogOpen(true)}>
          <FolderPlus className="h-4 w-4" />
          {t("setup.project.createProject")}
        </Button>
      )}

      {project !== null && (
        <>
          {step.state === "done" ? (
            <Notice variant="info" title={t("setup.project.doneTitle")}>
              {t("setup.project.doneBody")}
            </Notice>
          ) : (
            <Notice variant="warning" title={t("setup.project.needsRepositoryTitle")}>
              {t("setup.project.needsRepositoryBody")}
            </Notice>
          )}

          <ProjectRepositoriesSection
            project={project}
            repositories={projectRepos}
            projectNameById={projectNameById}
            onEditProject={() => setDialogOpen(true)}
            // No delete handlers on purpose: the section then draws no bin
            // icons at all. Undoing the step you are on is not a step, and the
            // Projects page is one click away for anyone who means it.
            onRestored={load}
            onAddRepository={(method) => repoImport.start(project.id, method)}
            addDisabled={repoImport.pendingSetup}
          />
        </>
      )}

      <ProjectFormDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        project={project}
        onSaved={() => void load()}
      />
      <RepositoryImportDialogs state={repoImport} />
    </div>
  );
}
