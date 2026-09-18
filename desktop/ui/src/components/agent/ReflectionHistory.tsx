import { ChevronRight } from "lucide-react";
import type { AgentKPI, AgentReflection, EvolutionEvent } from "@/api";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { ReflectionDetailDialog } from "@/components/agent/ReflectionDetailDialog";

type TFunc = (key: string, params?: Record<string, string | number>) => string;

export function reflectionTriggerLabel(t: TFunc, trigger: string): string {
  const key = `agentArea.perf.trigger.${trigger}`;
  const label = t(key);
  return label === key ? trigger : label;
}

export function reflectionStatusVariant(status: AgentReflection["status"]): BadgeProps["variant"] {
  return status === "completed" ? "success" : status === "failed" ? "destructive" : "warning";
}

export function reflectionStatusLabel(t: TFunc, status: AgentReflection["status"]): string {
  return status === "completed"
    ? t("agentArea.perf.reflections.statusCompleted")
    : status === "failed"
      ? t("agentArea.perf.reflections.statusFailed")
      : t("agentArea.perf.reflections.statusRunning");
}

function fmtDate(iso: string) {
  return new Date(iso).toLocaleString("tr-TR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function countByOutcome(reflection: AgentReflection) {
  const changes = reflection.decision?.changes ?? [];
  const applied = changes.filter((c) => c.outcome === "applied").length;
  const notApplied = changes.length - applied;
  return { applied, notApplied };
}

function compositeDelta(reflection: AgentReflection) {
  const after = reflection.performance_snapshot;
  const before = reflection.decision?.baseline;
  if (!after || !before) return null;
  return after.kpi_composite - before.kpi_composite;
}

function ReflectionRow({ reflection, onOpen }: { reflection: AgentReflection; onOpen: () => void }) {
  const { t } = useI18n();
  const { applied, notApplied } = countByOutcome(reflection);
  const delta = compositeDelta(reflection);
  const preview = reflection.decision?.self_assessment || reflection.summary;
  const isRunning = reflection.status === "running";

  return (
    <Card className="p-0">
      <button
        type="button"
        onClick={onOpen}
        className="flex w-full items-start justify-between gap-3 p-3 text-left transition-colors hover:bg-accent/40"
      >
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={reflectionStatusVariant(reflection.status)}>
              {isRunning ? <Spinner size="sm" className="mr-1 inline h-3 w-3" /> : null}
              {reflectionStatusLabel(t, reflection.status)}
            </Badge>
            <span className="text-xs text-muted-foreground">{reflectionTriggerLabel(t, reflection.trigger)}</span>
            <span className="text-xs text-muted-foreground">{fmtDate(reflection.created_at)}</span>
            {reflection.decision?.legacy ? <Badge variant="outline">{t("agentArea.perf.reflections.legacyBadge")}</Badge> : null}
          </div>
          {preview ? <p className="truncate text-sm">{preview}</p> : null}
          {reflection.status === "failed" && reflection.error ? (
            <p className="text-xs text-destructive">{reflection.error}</p>
          ) : null}
          {reflection.decision && reflection.decision.changes.length > 0 ? (
            <div className="flex flex-wrap items-center gap-2 pt-0.5">
              {applied > 0 ? (
                <Badge variant="success">{t("agentArea.perf.reflections.appliedCount", { count: applied })}</Badge>
              ) : null}
              {notApplied > 0 ? (
                <Badge variant="secondary">{t("agentArea.perf.reflections.notAppliedCount", { count: notApplied })}</Badge>
              ) : null}
              {delta != null ? (
                <span className={`text-xs font-medium tabular-nums ${delta >= 0 ? "text-emerald-600 dark:text-emerald-400" : "text-red-500"}`}>
                  {delta >= 0 ? "↑" : "↓"} {Math.abs(delta).toFixed(1)}
                </span>
              ) : null}
            </div>
          ) : null}
        </div>
        <ChevronRight className="mt-1 h-4 w-4 shrink-0 text-muted-foreground" />
      </button>
    </Card>
  );
}

interface ReflectionHistoryProps {
  reflections: AgentReflection[];
  events: EvolutionEvent[];
  kpis: AgentKPI[];
  openId: string | null;
  onOpenChange: (id: string | null) => void;
}

/**
 * Sits directly under the top score/KPI/self-evolution cards — reflections
 * used to be a JSON-summary afterthought at the bottom of the page, which is
 * why nobody looked at them once `summary` started shipping empty.
 */
export function ReflectionHistory({ reflections, events, kpis, openId, onOpenChange }: ReflectionHistoryProps) {
  const { t } = useI18n();
  const openReflection = reflections.find((r) => r.id === openId) ?? null;

  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold">{t("agentArea.perf.reflections.heading")}</h3>
      {reflections.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("agentArea.perf.reflections.empty")}</p>
      ) : (
        <div className="space-y-2">
          {reflections.map((r) => (
            <ReflectionRow key={r.id} reflection={r} onOpen={() => onOpenChange(r.id)} />
          ))}
        </div>
      )}
      {openReflection ? (
        <ReflectionDetailDialog
          reflection={openReflection}
          events={events}
          kpis={kpis}
          open
          onOpenChange={(o) => onOpenChange(o ? openReflection.id : null)}
        />
      ) : null}
    </section>
  );
}
