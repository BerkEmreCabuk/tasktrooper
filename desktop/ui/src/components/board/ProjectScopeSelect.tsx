import { FolderKanban, Plus } from "lucide-react";
import { useNavigate } from "react-router-dom";
import type { InitiativeProject } from "@/api";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import {
  PROJECT_SCOPE_ALL,
  PROJECT_SCOPE_NONE,
  type ProjectScope,
  type ProjectScopeCounts,
} from "@/lib/project-board";

interface ProjectScopeSelectProps {
  projects: InitiativeProject[];
  value: ProjectScope;
  onChange: (scope: ProjectScope) => void;
  /** Counts for the page it sits on — the board's and the backlog's differ. */
  counts: ProjectScopeCounts;
}

// Not a scope: picking it leaves for the projects hub, where "New project"
// lives, and the controlled value never changes.
const CREATE_PROJECT = "__create_project";

function ScopeOption({ label, count }: { label: string; count: number }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <span className="truncate">{label}</span>
      <span className="text-micro tabular-nums text-muted-foreground">{count}</span>
    </span>
  );
}

export function ProjectScopeSelect({ projects, value, onChange, counts }: ProjectScopeSelectProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const showNone = projects.length > 0 && (counts.none > 0 || value === PROJECT_SCOPE_NONE);

  const pick = (next: string) => {
    if (next === CREATE_PROJECT) navigate("/projects");
    else onChange(next);
  };

  return (
    <Select value={value} onValueChange={pick}>
      <SelectTrigger aria-label={t("boardArea.projectScope.label")} className="w-56 gap-2">
        <FolderKanban className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
        <span className="min-w-0 flex-1 text-left">
          <SelectValue />
        </span>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={PROJECT_SCOPE_ALL}>
          <ScopeOption label={t("boardArea.projectScope.all")} count={counts.all} />
        </SelectItem>
        {projects.map((project) => (
          <SelectItem key={project.id} value={project.id}>
            <ScopeOption label={project.name} count={counts.byProject[project.id] ?? 0} />
          </SelectItem>
        ))}
        {showNone && (
          <SelectItem value={PROJECT_SCOPE_NONE}>
            <ScopeOption label={t("boardArea.projectScope.none")} count={counts.none} />
          </SelectItem>
        )}
        {projects.length === 0 && (
          <SelectItem value={CREATE_PROJECT}>
            <span className="flex items-center gap-2 text-muted-foreground">
              <Plus className="h-3.5 w-3.5" aria-hidden />
              {t("boardArea.projectScope.createProject")}
            </span>
          </SelectItem>
        )}
      </SelectContent>
    </Select>
  );
}
