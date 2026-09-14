import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type DeploymentRun, type MatrixCell, type MatrixRepo } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { RunHistoryList } from "@/components/operations/RunHistoryList";
import { useI18n } from "@/hooks/useI18n";

interface DeploymentDetailDrawerProps {
  repo: MatrixRepo;
  cell: MatrixCell;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onActed: () => void;
}

type PendingAction = "deploy" | "rollback" | null;

/**
 * DeploymentDetailDrawer — where a user actually triggers a deploy or rolls
 * one back, and sees the run history behind that decision. Follows
 * TaskDetailDrawer's Dialog-as-drawer structure (bordered header, scrollable
 * body) and StoreReleaseControls' confirm pattern (typed confirmPhrase for
 * every production-class action). The ref field has no server-side default
 * to fall back on (domain.Repository persists no default branch — see
 * api.ts), so it is always editable here, pre-filled with "main".
 */
export function DeploymentDetailDrawer({ repo, cell, open, onOpenChange, onActed }: DeploymentDetailDrawerProps) {
  const { t } = useI18n();
  const [ref, setRef] = useState("main");
  const [pending, setPending] = useState<PendingAction>(null);
  const [busy, setBusy] = useState(false);
  const [runs, setRuns] = useState<DeploymentRun[]>([]);

  const loadRuns = useCallback(async () => {
    try {
      setRuns((await api.listDeployRuns(repo.id, cell.env)) ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    }
  }, [repo.id, cell.env, t]);

  // Reset the ref draft and reload history whenever a different cell is
  // opened — a stale "master" typed for one repo must not leak into the
  // next repo's dialog.
  useEffect(() => {
    if (!open) return;
    setRef("main");
    void loadRuns();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, repo.id, cell.env]);

  // Rollback is always production-class, and prod is production-class by
  // definition — mirrors the server's own rule (deployops.productionClass).
  const needsPhrase = pending === "rollback" || cell.env === "prod";

  const handleConfirm = async () => {
    if (!pending) return;
    setBusy(true);
    try {
      if (pending === "deploy") {
        await api.dispatchDeploy(repo.id, cell.env, {
          ref,
          confirm: needsPhrase ? repo.name : undefined,
        });
        toast.success(t("operations.deployments.deploySucceeded"));
      } else {
        await api.rollbackDeploy(repo.id, cell.env, repo.name);
        toast.success(t("operations.deployments.rollbackSucceeded"));
      }
      onActed();
      await loadRuns();
    } catch (err) {
      // Swallowed, not re-thrown (mirrors StoreReleaseControls' `run`
      // helper): the confirm dialog itself closes either way, but this
      // drawer's own open state is never touched here, so it stays open
      // for the user to see the error and retry the action.
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="flex h-[85vh] max-w-2xl flex-col gap-0 overflow-hidden p-0">
          <DialogHeader className="border-b border-border px-6 py-4">
            <div className="flex flex-wrap items-center gap-2 pr-6">
              <DialogTitle className="text-left">{repo.name}</DialogTitle>
              <Badge variant="outline" className="capitalize">
                {cell.env}
              </Badge>
            </div>
            <div className="mt-2 flex flex-wrap items-center gap-2 text-left">
              <Badge variant="secondary">{cell.provider}</Badge>
              {cell.health_url && (
                <a
                  href={cell.health_url}
                  target="_blank"
                  rel="noreferrer"
                  className="text-xs text-primary underline underline-offset-2"
                >
                  {cell.health_url}
                </a>
              )}
            </div>
          </DialogHeader>

          <ScrollArea className="min-h-0 flex-1">
            <div className="space-y-6 px-6 py-5">
              <section className="space-y-2">
                <Label htmlFor="deploy-ref">{t("operations.deployments.gitRef")}</Label>
                <Input id="deploy-ref" value={ref} onChange={(e) => setRef(e.target.value)} autoComplete="off" />
                <p className="text-xs text-muted-foreground">{t("operations.deployments.gitRefHint")}</p>
              </section>

              <section className="flex flex-wrap gap-4">
                <div className="space-y-1">
                  <Button
                    disabled={!cell.dispatchable}
                    title={!cell.dispatchable ? t("operations.deployments.errors.noWorkflowMapping") : undefined}
                    onClick={() => setPending("deploy")}
                  >
                    {t("operations.deployments.deploy")}
                  </Button>
                  {!cell.dispatchable && (
                    <p className="text-xs text-destructive">{t("operations.deployments.errors.noWorkflowMapping")}</p>
                  )}
                </div>
                <div className="space-y-1">
                  <Button
                    variant="outline"
                    disabled={!cell.rollback_sha}
                    title={!cell.rollback_sha ? t("operations.deployments.errors.noRollbackTarget") : undefined}
                    onClick={() => setPending("rollback")}
                  >
                    {t("operations.deployments.rollback")}
                  </Button>
                  {!cell.rollback_sha && (
                    <p className="text-xs text-destructive">{t("operations.deployments.errors.noRollbackTarget")}</p>
                  )}
                </div>
              </section>

              <Separator />

              <section className="space-y-3">
                <Label className="text-muted-foreground">{t("operations.deployments.history")}</Label>
                <RunHistoryList runs={runs} />
              </section>
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(o) => !o && setPending(null)}
        title={
          pending === "rollback"
            ? t("operations.deployments.rollbackTitle", { env: cell.env })
            : t("operations.deployments.deployTitle", { env: cell.env })
        }
        description={
          pending === "rollback"
            ? t("operations.deployments.rollbackDescription", {
                repo: repo.name,
                env: cell.env,
                sha: cell.rollback_sha.slice(0, 7),
              })
            : t("operations.deployments.deployDescription", { repo: repo.name, env: cell.env, ref })
        }
        confirmLabel={pending === "rollback" ? t("operations.deployments.rollback") : t("operations.deployments.deploy")}
        confirmPhrase={needsPhrase ? repo.name : undefined}
        loading={busy}
        onConfirm={handleConfirm}
      />
    </>
  );
}
