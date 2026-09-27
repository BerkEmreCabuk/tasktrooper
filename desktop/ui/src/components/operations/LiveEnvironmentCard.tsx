import { ExternalLink, Loader2 } from "lucide-react";
import { HEALTH_DOT } from "@/components/projects/hub/EnvironmentChips";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { DeployStateBadge } from "@/components/operations/DeployFeedTable";
import { useI18n } from "@/hooks/useI18n";
import type { DeployFeedItem, EnvRef } from "@/lib/deployFeed";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

interface LiveEnvironmentCardProps {
  envRef: EnvRef;
  live?: DeployFeedItem;
  inFlight?: DeployFeedItem;
  /** The provider could not be read for this environment. */
  unavailable?: boolean;
  onShowHistory: () => void;
}

/** One environment and what it serves right now. */
export function LiveEnvironmentCard({ envRef, live, inFlight, unavailable, onShowHistory }: LiveEnvironmentCardProps) {
  const { t, lang } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.deployments.live.${key}`, params);
  const { env, repo, componentName } = envRef;
  const health = env.health ?? "unknown";
  const host = env.url?.replace(/^https?:\/\//, "").replace(/\/$/, "");

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-start gap-2">
        {env.provider && <ProviderIcon provider={env.provider} className="mt-0.5 h-4 w-4 shrink-0" />}
        <div className="min-w-0 flex-1">
          <p className="truncate font-semibold">{componentName}</p>
          {componentName !== repo.name && <p className="truncate text-caption text-muted-foreground">{repo.name}</p>}
        </div>
        <Badge variant="outline">{t(`cloud.environments.${env.environment}`)}</Badge>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption">
        <span className="inline-flex items-center gap-1.5">
          <span className={cn("h-2 w-2 rounded-full", HEALTH_DOT[health])} aria-hidden />
          {t(`cloud.health.${health}`)}
        </span>
        {env.error_count_24h > 0 && (
          <span className="text-destructive">{d("errors24h", { count: env.error_count_24h })}</span>
        )}
        {env.url && (
          <a
            href={env.url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex min-w-0 items-center gap-1 text-muted-foreground hover:text-foreground"
          >
            <span className="truncate">{host}</span>
            <ExternalLink className="h-3 w-3 shrink-0" />
          </a>
        )}
      </div>

      <div className="rounded-md bg-muted/50 px-3 py-2">
        <div className="flex items-center justify-between gap-2">
          <p className="text-micro font-medium uppercase tracking-wide text-muted-foreground">{d("serving")}</p>
          {live && live.state !== "success" && <DeployStateBadge item={live} />}
        </div>
        {live ? (
          <>
            <p className="mt-1 flex items-center gap-2 text-body">
              {live.commitSha && <code className="shrink-0 font-mono text-caption">{live.commitSha.slice(0, 7)}</code>}
              <span className="truncate">{live.commitMessage || live.release?.version || "—"}</span>
            </p>
            <p className="mt-0.5 text-caption text-muted-foreground" title={formatDate(live.at)}>
              {formatRelativeTime(live.at, lang)}
              {live.creator ? ` · ${live.creator}` : ""}
              {live.release ? ` · ${d("viaRelease", { version: live.release.version })}` : ""}
            </p>
          </>
        ) : (
          <p className="mt-1 text-caption text-muted-foreground">{unavailable ? d("unavailable") : d("nothingYet")}</p>
        )}
      </div>

      <div className="mt-auto flex items-center justify-between gap-2">
        {inFlight ? (
          <span className="inline-flex items-center gap-1.5 text-caption text-info">
            <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            {d("inFlight")}
          </span>
        ) : (
          <span />
        )}
        <Button variant="ghost" size="sm" onClick={onShowHistory}>
          {d("showHistory")}
        </Button>
      </div>
    </Card>
  );
}
