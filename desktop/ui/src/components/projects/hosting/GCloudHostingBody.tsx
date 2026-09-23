import { Cloud, Link2, RefreshCw, Unlink } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ApiError, api, type BoundGCloudResource } from "@/api";
import { GCloudResourcePickerDialog } from "@/components/admin/GCloudResourcePickerDialog";
import { ExternalLinkText, Field } from "@/components/projects/hosting/HostingFields";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Notice } from "@/components/ui/notice";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate } from "@/lib/utils";

// Google Cloud spells readiness as a condition string ("True"/"False") for
// Cloud Run and as an enum for GKE. Both are mapped to the same three badges
// so a reader does not have to learn two vocabularies.
function readyVariant(value: string): "success" | "warning" | "destructive" | "outline" {
  const normalized = value.trim().toLowerCase();
  if (normalized === "true" || normalized === "running") return "success";
  if (normalized === "false" || normalized === "degraded" || normalized === "error") return "destructive";
  if (normalized === "" || normalized === "unknown") return "outline";
  return "warning";
}

interface GCloudHostingBodyProps {
  repositoryId: string;
  /** "" (default) is the repository itself; a monorepo passes the sub-repo path. */
  subProjectPath?: string;
  onLinkChange?: () => void;
  /** Reports whether this scope is bound, so the frame can badge the provider
      without repeating the request this body already makes. */
  onStatusChange?: (linked: boolean) => void;
}

/**
 * The Google Cloud half of HostingPanel: which Cloud Run service or GKE
 * cluster this scope ships to, and what that resource is doing right now.
 *
 * The binding and the live read are told apart on purpose. The server answers
 * 200 with `detail_available: false` when the credential is gone or cannot
 * read the resource, because the binding is a fact about the repository that
 * outlives the key — so a missing detail is a notice here, never an error that
 * hides which resource is bound.
 */
