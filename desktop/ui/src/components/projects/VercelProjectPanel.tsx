import { ExternalLink, Link2, RefreshCw, Triangle, Unlink } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ApiError, api, type VercelDeployment, type VercelProjectDetails } from "@/api";
import { VercelProjectPickerDialog } from "@/components/admin/VercelProjectPickerDialog";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate } from "@/lib/utils";

type DeploymentState = "ready" | "building" | "queued" | "initializing" | "error" | "canceled";

const DEPLOYMENT_STATES: Record<string, DeploymentState> = {
  ready: "ready",
  building: "building",
  queued: "queued",
  initializing: "initializing",
  error: "error",
  failed: "error",
  canceled: "canceled",
  cancelled: "canceled",
};

const STATE_VARIANT: Record<DeploymentState, BadgeProps["variant"]> = {
  ready: "success",
  building: "warning",
  queued: "warning",
  initializing: "warning",
  error: "destructive",
  canceled: "outline",
};

function deploymentState(raw?: string): DeploymentState | undefined {
  return raw ? DEPLOYMENT_STATES[raw.trim().toLowerCase()] : undefined;
}

function shortSha(sha: string): string {
  return sha.length > 7 ? sha.slice(0, 7) : sha;
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
      aria-label={`${label} (${t("projectAdmin.vercelProject.openInNewTab")})`}
      className={cn("inline-flex min-w-0 items-center gap-1 underline underline-offset-2", mono && "font-mono text-xs")}
    >
      <span className="truncate">{label}</span>
      <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
    </a>
  );
}

interface DeploymentBlockProps {
  title: string;
  hint?: string;
  deployment: VercelDeployment;
}

