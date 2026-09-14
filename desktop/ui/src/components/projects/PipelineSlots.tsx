import type { PipelineCategory, PipelineCategorySuggestion } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

// Listed in pipeline order — the order a change actually travels: the four
// checks that run on the branch, then the workflow that opens the PR, then the
// deploy environments. mutation_test is a check like the others but never
// reddens the gate (the workflow runs it continue-on-error), and pr_open is
// informational: agent task branches open their own PR, so nothing dispatches
// it.
export const PIPELINE_CATEGORIES: { value: PipelineCategory; target: "job" | "workflow" }[] = [
  { value: "validate", target: "job" },
  { value: "build", target: "job" },
  { value: "test", target: "job" },
  { value: "mutation_test", target: "job" },
  { value: "pr_open", target: "workflow" },
  { value: "stage_deploy", target: "workflow" },
  { value: "preprod_deploy", target: "workflow" },
  { value: "prod_deploy", target: "workflow" },
];

const NO_TARGET = "__none__";

interface PipelineSlotsProps {
  /** The suggestions for this one group (one sub-project path / sub-repo kind). */
  suggestions: PipelineCategorySuggestion[];
  /** Chosen workflow/job ref per category; a missing entry means "skip". */
  values: Record<string, string>;
  onChange: (category: PipelineCategory, targetRef: string) => void;
  className?: string;
}

/**
 * PipelineSlots renders the eight category → Actions job/workflow selects for a
 * single pipeline group. It owns no state: the settings page holds one slot map
 * for the whole repository (keyed by path + kind + category) because one PUT
 * saves every group at once.
 */
export function PipelineSlots({ suggestions, values, onChange, className }: PipelineSlotsProps) {
  const { t } = useI18n();

  return (
    <div className={cn("grid gap-3 sm:grid-cols-2 lg:grid-cols-4", className)}>
      {PIPELINE_CATEGORIES.map((cat) => {
        const suggestion = suggestions.find((s) => s.category === cat.value);
        const candidates = suggestion?.candidates ?? [];
        const value = values[cat.value] ?? "";
        return (
          <div key={cat.value} className="space-y-1.5">
            <div className="flex items-center gap-1.5">
              <Label className="text-xs">{t(`projectAdmin.projectSettings.categories.${cat.value}`)}</Label>
              {suggestion?.ambiguous && !value && (
                <Badge variant="outline" className="border-amber-500/50 text-amber-500">
                  {t("projectAdmin.projectSettings.selectBadge")}
                </Badge>
              )}
            </div>
            <Select
              value={value || NO_TARGET}
              onValueChange={(v) => onChange(cat.value, v === NO_TARGET ? "" : v)}
            >
              <SelectTrigger className="text-xs">
                <SelectValue placeholder="—" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_TARGET}>{t("projectAdmin.projectSettings.skipOption")}</SelectItem>
                {value && !candidates.some((c) => c.ref === value) && <SelectItem value={value}>{value}</SelectItem>}
                {candidates.map((c) => (
                  <SelectItem key={`${c.ref}::${c.workflow_file ?? ""}`} value={c.ref}>
                    {c.ref}
                    {c.workflow_file && c.workflow_file !== c.ref && (
                      <span className="ml-1.5 text-muted-foreground">· {c.workflow_file}</span>
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        );
      })}
    </div>
  );
}
