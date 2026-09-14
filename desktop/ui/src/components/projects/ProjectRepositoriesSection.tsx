import { FolderOpen, FolderPlus, GitBranch, Pencil, Trash2 } from "lucide-react";
import type { InitiativeProject, Repository } from "@/api";
import { RepositoryRow } from "@/components/projects/RepositoryRow";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";

export type RepositoryAddMethod = "open" | "import" | "create";

interface ProjectRepositoriesSectionProps {
  project: InitiativeProject;
  repositories: Repository[];
  projectNameById: Record<string, string>;
  onEditProject: () => void;
  /**
   * Omit to draw no delete affordance at all — the guided first-run sequence
   * renders this section for the project it has just told the user to create,
   * and a bin icon beside it is an invitation to undo the step they are on.
   * Deleting lives on the Projects page, where it belongs.
   */
  onDeleteProject?: () => void;
  /** Omit for the same reason as `onDeleteProject`: no button rather than a dead one. */
  onDeleteRepository?: (repository: Repository) => void;
  onRestored: () => void;
  onAddRepository: (method: RepositoryAddMethod) => void;
  /** True while another repository's import wizard is mid-flow anywhere on the page. */
  addDisabled: boolean;
}

/**
 * One project, its repositories, and the one place they are added from. The
 * three add buttons stay reachable even with zero repositories yet — an empty
 * project is exactly when "add a repository" matters most.
 */
export function ProjectRepositoriesSection({
  project,
  repositories,
  projectNameById,
  onEditProject,
  onDeleteProject,
  onDeleteRepository,
  onRestored,
  onAddRepository,
  addDisabled,
}: ProjectRepositoriesSectionProps) {
  const { t } = useI18n();

  return (
    <Card className="overflow-hidden p-0">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border px-4 py-3">
        <div className="min-w-0">
          <h3 className="font-semibold">{project.name}</h3>
          <p className="mt-0.5 line-clamp-2 text-sm text-muted-foreground">
            {project.description || t("projectAdmin.projects.noDescription")}
          </p>
        </div>
        <div className="flex shrink-0 gap-1">
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onEditProject} title={t("common.edit")}>
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          {onDeleteProject && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-muted-foreground hover:text-destructive"
              onClick={onDeleteProject}
              title={t("projectAdmin.projects.delete")}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          )}
        </div>
      </div>

      {repositories.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-muted-foreground">
          {t("projectAdmin.projects.noRepositories")}
        </p>
      ) : (
        <div className="divide-y divide-border">
          {repositories.map((repo) => (
            <RepositoryRow
              key={repo.id}
              repository={repo}
              projectNameById={projectNameById}
              {...(onDeleteRepository ? { onDeleteRequest: onDeleteRepository } : {})}
              onRestored={onRestored}
            />
          ))}
        </div>
      )}

      <div className="flex flex-wrap gap-2 border-t border-border px-4 py-3">
        <Button
          variant="outline"
          size="sm"
          className="gap-2"
          disabled={addDisabled}
          onClick={() => onAddRepository("open")}
        >
          <FolderOpen className="h-3.5 w-3.5" />
          {t("boardArea.repos.openRepo")}
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="gap-2"
          disabled={addDisabled}
          onClick={() => onAddRepository("import")}
        >
          <GitBranch className="h-3.5 w-3.5" />
          {t("boardArea.repos.importGithub")}
        </Button>
        <Button size="sm" className="gap-2" disabled={addDisabled} onClick={() => onAddRepository("create")}>
          <FolderPlus className="h-3.5 w-3.5" />
          {t("boardArea.repos.createRepo")}
        </Button>
      </div>
      {addDisabled && (
        <p className="border-t border-border bg-muted/30 px-4 py-2 text-xs text-muted-foreground">
          {t("projectAdmin.projects.addRepoBlocked")}
        </p>
      )}
    </Card>
  );
}