export function GCloudHostingBody({
  repositoryId,
  subProjectPath = "",
  onLinkChange,
  onStatusChange,
}: GCloudHostingBodyProps) {
  const { t } = useI18n();

  const [bound, setBound] = useState<BoundGCloudResource | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [confirmUnbind, setConfirmUnbind] = useState(false);
  const [unbinding, setUnbinding] = useState(false);

  // Only the newest read may write state: a slow first pass must not land on
  // top of the refresh that overtook it.
  const requestRef = useRef(0);

  const load = useCallback(async () => {
    const seq = ++requestRef.current;
    try {
      const data = await api.gcloudResourceDetails(repositoryId, subProjectPath);
      if (seq !== requestRef.current) return;
      setBound(data);
      setLoadError(null);
      onStatusChange?.(true);
    } catch (err) {
      if (seq !== requestRef.current) return;
      // 404 is "nothing bound yet", which is a starting point rather than a
      // failure — every other status is the server saying something went wrong.
      if (err instanceof ApiError && err.status === 404) {
        setBound(null);
        setLoadError(null);
        onStatusChange?.(false);
      } else {
        setLoadError(err instanceof Error ? err.message : null);
      }
    } finally {
      if (seq === requestRef.current) setLoading(false);
    }
  }, [repositoryId, subProjectPath, onStatusChange]);

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

  const unbind = async () => {
    setUnbinding(true);
    try {
      await api.unbindGCloudResource(repositoryId, subProjectPath);
      setBound(null);
      setLoadError(null);
      onStatusChange?.(false);
      onLinkChange?.();
      toast.success(t("projectAdmin.hosting.gcloud.unbound"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setUnbinding(false);
    }
  };

  if (loading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  const binding = bound?.binding;
  const detail = bound?.detail;
  const cloudRun = detail?.cloud_run;
  const gke = detail?.gke_cluster;
  const resourceLabel = binding?.display_name || binding?.resource_name || "";
  const typeLabel =
    binding?.resource_type === "gke_cluster"
      ? t("projectAdmin.hosting.gcloud.typeGke")
      : t("projectAdmin.hosting.gcloud.typeCloudRun");

  return (
    <div className="space-y-4">
      {binding && (
        <div className="flex flex-wrap justify-end gap-2">
          <Button size="sm" variant="outline" onClick={() => setPickerOpen(true)}>
            <Link2 className="mr-2 h-4 w-4" aria-hidden />
            {t("projectAdmin.hosting.gcloud.changeResource")}
          </Button>
          <Button size="sm" variant="ghost" disabled={refreshing} onClick={() => void refresh()}>
            <RefreshCw className={cn("mr-2 h-4 w-4", refreshing && "animate-spin")} aria-hidden />
            {t("projectAdmin.hosting.refresh")}
          </Button>
          <Button size="sm" variant="ghost" disabled={unbinding} onClick={() => setConfirmUnbind(true)}>
            <Unlink className="mr-2 h-4 w-4" aria-hidden />
            {t("projectAdmin.hosting.gcloud.unbind")}
          </Button>
        </div>
      )}

      {loadError && (
        <Notice variant="error" title={t("projectAdmin.hosting.gcloud.loadFailed")}>
          {loadError}
        </Notice>
      )}

      {!binding && !loadError && (
        <div className="flex flex-wrap items-center justify-between gap-4 rounded-md border border-dashed border-border/60 p-4">
          <div className="flex min-w-0 items-start gap-3">
            <Cloud className="mt-0.5 h-5 w-5 shrink-0 text-muted-foreground" aria-hidden />
            <div className="min-w-0">
              <p className="text-sm font-medium">{t("projectAdmin.hosting.gcloud.notBoundTitle")}</p>
              <p className="text-caption text-muted-foreground">{t("projectAdmin.hosting.gcloud.notBoundDesc")}</p>
            </div>
          </div>
          <Button size="sm" onClick={() => setPickerOpen(true)}>
            <Link2 className="mr-2 h-4 w-4" aria-hidden />
            {t("projectAdmin.hosting.gcloud.bindResource")}
          </Button>
        </div>
      )}

      {binding && (
        <>
          {bound?.detail_available === false && (
            <Notice variant="info" title={t("projectAdmin.hosting.gcloud.detailUnavailableTitle")}>
              {bound.reason === "not_connected"
                ? t("projectAdmin.hosting.gcloud.detailNotConnected")
                : t("projectAdmin.hosting.gcloud.detailUnsupported")}
            </Notice>
          )}

          <div className="grid gap-3 rounded-md border border-border/60 p-3 sm:grid-cols-2">
            <Field label={t("projectAdmin.hosting.gcloud.resource")}>
              <span className="font-medium">{resourceLabel}</span>
              <p className="truncate font-mono text-xs text-muted-foreground">{binding.resource_name}</p>
            </Field>
            <Field label={t("projectAdmin.hosting.gcloud.kind")}>
              <Badge variant="outline">{typeLabel}</Badge>
            </Field>
            <Field label={t("projectAdmin.hosting.gcloud.project")}>
              {binding.project_id || <span className="text-muted-foreground">{t("projectAdmin.hosting.notReported")}</span>}
            </Field>
            <Field label={t("projectAdmin.hosting.gcloud.location")}>
              {binding.location || <span className="text-muted-foreground">{t("projectAdmin.hosting.notReported")}</span>}
            </Field>
          </div>

          {cloudRun && (
            <>
              <Separator />
              <div className="space-y-3 rounded-md border border-border/60 p-3">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <h4 className="text-sm font-medium">{t("projectAdmin.hosting.gcloud.cloudRunTitle")}</h4>
                  <Badge variant={readyVariant(cloudRun.ready)}>
                    {cloudRun.ready_reason || cloudRun.ready || t("projectAdmin.hosting.gcloud.stateUnknown")}
                  </Badge>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  {detail?.ref.uri && (
                    <Field label={t("projectAdmin.hosting.gcloud.serviceUrl")}>
                      <ExternalLinkText href={detail.ref.uri} label={detail.ref.uri} mono />
                    </Field>
                  )}
                  {cloudRun.image && (
                    <Field label={t("projectAdmin.hosting.gcloud.image")}>
                      <span className="truncate font-mono text-xs">{cloudRun.image}</span>
                    </Field>
                  )}
                  {cloudRun.latest_ready_revision && (
                    <Field label={t("projectAdmin.hosting.gcloud.latestReady")}>
                      <span className="font-mono text-xs">{cloudRun.latest_ready_revision}</span>
                    </Field>
                  )}
                  {cloudRun.update_time && (
                    <Field label={t("projectAdmin.hosting.gcloud.updatedAt")}>{formatDate(cloudRun.update_time)}</Field>
                  )}
                </div>

                {cloudRun.traffic.length > 0 && (
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">{t("projectAdmin.hosting.gcloud.traffic")}</p>
                    <div className="flex flex-wrap gap-1">
                      {cloudRun.traffic.map((split) => (
                        <Badge key={`${split.revision}-${split.tag ?? ""}`} variant="outline" className="font-mono text-micro">
                          {split.percent}% · {split.revision || split.tag}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}

                {cloudRun.ready_message && <p className="text-sm text-muted-foreground">{cloudRun.ready_message}</p>}
              </div>
            </>
          )}

          {gke && (
            <>
              <Separator />
              <div className="space-y-3 rounded-md border border-border/60 p-3">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <h4 className="text-sm font-medium">{t("projectAdmin.hosting.gcloud.gkeTitle")}</h4>
                  <Badge variant={readyVariant(gke.status)}>
                    {gke.status || t("projectAdmin.hosting.gcloud.stateUnknown")}
                  </Badge>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  {gke.master_version && (
                    <Field label={t("projectAdmin.hosting.gcloud.masterVersion")}>
                      <span className="font-mono text-xs">{gke.master_version}</span>
                    </Field>
                  )}
                  <Field label={t("projectAdmin.hosting.gcloud.nodeCount")}>{gke.node_count}</Field>
                  <Field label={t("projectAdmin.hosting.gcloud.autopilot")}>
                    {gke.autopilot ? t("projectAdmin.hosting.yes") : t("projectAdmin.hosting.no")}
                  </Field>
                  <Field label={t("projectAdmin.hosting.gcloud.privateEndpoint")}>
                    {gke.private_endpoint ? t("projectAdmin.hosting.yes") : t("projectAdmin.hosting.no")}
                  </Field>
                </div>

                {(gke.node_pools?.length ?? 0) > 0 && (
                  <div className="space-y-1">
                    <p className="text-xs text-muted-foreground">{t("projectAdmin.hosting.gcloud.nodePools")}</p>
                    <div className="flex flex-wrap gap-1">
                      {(gke.node_pools ?? []).map((pool) => (
                        <Badge key={pool.name} variant="outline" className="font-mono text-micro">
                          {pool.name} · {pool.node_count}
                        </Badge>
                      ))}
                    </div>
                  </div>
                )}

                {gke.status_message && <p className="text-sm text-muted-foreground">{gke.status_message}</p>}
                {/* Prose from the server naming which wall blocked this cluster. */}
                {gke.workloads_note && <p className="text-sm text-muted-foreground">{gke.workloads_note}</p>}
              </div>
            </>
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
          onLinkChange?.();
          void load();
        }}
      />

      <ConfirmDialog
        open={confirmUnbind}
        onOpenChange={setConfirmUnbind}
        title={t("projectAdmin.hosting.gcloud.unbindTitle")}
        description={t("projectAdmin.hosting.gcloud.unbindDescription", {
          name: resourceLabel || t("projectAdmin.hosting.notReported"),
        })}
        confirmLabel={t("projectAdmin.hosting.gcloud.unbind")}
        loading={unbinding}
        onConfirm={() => void unbind()}
      />
    </div>
  );
}