function DeploymentBlock({ title, hint, deployment }: DeploymentBlockProps) {
  const { t } = useI18n();
  const state = deploymentState(deployment.state);
  const stateLabel = state ? t(`projectAdmin.vercelProject.states.${state}`) : deployment.state?.trim();
  const finishedAt = deployment.ready_at || deployment.created_at;
  const target = deployment.target?.trim().toLowerCase();
  const targetLabel =
    target === "production" || target === "preview"
      ? t(`projectAdmin.vercelProject.targets.${target}`)
      : deployment.target?.trim();

  return (
    <div className="space-y-3 rounded-md border border-border/60 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <h4 className="text-sm font-medium">{title}</h4>
          {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
        </div>
        <Badge variant={state ? STATE_VARIANT[state] : "outline"}>
          {stateLabel || t("projectAdmin.vercelProject.states.unknown")}
        </Badge>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        {targetLabel && <Field label={t("projectAdmin.vercelProject.target")}>{targetLabel}</Field>}
        {finishedAt && <Field label={t("projectAdmin.vercelProject.deployedAt")}>{formatDate(finishedAt)}</Field>}
        {deployment.url && (
          <Field label={t("projectAdmin.vercelProject.deploymentUrl")}>
            <ExternalLinkText href={`https://${deployment.url.replace(/^https?:\/\//, "")}`} label={deployment.url} mono />
          </Field>
        )}
        {deployment.commit_sha && (
          <Field label={t("projectAdmin.vercelProject.commit")}>
            <span className="font-mono text-xs">{shortSha(deployment.commit_sha)}</span>
            {deployment.commit_ref && <span className="text-xs text-muted-foreground"> · {deployment.commit_ref}</span>}
            {deployment.commit_message && (
              <p className="truncate text-xs text-muted-foreground">{deployment.commit_message}</p>
            )}
          </Field>
        )}
      </div>

      {(deployment.error_message || deployment.error_code) && (
        <p className="text-sm">
          <span className="text-xs text-muted-foreground">{t("projectAdmin.vercelProject.errorLabel")}: </span>
          {deployment.error_message || deployment.error_code}
          {deployment.error_message && deployment.error_code && (
            <span className="font-mono text-xs text-muted-foreground"> ({deployment.error_code})</span>
          )}
        </p>
      )}

      {deployment.inspector_url && (
        <ExternalLinkText href={deployment.inspector_url} label={t("projectAdmin.vercelProject.openInspector")} />
      )}
    </div>
  );
}

interface VercelProjectPanelProps {
  repositoryId: string;
  /** "" (default) is the repository itself; a monorepo passes the sub-repo path. */
  subProjectPath?: string;
  className?: string;
}

/**
 * VercelProjectPanel — the hosting half of a frontend scope's settings: which
 * Vercel project it deploys to, where that lands in production, and what the
 * last builds did.
 *
 * The last failure gets a block of its own rather than being folded into the
 * latest deployment, because a build that broke and was then fixed is still
 * something the reader has to know about; the server reports the two apart for
 * exactly that reason (see `VercelProjectDetails`).
 */
export function VercelProjectPanel({ repositoryId, subProjectPath = "", className }: VercelProjectPanelProps) {
  const { t } = useI18n();

  const [details, setDetails] = useState<VercelProjectDetails | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [confirmUnlink, setConfirmUnlink] = useState(false);
  const [unlinking, setUnlinking] = useState(false);

  // Only the newest read may write state: a slow first pass must not land on
  // top of the refresh that overtook it.
  const requestRef = useRef(0);

  const load = useCallback(async () => {
    const seq = ++requestRef.current;
    try {
      const data = await api.vercelProjectDetails(repositoryId, subProjectPath);
      if (seq !== requestRef.current) return;
      setDetails(data);
      setLoadError(null);
    } catch (err) {
      if (seq !== requestRef.current) return;
      // 404 is "nothing linked yet", which is a starting point rather than a
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

  const unlink = async () => {
    setUnlinking(true);
    try {
      await api.unlinkVercelProject(repositoryId, subProjectPath);
      setDetails(null);
      setLoadError(null);
      toast.success(t("projectAdmin.vercelProject.unlinked"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setUnlinking(false);
    }
  };

  const link = details?.link;
  const projectName = link?.project_name || link?.project_id || "";
  const productionUrl = details?.production_url || link?.production_url || "";
  const framework = details?.framework || link?.framework || "";
  const rootDirectory = details?.root_directory || link?.root_directory || "";
  const failed = details?.last_failed_deployment;
  // A project that is broken right now carries the same deployment in both
  // fields; printing it twice would read as two separate failures.
  const showFailedApart = Boolean(failed && failed.id !== details?.latest_deployment?.id);

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
            <Triangle className="h-4 w-4" aria-hidden />
            {t("projectAdmin.vercelProject.title")}
          </h3>
          <p className="text-sm text-muted-foreground">{t("projectAdmin.vercelProject.subtitle")}</p>
        </div>
        {details && (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="outline" onClick={() => setPickerOpen(true)}>
              <Link2 className="mr-2 h-4 w-4" aria-hidden />
              {t("projectAdmin.vercelProject.changeProject")}
            </Button>
            <Button size="sm" variant="ghost" disabled={refreshing} onClick={() => void refresh()}>
              <RefreshCw className={cn("mr-2 h-4 w-4", refreshing && "animate-spin")} aria-hidden />
              {t("projectAdmin.vercelProject.refresh")}
            </Button>
            <Button size="sm" variant="ghost" disabled={unlinking} onClick={() => setConfirmUnlink(true)}>
              <Unlink className="mr-2 h-4 w-4" aria-hidden />
              {t("projectAdmin.vercelProject.unlink")}
            </Button>
          </div>
        )}
      </div>

      {loadError && (
        <Notice variant="error" title={t("projectAdmin.vercelProject.loadFailed")}>
          {loadError}
        </Notice>
      )}

      {!details && !loadError && (
        <EmptyState
          icon={Triangle}
          title={t("projectAdmin.vercelProject.notLinkedTitle")}
          description={t("projectAdmin.vercelProject.notLinkedDesc")}
          className="py-8"
          action={
            <Button size="sm" onClick={() => setPickerOpen(true)}>
              <Link2 className="mr-2 h-4 w-4" aria-hidden />
              {t("projectAdmin.vercelProject.linkProject")}
            </Button>
          }
        />
      )}

      {details && link && (
        <>
          {details.warnings && details.warnings.length > 0 && (
            <Notice variant="info" title={t("projectAdmin.vercelProject.warningsTitle")}>
              <p>{t("projectAdmin.vercelProject.warningsBody")}</p>
              <ul className="mt-1 list-disc space-y-0.5 pl-4">
                {details.warnings.map((warning) => (
                  <li key={warning}>{warning}</li>
                ))}
              </ul>
            </Notice>
          )}

          <div className="grid gap-3 rounded-md border border-border/60 p-3 sm:grid-cols-2">
            <Field label={t("projectAdmin.vercelProject.project")}>
              <span className="font-medium">{projectName || t("projectAdmin.vercelProject.notReported")}</span>
              {link.team_slug && <p className="text-xs text-muted-foreground">{link.team_slug}</p>}
            </Field>
            <Field label={t("projectAdmin.vercelProject.productionUrl")}>
              {productionUrl ? (
                <ExternalLinkText
                  href={`https://${productionUrl.replace(/^https?:\/\//, "")}`}
                  label={productionUrl}
                  mono
                />
              ) : (
                <span className="text-muted-foreground">{t("projectAdmin.vercelProject.noProductionUrl")}</span>
              )}
            </Field>
            <Field label={t("projectAdmin.vercelProject.framework")}>
              {framework || <span className="text-muted-foreground">{t("projectAdmin.vercelProject.notReported")}</span>}
            </Field>
            <Field label={t("projectAdmin.vercelProject.rootDirectory")}>
              <span className="font-mono text-xs">{rootDirectory || "/"}</span>
            </Field>
          </div>

          <Separator />

          <div className="space-y-3">
            {details.latest_deployment ? (
              <DeploymentBlock
                title={t("projectAdmin.vercelProject.latestTitle")}
                deployment={details.latest_deployment}
              />
            ) : (
              <p className="text-sm text-muted-foreground">{t("projectAdmin.vercelProject.latestEmpty")}</p>
            )}

            {showFailedApart && failed && (
              <DeploymentBlock
                title={t("projectAdmin.vercelProject.lastFailedTitle")}
                hint={t("projectAdmin.vercelProject.lastFailedHint")}
                deployment={failed}
              />
            )}
          </div>
        </>
      )}

      <VercelProjectPickerDialog
        repositoryId={repositoryId}
        subProjectPath={subProjectPath}
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onLinked={() => {
          setPickerOpen(false);
          void load();
        }}
      />

      <ConfirmDialog
        open={confirmUnlink}
        onOpenChange={setConfirmUnlink}
        title={t("projectAdmin.vercelProject.unlinkTitle")}
        description={t("projectAdmin.vercelProject.unlinkDescription", {
          name: projectName || t("projectAdmin.vercelProject.notReported"),
        })}
        confirmLabel={t("projectAdmin.vercelProject.unlink")}
        loading={unlinking}
        onConfirm={() => void unlink()}
      />
    </Card>
  );
}
