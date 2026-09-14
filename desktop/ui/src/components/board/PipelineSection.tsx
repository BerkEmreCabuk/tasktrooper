import { AlertTriangle, ExternalLink, GitBranch, Loader2, RotateCw } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type TaskPipeline } from "@/api";
import { PipelineStages } from "@/components/board/PipelineStages";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import {
  pipelineGateReasonLabel,
  pipelineStatusLabel,
  pipelineStatusVariant,
  pipelineTriggerLabel,
} from "@/lib/project-board";
import { formatDurationMs, formatRelativeDate } from "@/lib/utils";

interface PipelineSectionProps {
  repositoryId: string;
  taskId: string;
}

function pipelineDuration(pipeline: TaskPipeline): string {
  if (!pipeline.started_at || !pipeline.finished_at) return "-";
  const ms = new Date(pipeline.finished_at).getTime() - new Date(pipeline.started_at).getTime();
  return formatDurationMs(ms);
}

/** First job link of a run — the entry point into the provider's own UI. */
function pipelineRunUrl(pipeline: TaskPipeline): string | undefined {
  return pipeline.jobs?.find((job) => job.run_url)?.run_url;
}

/**
 * PipelineSection loads and polls a task's QA-gate pipeline runs: the latest
 * run expanded (via PipelineStages), older runs as collapsed history rows,
 * with a manual retrigger action.
 */
export function PipelineSection({ repositoryId, taskId }: PipelineSectionProps) {
  const { t } = useI18n();
  const [pipelines, setPipelines] = useState<TaskPipeline[]>([]);
  const [loading, setLoading] = useState(true);
  const [triggering, setTriggering] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.listTaskPipelines(repositoryId, taskId);
      setPipelines(res.pipelines ?? []);
    } catch {
      setPipelines([]);
    } finally {
      setLoading(false);
    }
  }, [repositoryId, taskId]);

  useEffect(() => {
    setLoading(true);
    load();
  }, [load]);

  const latest = pipelines[0] ?? null;
  const isActive = latest ? latest.status === "pending" || latest.status === "running" : false;

  usePolling(load, 5000, isActive);

  const handleRetry = async () => {
    setTriggering(true);
    try {
      await api.triggerTaskPipeline(repositoryId, taskId);
      await load();
      toast.success(t("boardArea.components.pipeline.triggered"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.pipeline.triggerFailed"));
    } finally {
      setTriggering(false);
    }
  };

  if (loading && pipelines.length === 0) {
    return (
      <div className="flex items-center justify-center py-6">
        <Spinner />
      </div>
    );
  }

  if (!latest) {
    return <EmptyState icon={GitBranch} title={t("boardArea.components.pipeline.empty")} />;
  }

  const older = pipelines.slice(1);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <Badge variant={pipelineStatusVariant(latest.status)}>{pipelineStatusLabel(latest.status)}</Badge>
          <span>{pipelineTriggerLabel(latest.trigger)}</span>
          {(latest.status === "success" || latest.status === "failed") && (
            <span>· {pipelineDuration(latest)}</span>
          )}
          <span>· {formatRelativeDate(latest.created_at)}</span>
          {latest.provider && (
            <Badge variant="outline" className="font-normal">
              {t(`boardArea.components.pipeline.provider.${latest.provider}`)}
            </Badge>
          )}
          {latest.coverage_pct != null && (
            <Badge variant="outline" className="font-normal">
              {t("boardArea.components.pipeline.coverage", { value: latest.coverage_pct.toFixed(1) })}
            </Badge>
          )}
          {pipelineRunUrl(latest) && (
            <a
              href={pipelineRunUrl(latest)}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-primary hover:underline"
            >
              <ExternalLink className="h-3 w-3" />
              {t("boardArea.components.pipeline.openRun")}
            </a>
          )}
        </div>
        <Button variant="outline" size="sm" className="gap-1.5" onClick={handleRetry} disabled={triggering || isActive}>
          {triggering ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />}
          {t("boardArea.components.pipeline.retry")}
        </Button>
      </div>

      {/*
        The gate banner. A "Not run" badge with a reviewer already on the card is
        the most confusing state this panel can show — indistinguishable, at a
        glance, from CI having passed — so when the gate was opened WITHOUT a
        build the panel says so in a sentence, and repeats the server's own note
        (which names the commit, the window it waited out, or GitHub's refusal).
      */}
      {latest.gate_reason && (
        <div className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 p-2.5 text-xs">
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          <div className="space-y-0.5">
            <p className="font-medium">{pipelineGateReasonLabel(latest.gate_reason)}</p>
            {latest.note && <p className="text-muted-foreground">{latest.note}</p>}
          </div>
        </div>
      )}

      <PipelineStages pipeline={latest} />

      {older.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t("boardArea.components.pipeline.history")}</p>
          <div className="divide-y divide-border rounded-lg border border-border">
            {older.map((p) => (
              <div key={p.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-xs">
                <div className="flex items-center gap-2">
                  <Badge variant={pipelineStatusVariant(p.status)}>{pipelineStatusLabel(p.status)}</Badge>
                  <span className="text-muted-foreground">{pipelineTriggerLabel(p.trigger)}</span>
                  {p.provider && (
                    <span className="text-muted-foreground">
                      {t(`boardArea.components.pipeline.provider.${p.provider}`)}
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-2 text-muted-foreground">
                  <span>{pipelineDuration(p)}</span>
                  <span>{formatRelativeDate(p.created_at)}</span>
                  {pipelineRunUrl(p) && (
                    <a
                      href={pipelineRunUrl(p)}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-primary hover:underline"
                    >
                      <ExternalLink className="h-3 w-3" />
                    </a>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
