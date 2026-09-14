import { AlertCircle, CheckCircle2, CircleDashed, Clock } from "lucide-react";
import type { WorkspaceIndex } from "@/api";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useIndexProgress } from "@/hooks/useIndexProgress";
import { cn } from "@/lib/utils";

const SIZE = 28;
const STROKE = 2.5;

type TFunc = (key: string, params?: Record<string, string | number>) => string;

function tooltip(t: TFunc, index: WorkspaceIndex | null, percent: number): string {
  if (!index) return t("projectAdmin.components.indexNone");
  switch (index.status) {
    case "completed":
      return t("projectAdmin.components.indexDone", { count: index.file_count });
    case "running":
      return t("projectAdmin.components.indexRunning", { percent });
    case "pending":
      return t("projectAdmin.components.indexPending");
    case "failed":
      return index.error
        ? t("projectAdmin.components.indexError", { error: index.error })
        : t("projectAdmin.components.indexErrorGeneric");
    default:
      return index.status;
  }
}

function IndexProgressRing({ percent }: { percent: number }) {
  const radius = (SIZE - STROKE) / 2;
  const circumference = 2 * Math.PI * radius;
  const offset = circumference - (percent / 100) * circumference;

  return (
    <div className="relative flex h-7 w-7 shrink-0 items-center justify-center">
      <svg width={SIZE} height={SIZE} className="-rotate-90" aria-hidden>
        <circle
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={radius}
          fill="none"
          stroke="currentColor"
          strokeWidth={STROKE}
          className="text-muted"
        />
        <circle
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={radius}
          fill="none"
          stroke="currentColor"
          strokeWidth={STROKE}
          strokeLinecap="round"
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          className="text-primary transition-all duration-300"
        />
      </svg>
      <span className="absolute text-[9px] font-semibold tabular-nums text-foreground">{percent}</span>
    </div>
  );
}

type ProjectIndexStatusProps = {
  repositoryId: string;
  projectId?: string;
  className?: string;
};

export function ProjectIndexStatus({ repositoryId, projectId, className }: ProjectIndexStatusProps) {
  const { t } = useI18n();
  const id = repositoryId || projectId;
  const { index, loading, percent } = useIndexProgress(id);

  if (!id) return null;

  if (loading && !index) {
    return <Skeleton className={cn("h-7 w-7 shrink-0 rounded-full", className)} />;
  }

  const title = tooltip(t, index, percent);

  if (!index) {
    return (
      <span className={cn("inline-flex shrink-0", className)} title={title} aria-label={title}>
        <CircleDashed className="h-5 w-5 text-muted-foreground" />
      </span>
    );
  }

  if (index.status === "running") {
    return (
      <span className={cn("inline-flex shrink-0", className)} title={title} aria-label={title}>
        <IndexProgressRing percent={percent} />
      </span>
    );
  }

  if (index.status === "completed") {
    return (
      <span className={cn("inline-flex shrink-0", className)} title={title} aria-label={title}>
        <CheckCircle2 className="h-5 w-5 text-success" />
      </span>
    );
  }

  if (index.status === "failed") {
    return (
      <span className={cn("inline-flex shrink-0", className)} title={title} aria-label={title}>
        <AlertCircle className="h-5 w-5 text-destructive" />
      </span>
    );
  }

  return (
    <span className={cn("inline-flex shrink-0", className)} title={title} aria-label={title}>
      <Clock className="h-5 w-5 text-muted-foreground" />
    </span>
  );
}
