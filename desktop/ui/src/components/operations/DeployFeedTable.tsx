import { ExternalLink } from "lucide-react";
import { DEPLOYMENT_STATUS_VARIANT } from "@/components/projects/repository/deploy/DeploymentsPanel";
import { RELEASE_STATUS_VARIANT } from "@/components/projects/repository/deploy/ReleaseDrawer";
import { Badge } from "@/components/ui/badge";
import { useI18n } from "@/hooks/useI18n";
import type { DeployFeedItem } from "@/lib/deployFeed";
import { cn, formatDate, formatDuration, formatRelativeTime } from "@/lib/utils";

export function DeployStateBadge({ item }: { item: DeployFeedItem }) {
  const { t } = useI18n();
  if (item.release) {
    return (
      <Badge variant={RELEASE_STATUS_VARIANT[item.release.status]}>{t(`release.statuses.${item.release.status}`)}</Badge>
    );
  }
  if (item.deployment) {
    return (
      <Badge variant={DEPLOYMENT_STATUS_VARIANT[item.deployment.status]}>
        {t(`repositoryPage.deploy.runtime.deployments.status.${item.deployment.status}`)}
      </Badge>
    );
  }
  return null;
}

export function feedLink(item: DeployFeedItem): string | undefined {
  return item.deployment?.inspect_url || item.release?.deploy?.run_url || item.deployment?.url;
}

interface DeployFeedTableProps {
  items: DeployFeedItem[];
  onOpenRelease: (item: DeployFeedItem) => void;
}

/** Deploys newest first: TaskTrooper releases (row opens the release drawer)
 * and provider deployments nobody released through TaskTrooper (row links out
 * to the provider). */
export function DeployFeedTable({ items, onOpenRelease }: DeployFeedTableProps) {
  const { t, lang } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.deployments.feed.${key}`, params);

  return (
    <div className="-mx-5 overflow-x-auto px-5">
      <table className="w-full min-w-[820px] text-body">
        <thead>
          <tr className="border-b border-border text-left text-caption text-muted-foreground">
            <th className="pb-2 pr-3 font-medium">{d("when")}</th>
            <th className="pb-2 pr-3 font-medium">{d("where")}</th>
            <th className="pb-2 pr-3 font-medium">{d("change")}</th>
            <th className="pb-2 pr-3 font-medium">{d("source")}</th>
            <th className="pb-2 pr-3 font-medium">{d("status")}</th>
            <th className="pb-2 pr-3 text-right font-medium">{d("duration")}</th>
            <th className="w-8 pb-2" />
          </tr>
        </thead>
        <tbody>
          {items.map((item) => {
            const link = feedLink(item);
            const release = item.release;
            const taskCount = release?.tasks?.length ?? 0;
            return (
              <tr
                key={item.key}
                onClick={release ? () => onOpenRelease(item) : undefined}
                onKeyDown={
                  release
                    ? (e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          onOpenRelease(item);
                        }
                      }
                    : undefined
                }
                tabIndex={release ? 0 : undefined}
                className={cn(
                  "border-b border-border/50 align-top last:border-0",
                  release && "cursor-pointer outline-none hover:bg-muted/40 focus-visible:bg-muted/40",
                )}
              >
                <td className="whitespace-nowrap py-2.5 pr-3 text-muted-foreground" title={formatDate(item.at)}>
                  {formatRelativeTime(item.at, lang)}
                </td>
                <td className="py-2.5 pr-3">
                  <p className="font-medium">{item.repo?.name ?? d("unknownRepository")}</p>
                  <p className="text-caption text-muted-foreground">
                    {item.componentName && item.componentName !== item.repo?.name ? `${item.componentName} · ` : ""}
                    {item.environment ? t(`cloud.environments.${item.environment}`) : "—"}
                  </p>
                </td>
                <td className="max-w-80 py-2.5 pr-3">
                  <p className="flex items-center gap-2">
                    {item.commitSha && (
                      <code className="shrink-0 font-mono text-caption">{item.commitSha.slice(0, 7)}</code>
                    )}
                    <span className="truncate">{item.commitMessage || (release ? release.version : "")}</span>
                  </p>
                  {release && taskCount > 0 && (
                    <p className="truncate text-caption text-muted-foreground">
                      {release.tasks
                        .map((task) => task.key || task.title)
                        .filter(Boolean)
                        .join(", ")}
                    </p>
                  )}
                </td>
                <td className="py-2.5 pr-3">
                  {release ? (
                    <>
                      <p>{d("sourceRelease", { version: release.version })}</p>
                      <p className="text-caption text-muted-foreground">
                        {release.executor ? t(`release.executors.${release.executor}`) : t(`release.modes.${release.mode}`)}
                      </p>
                    </>
                  ) : (
                    <>
                      <p>{item.repo && envProvider(item) ? t(`cloud.providers.${envProvider(item)}`) : d("sourceProvider")}</p>
                      <p className="text-caption text-muted-foreground">
                        {item.deployment?.branch ? `${item.deployment.branch}${item.creator ? ` · ${item.creator}` : ""}` : item.creator}
                      </p>
                    </>
                  )}
                </td>
                <td className="py-2.5 pr-3">
                  <DeployStateBadge item={item} />
                </td>
                <td className="whitespace-nowrap py-2.5 pr-3 text-right tabular-nums text-muted-foreground">
                  {item.durationMs ? formatDuration(item.durationMs, lang) : "—"}
                </td>
                <td className="py-2.5">
                  {link && (
                    <a
                      href={link}
                      target="_blank"
                      rel="noreferrer"
                      onClick={(e) => e.stopPropagation()}
                      aria-label={d("openExternal")}
                      className="inline-flex text-muted-foreground hover:text-foreground"
                    >
                      <ExternalLink className="h-4 w-4" />
                    </a>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function envProvider(item: DeployFeedItem): string | undefined {
  return item.repo?.environments.find((e) => e.id === item.envId)?.provider;
}
