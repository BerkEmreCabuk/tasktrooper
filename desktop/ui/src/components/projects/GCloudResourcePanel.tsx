import { Cloud, ExternalLink, Link2, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  ApiError,
  api,
  type CloudRunServiceDetail,
  type GCloudResourceDetails,
  type GKEClusterDetail,
} from "@/api";
import { GCloudResourcePickerDialog } from "@/components/admin/GCloudResourcePickerDialog";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate } from "@/lib/utils";

type ReadyState = "ready" | "notReady" | "pending";

const READY_STATES: Record<string, ReadyState> = {
  true: "ready",
  ready: "ready",
  condition_succeeded: "ready",
  succeeded: "ready",
  false: "notReady",
  failed: "notReady",
  condition_failed: "notReady",
  pending: "pending",
  unknown: "pending",
  reconciling: "pending",
  condition_pending: "pending",
};

const READY_VARIANT: Record<ReadyState, BadgeProps["variant"]> = {
  ready: "success",
  notReady: "destructive",
  pending: "warning",
};

type ClusterState = "running" | "degraded" | "provisioning";

const CLUSTER_STATES: Record<string, ClusterState> = {
  running: "running",
  reconciling: "provisioning",
  provisioning: "provisioning",
  stopping: "provisioning",
  degraded: "degraded",
  error: "degraded",
  status_unspecified: "provisioning",
};

const CLUSTER_VARIANT: Record<ClusterState, BadgeProps["variant"]> = {
  running: "success",
  degraded: "destructive",
  provisioning: "warning",
};

function normalize(raw?: string): string {
  return (raw ?? "").trim().toLowerCase();
}

interface FieldProps {
  label: string;
  children: React.ReactNode;
}

function Field({ label, children }: FieldProps) {
  return (
    <div className="min-w-0 space-y-0.5">
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className="min-w-0 text-sm">{children}</div>
    </div>
  );
}

interface ExternalLinkTextProps {
  href: string;
  label: string;
  mono?: boolean;
}

/** An outward link that names where it goes and says it opens elsewhere. */
function ExternalLinkText({ href, label, mono }: ExternalLinkTextProps) {
  const { t } = useI18n();
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      aria-label={`${label} (${t("projectAdmin.gcloudResource.openInNewTab")})`}
      className={cn("inline-flex min-w-0 items-center gap-1 underline underline-offset-2", mono && "font-mono text-xs")}
    >
      <span className="truncate">{label}</span>
      <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
    </a>
  );
}

