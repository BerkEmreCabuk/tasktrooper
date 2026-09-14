import { Activity, ChevronUp } from "lucide-react";
import type { SessionStep } from "@/api";
import { Button } from "@/components/ui/button";
import { getLiveStepSummary } from "@/lib/sessionGraph";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface SessionActivityBarProps {
  steps: SessionStep[];
  isLive: boolean;
  onOpenDetails?: () => void;
  className?: string;
}

export function SessionActivityBar({ steps, isLive, onOpenDetails, className }: SessionActivityBarProps) {
  const { t } = useI18n();
  const summary = getLiveStepSummary(steps, isLive);
  if (!isLive && steps.length === 0) return null;

  return (
    <div
      className={cn(
        "flex items-center gap-3 border-t border-border bg-muted/30 px-4 py-2",
        className,
      )}
    >
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <span className="relative flex h-2 w-2 shrink-0">
          {isLive && (
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-warning opacity-60" />
          )}
          <span className={cn("relative inline-flex h-2 w-2 rounded-full", isLive ? "bg-warning" : "bg-muted-foreground/40")} />
        </span>
        <Activity className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <p className="truncate text-xs text-foreground">{summary ?? t("chatArea.chat.activityBar.sessionActivity")}</p>
        {steps.length > 0 && (
          <span className="shrink-0 text-[10px] text-muted-foreground">{t("chatArea.chat.activityBar.stepsCount", { count: steps.length })}</span>
        )}
      </div>
      {onOpenDetails && (
        <Button type="button" variant="ghost" size="sm" className="h-7 shrink-0 gap-1 text-xs xl:hidden" onClick={onOpenDetails}>
          <ChevronUp className="h-3.5 w-3.5" />
          {t("chatArea.chat.activityBar.graph")}
        </Button>
      )}
    </div>
  );
}
