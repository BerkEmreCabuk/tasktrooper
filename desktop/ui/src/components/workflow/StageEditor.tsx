import { AlertTriangle, ChevronDown, ChevronRight, ChevronUp, Trash2 } from "lucide-react";
import { useState } from "react";
import {
  STAGE_KINDS,
  type BehaviourSpec,
  type BoardColumn,
  type Role,
  type StageKind,
  type WorkflowProblem,
  type WorkflowStage,
} from "@/api";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { BehaviourPicker } from "@/components/workflow/BehaviourPicker";
import { ParticipantList } from "@/components/workflow/ParticipantList";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface StageEditorProps {
  stages: WorkflowStage[];
  onChange: (stages: WorkflowStage[]) => void;
  /** The board's current columns — a stage whose column_slug isn't here is orphaned. */
  columns: BoardColumn[];
  /** Full registry from GET /v1/workflow/behaviours; filtered here to stage scope. */
  behaviourRegistry: BehaviourSpec[];
  roles: Role[];
  hasSubscriber: (roleId: string, columnSlug: string) => boolean;
  problems?: WorkflowProblem[];
  disabled?: boolean;
}

/**
 * Organism: one task type's ordered stage list. Reordering is move-up/down
 * (drag is optional per spec, not implemented) — position is always
 * reindexed to the array's own order on every change so the PUT body never
 * carries gaps or duplicates.
 */