function CloudRunDetail({ service }: { service: CloudRunServiceDetail }) {
  const { t } = useI18n();
  const ready = READY_STATES[normalize(service.ready)];
  const readyLabel = ready
    ? t(`projectAdmin.gcloudResource.cloudRun.readyStates.${ready}`)
    : service.ready.trim() || t("projectAdmin.gcloudResource.notReported");
  const reason = service.ready_message || service.ready_reason;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h4 className="text-sm font-medium">{t("projectAdmin.gcloudResource.cloudRun.title")}</h4>
        <Badge variant={ready ? READY_VARIANT[ready] : "outline"}>{readyLabel}</Badge>
      </div>

      {reason && <p className="text-sm text-muted-foreground">{reason}</p>}

      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t("projectAdmin.gcloudResource.cloudRun.serviceUrl")}>
          {service.ref.uri ? (
            <ExternalLinkText href={service.ref.uri} label={service.ref.uri} mono />
          ) : (
            <span className="text-muted-foreground">{t("projectAdmin.gcloudResource.cloudRun.noServiceUrl")}</span>
          )}
        </Field>
        <Field label={t("projectAdmin.gcloudResource.cloudRun.latestReady")}>
          <span className="font-mono text-xs">
            {service.latest_ready_revision || t("projectAdmin.gcloudResource.notReported")}
          </span>
        </Field>
        <Field label={t("projectAdmin.gcloudResource.cloudRun.latestCreated")}>
          <span className="font-mono text-xs">
            {service.latest_created_revision || t("projectAdmin.gcloudResource.notReported")}
          </span>
        </Field>
        {service.update_time && (
          <Field label={t("projectAdmin.gcloudResource.cloudRun.updated")}>{formatDate(service.update_time)}</Field>
        )}
      </div>

      <Field label={t("projectAdmin.gcloudResource.cloudRun.image")}>
        <span className="break-all font-mono text-xs">
          {service.image || t("projectAdmin.gcloudResource.notReported")}
        </span>
      </Field>

      <div className="space-y-2">
        <p className="text-xs text-muted-foreground">{t("projectAdmin.gcloudResource.cloudRun.traffic")}</p>
        {service.traffic.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("projectAdmin.gcloudResource.cloudRun.trafficEmpty")}</p>
        ) : (
          <ul className="space-y-2">
            {service.traffic.map((target) => (
              <li key={`${target.revision}-${target.tag ?? ""}`} className="space-y-1">
                <div className="flex flex-wrap items-baseline justify-between gap-2">
                  <span className="min-w-0 truncate font-mono text-xs">
                    {target.revision || t("projectAdmin.gcloudResource.notReported")}
                    {target.tag && <span className="text-muted-foreground"> · {target.tag}</span>}
                  </span>
                  <span className="text-xs tabular-nums">{target.percent}%</span>
                </div>
                <Progress value={target.percent} />
                {target.uri && <ExternalLinkText href={target.uri} label={target.uri} mono />}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function GKEDetail({ cluster }: { cluster: GKEClusterDetail }) {
  const { t } = useI18n();
  const state = CLUSTER_STATES[normalize(cluster.status)];
  const statusLabel = state
    ? t(`projectAdmin.gcloudResource.gke.statuses.${state}`)
    : cluster.status.trim() || t("projectAdmin.gcloudResource.notReported");
  const pools = cluster.node_pools ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h4 className="text-sm font-medium">{t("projectAdmin.gcloudResource.gke.title")}</h4>
        <div className="flex flex-wrap gap-2">
          <Badge variant={state ? CLUSTER_VARIANT[state] : "outline"}>{statusLabel}</Badge>
          <Badge variant="outline">
            {cluster.autopilot
              ? t("projectAdmin.gcloudResource.gke.autopilot")
              : t("projectAdmin.gcloudResource.gke.standard")}
          </Badge>
          <Badge variant="outline">
            {cluster.private_endpoint
              ? t("projectAdmin.gcloudResource.gke.privateEndpoint")
              : t("projectAdmin.gcloudResource.gke.publicEndpoint")}
          </Badge>
        </div>
      </div>

      {cluster.status_message && <p className="text-sm text-muted-foreground">{cluster.status_message}</p>}

      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t("projectAdmin.gcloudResource.gke.masterVersion")}>
          <span className="font-mono text-xs">
            {cluster.master_version || t("projectAdmin.gcloudResource.notReported")}
          </span>
        </Field>
        <Field label={t("projectAdmin.gcloudResource.gke.nodeCount")}>
          <span className="tabular-nums">{cluster.node_count}</span>
        </Field>
      </div>

      <div className="space-y-2">
        <p className="text-xs text-muted-foreground">{t("projectAdmin.gcloudResource.gke.nodePools")}</p>
        {pools.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("projectAdmin.gcloudResource.gke.nodePoolsEmpty")}</p>
        ) : (
          <ul className="divide-y rounded-md border border-border/60">
            {pools.map((pool) => {
              const poolState = CLUSTER_STATES[normalize(pool.status)];
              return (
                <li key={pool.name} className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{pool.name}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {t("projectAdmin.gcloudResource.gke.poolNodes", { count: pool.node_count })}
                      {pool.machine_type && ` · ${pool.machine_type}`}
                      {pool.version && ` · ${pool.version}`}
                    </p>
                  </div>
                  <Badge variant={poolState ? CLUSTER_VARIANT[poolState] : "outline"}>
                    {poolState
                      ? t(`projectAdmin.gcloudResource.gke.statuses.${poolState}`)
                      : pool.status.trim() || t("projectAdmin.gcloudResource.notReported")}
                  </Badge>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      {!cluster.workloads_available && (
        <Notice variant="info" title={t("projectAdmin.gcloudResource.gke.workloadsTitle")}>
          {cluster.workloads_note || t("projectAdmin.gcloudResource.gke.workloadsBody")}
        </Notice>
      )}
    </div>
  );
}

interface GCloudResourcePanelProps {
  repositoryId: string;
  /** "" (default) is the repository itself; a monorepo passes the sub-repo path. */
  subProjectPath?: string;
  className?: string;
}

/**
 * GCloudResourcePanel — the hosting half of a backend or worker scope's
 * settings: the Cloud Run service or GKE cluster it runs on, and what that
 * looks like right now.
 *
 * A cluster whose workloads cannot be listed says so as a note rather than an
 * error: the shared agent-server sits outside the customer's VPC, so a private
 * control plane is unreachable by design, and `workloads_note` is the server's
 * own sentence naming which wall it hit.
 */
export function GCloudResourcePanel({ repositoryId, subProjectPath = "", className }: GCloudResourcePanelProps) {
  const { t } = useI18n();

  const [details, setDetails] = useState<GCloudResourceDetails | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);

  // Only the newest read may write state: a slow first pass must not land on
  // top of the refresh that overtook it.
  const requestRef = useRef(0);

  const load = useCallback(async () => {
    const seq = ++requestRef.current;
    try {
      const data = await api.gcloudResourceDetails(repositoryId, subProjectPath);
      if (seq !== requestRef.current) return;
      setDetails(data);
      setLoadError(null);
    } catch (err) {
      if (seq !== requestRef.current) return;
      // 404 is "nothing bound yet", which is a starting point rather than a
      // failure — every other status is the server saying something went wrong.
      if (err instanceof ApiError && err.status === 404) {
        setDetails(null);
        setLoadError(null);
      } else {
        setLoadError(err instanceof Error ? err.message : null);
      }
    } finally {
      if (seq === requestRef.current) setLoading(false);
    }
  }, [repositoryId, subProjectPath]);

  useEffect(() => {
    void load();
  }, [load]);

  const refresh = async () => {
    setRefreshing(true);
    try {
      await load();
    } finally {
      setRefreshing(false);
    }
  };

  const ref = details?.ref;

  if (loading) {
    return (
      <Card className={cn("w-full space-y-4 p-6", className)}>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-24 w-full" />
      </Card>
    );
  }

  return (
    <Card className={cn("w-full space-y-5 p-6", className)}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 font-semibold">
            <Cloud className="h-4 w-4" aria-hidden />
            {t("projectAdmin.gcloudResource.title")}
          </h3>
          <p className="text-sm text-muted-foreground">{t("projectAdmin.gcloudResource.subtitle")}</p>
        </div>
        {details && (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="outline" onClick={() => setPickerOpen(true)}>
              <Link2 className="mr-2 h-4 w-4" aria-hidden />
              {t("projectAdmin.gcloudResource.changeResource")}
            </Button>
            <Button size="sm" variant="ghost" disabled={refreshing} onClick={() => void refresh()}>
              <RefreshCw className={cn("mr-2 h-4 w-4", refreshing && "animate-spin")} aria-hidden />
              {t("projectAdmin.gcloudResource.refresh")}
            </Button>
          </div>
        )}
      </div>

      {loadError && (
        <Notice variant="error" title={t("projectAdmin.gcloudResource.loadFailed")}>
          {loadError}
        </Notice>
      )}

      {!details && !loadError && (
        <EmptyState
          icon={Cloud}
          title={t("projectAdmin.gcloudResource.notBoundTitle")}
          description={t("projectAdmin.gcloudResource.notBoundDesc")}
          className="py-8"
          action={
            <Button size="sm" onClick={() => setPickerOpen(true)}>
              <Link2 className="mr-2 h-4 w-4" aria-hidden />
              {t("projectAdmin.gcloudResource.bindResource")}
            </Button>
          }
        />
      )}

      {details && ref && (
        <>
          <div className="grid gap-3 rounded-md border border-border/60 p-3 sm:grid-cols-2">
            <Field label={t("projectAdmin.gcloudResource.resourceName")}>
              <span className="font-medium">
                {ref.display_name || ref.name || t("projectAdmin.gcloudResource.notReported")}
              </span>
              <p className="break-all font-mono text-xs text-muted-foreground">{ref.name}</p>
            </Field>
            <Field label={t("projectAdmin.gcloudResource.type")}>
              <Badge variant="outline">{t(`projectAdmin.gcloudResource.types.${ref.type}`)}</Badge>
            </Field>
            <Field label={t("projectAdmin.gcloudResource.project")}>
              <span className="font-mono text-xs">{ref.project_id || t("projectAdmin.gcloudResource.notReported")}</span>
            </Field>
            <Field label={t("projectAdmin.gcloudResource.location")}>
              <span className="font-mono text-xs">{ref.location || t("projectAdmin.gcloudResource.notReported")}</span>
            </Field>
          </div>

          <Separator />

          {details.cloud_run ? (
            <CloudRunDetail service={details.cloud_run} />
          ) : details.gke_cluster ? (
            <GKEDetail cluster={details.gke_cluster} />
          ) : (
            <p className="text-sm text-muted-foreground">{t("projectAdmin.gcloudResource.noDetail")}</p>
          )}
        </>
      )}

      <GCloudResourcePickerDialog
        repositoryId={repositoryId}
        subProjectPath={subProjectPath}
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onBound={() => {
          setPickerOpen(false);
          void load();
        }}
      />
    </Card>
  );
}