export function StageEditor({
  stages,
  onChange,
  columns,
  behaviourRegistry,
  roles,
  hasSubscriber,
  problems = [],
  disabled,
}: StageEditorProps) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const sorted = [...stages].sort((a, b) => a.position - b.position);
  const usedColumns = new Set(stages.map((s) => s.column_slug));
  const availableColumns = columns.filter((c) => !usedColumns.has(c.slug));
  const stageBehaviours = behaviourRegistry.filter((b) => b.scope === "stage");

  const toggleExpanded = (slug: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) next.delete(slug);
      else next.add(slug);
      return next;
    });
  };

  const reindex = (list: WorkflowStage[]) => list.map((s, i) => ({ ...s, position: i }));

  const updateStage = (slug: string, patch: Partial<WorkflowStage>) => {
    onChange(stages.map((s) => (s.column_slug === slug ? { ...s, ...patch } : s)));
  };

  const move = (slug: string, direction: -1 | 1) => {
    const index = sorted.findIndex((s) => s.column_slug === slug);
    const target = index + direction;
    if (index < 0 || target < 0 || target >= sorted.length) return;
    const next = [...sorted];
    [next[index], next[target]] = [next[target], next[index]];
    onChange(reindex(next));
  };

  const removeStage = (slug: string) => onChange(reindex(stages.filter((s) => s.column_slug !== slug)));

  const addStage = (slug: string) => {
    if (!slug || usedColumns.has(slug)) return;
    const next: WorkflowStage = {
      column_slug: slug,
      position: stages.length,
      on_path: true,
      kind: "work",
      behaviours: [],
      instructions: "",
      participants: [],
    };
    onChange([...stages, next]);
    setExpanded((prev) => new Set(prev).add(slug));
  };

  const problemsFor = (slug: string) => problems.filter((p) => p.column_slug === slug);

  return (
    <div className="space-y-2">
      {sorted.map((stage, index) => {
        const column = columns.find((c) => c.slug === stage.column_slug);
        const orphaned = !column;
        const isOpen = expanded.has(stage.column_slug);
        const stageProblems = problemsFor(stage.column_slug);
        return (
          <Card key={stage.column_slug} className={cn("overflow-hidden", orphaned && "border-destructive/50")}>
            <div className="flex items-center gap-2 px-3 py-2">
              <div className="flex flex-col">
                <button
                  type="button"
                  onClick={() => move(stage.column_slug, -1)}
                  disabled={disabled || index === 0}
                  className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                  aria-label={t("settingsPages.workflows.moveUp")}
                >
                  <ChevronUp className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  onClick={() => move(stage.column_slug, 1)}
                  disabled={disabled || index === sorted.length - 1}
                  className="text-muted-foreground hover:text-foreground disabled:opacity-30"
                  aria-label={t("settingsPages.workflows.moveDown")}
                >
                  <ChevronDown className="h-3.5 w-3.5" />
                </button>
              </div>
              <button
                type="button"
                className="flex flex-1 flex-wrap items-center gap-2 text-left"
                onClick={() => toggleExpanded(stage.column_slug)}
              >
                {isOpen ? <ChevronDown className="h-4 w-4 shrink-0" /> : <ChevronRight className="h-4 w-4 shrink-0" />}
                <span className="text-sm font-medium">{column?.label ?? stage.column_slug}</span>
                {orphaned && (
                  <Badge variant="destructive" className="gap-1">
                    <AlertTriangle className="h-3 w-3" />
                    {t("settingsPages.workflows.orphanedStage")}
                  </Badge>
                )}
                <Badge variant="outline">{t(`settingsPages.workflows.kind.${stage.kind}`)}</Badge>
                {!stage.on_path && <Badge variant="secondary">{t("settingsPages.workflows.offPath")}</Badge>}
                {stageProblems.length > 0 && <Badge variant="destructive">{stageProblems.length}</Badge>}
              </button>
              <button
                type="button"
                className="text-muted-foreground hover:text-destructive"
                onClick={() => removeStage(stage.column_slug)}
                disabled={disabled}
                title={t("settingsPages.workflows.removeStage")}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>

            {isOpen && (
              <div className="space-y-4 border-t border-border px-3 py-3">
                {stageProblems.length > 0 && (
                  <div className="space-y-1 rounded-lg border border-destructive/40 bg-destructive/5 p-2.5 text-xs text-destructive">
                    {stageProblems.map((p, i) => (
                      <p key={i}>
                        <span className="font-medium">{p.field}:</span> {p.message}
                      </p>
                    ))}
                  </div>
                )}
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <label className="text-xs text-muted-foreground">{t("settingsPages.workflows.kindLabel")}</label>
                    <Select
                      value={stage.kind}
                      onValueChange={(v) => updateStage(stage.column_slug, { kind: v as StageKind })}
                      disabled={disabled}
                    >
                      <SelectTrigger className="h-8">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {STAGE_KINDS.map((kind) => (
                          <SelectItem key={kind} value={kind}>
                            {t(`settingsPages.workflows.kind.${kind}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <label className="flex items-center gap-2 self-end pb-1.5 text-sm">
                    <Checkbox
                      checked={stage.on_path}
                      onCheckedChange={(v) => updateStage(stage.column_slug, { on_path: v === true })}
                      disabled={disabled}
                    />
                    {t("settingsPages.workflows.onPathLabel")}
                  </label>
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs text-muted-foreground">{t("settingsPages.workflows.behavioursLabel")}</label>
                  <BehaviourPicker
                    registry={stageBehaviours}
                    columns={columns}
                    selected={stage.behaviours}
                    onChange={(behaviours) => updateStage(stage.column_slug, { behaviours })}
                    disabled={disabled}
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs text-muted-foreground">{t("settingsPages.workflows.instructionsLabel")}</label>
                  <Textarea
                    value={stage.instructions}
                    onChange={(e) => updateStage(stage.column_slug, { instructions: e.target.value })}
                    rows={3}
                    disabled={disabled}
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs text-muted-foreground">{t("settingsPages.workflows.participantsLabel")}</label>
                  <ParticipantList
                    roles={roles}
                    participants={stage.participants}
                    onChange={(participants) => updateStage(stage.column_slug, { participants })}
                    hasSubscriber={(roleId) => hasSubscriber(roleId, stage.column_slug)}
                    disabled={disabled}
                  />
                </div>
              </div>
            )}
          </Card>
        );
      })}

      {availableColumns.length > 0 && (
        <Select value="" onValueChange={addStage} disabled={disabled}>
          <SelectTrigger className="w-64">
            <SelectValue placeholder={t("settingsPages.workflows.addStage")} />
          </SelectTrigger>
          <SelectContent>
            {availableColumns.map((col) => (
              <SelectItem key={col.slug} value={col.slug}>
                {col.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  );
}
