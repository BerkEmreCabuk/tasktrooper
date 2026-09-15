import { Cloud } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type GCloudCredentialView } from "@/api";
import { GCloudResourcesBrowser } from "@/components/admin/GCloudResourcePickerDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { tStatic, useI18n } from "@/hooks/useI18n";
import { formatDate } from "@/lib/utils";

/**
 * Settings → Integrations: the operator's read-only Google Cloud service
 * account, and the proof it reaches their project.
 *
 * The key behaves like the store credentials next to it — the API never reads
 * the stored JSON back, so the textarea always starts empty, every save
 * replaces it in full, and a key Google Cloud refuses is not kept. What *is*
 * kept in the clear is `client_email` and `project_id`: they exist so this
 * card can name which connection is saved, which is the one thing an
 * encrypted blob cannot answer.
 */
export function GoogleCloudCard() {
  const { t } = useI18n();
  const [credential, setCredential] = useState<GCloudCredentialView | null>(null);
  const [loading, setLoading] = useState(true);
  const [json, setJson] = useState("");
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Not keyed on `t`: a language switch must not refire this and blank the
  // card while the vault is re-read.
  const load = useCallback(async () => {
    try {
      setCredential(await api.getGCloudCredential());
    } catch (err) {
      setCredential(null);
      toast.error(err instanceof Error ? err.message : tStatic("settingsPages.integrations.gcloud.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const save = useCallback(async () => {
    setBusy(true);
    try {
      // Trimmed on submit for the same reason the store forms trim: JSON
      // pasted out of a console carries whitespace that would otherwise reach
      // Google verbatim and fail parsing for no visible reason.
      setCredential(await api.saveGCloudCredential(json.trim()));
      setJson("");
      toast.success(t("settingsPages.integrations.gcloud.saved"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  }, [json, t]);

  const remove = useCallback(async () => {
    setBusy(true);
    try {
      await api.deleteGCloudCredential();
      toast.success(t("settingsPages.integrations.gcloud.deleted"));
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
      setConfirmDelete(false);
    }
  }, [load, t]);

  const connected = credential?.connected ?? false;

  return (
    <Card className="mt-4 w-full space-y-3 p-6">
      <Label className="flex items-center gap-2">
        <Cloud className="h-4 w-4" />
        {t("settingsPages.integrations.gcloud.title")}
      </Label>
      <p className="text-sm text-muted-foreground">{t("settingsPages.integrations.gcloud.description")}</p>

      {loading ? (
        <Skeleton className="h-6 w-40" />
      ) : credential === null ? (
        <p className="text-sm text-muted-foreground">{t("settingsPages.integrations.gcloud.statusUnavailable")}</p>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          {connected ? (
            <Badge variant="success">{t("settingsPages.integrations.gcloud.configured")}</Badge>
          ) : (
            <Badge variant="outline">{t("settingsPages.integrations.gcloud.notConfigured")}</Badge>
          )}
          {connected && credential.updated_at && (
            <span className="text-xs text-muted-foreground">
              {t("settingsPages.integrations.gcloud.updated", { date: formatDate(credential.updated_at) })}
            </span>
          )}
        </div>
      )}

      {connected && (credential?.client_email || credential?.project_id) && (
        <div className="space-y-1 rounded-md border border-border/60 px-3 py-2.5">
          {credential.client_email && (
            <p className="flex flex-wrap items-baseline gap-1.5 text-sm">
              <span className="text-muted-foreground">{t("settingsPages.integrations.gcloud.accountLabel")}:</span>
              <span className="break-all font-mono text-xs">{credential.client_email}</span>
            </p>
          )}
          {credential.project_id && (
            <p className="flex flex-wrap items-baseline gap-1.5 text-sm">
              <span className="text-muted-foreground">{t("settingsPages.integrations.gcloud.projectLabel")}:</span>
              <span className="break-all font-mono text-xs">{credential.project_id}</span>
            </p>
          )}
          <p className="text-xs text-muted-foreground">{t("settingsPages.integrations.gcloud.identityHint")}</p>
        </div>
      )}

      <div className="space-y-1">
        <Label htmlFor="gcloud-sa-json">{t("settingsPages.integrations.gcloud.jsonLabel")}</Label>
        <Textarea
          id="gcloud-sa-json"
          value={json}
          onChange={(e) => setJson(e.target.value)}
          placeholder={t("settingsPages.integrations.gcloud.jsonPlaceholder")}
          className="min-h-[100px] font-mono text-xs"
        />
      </div>

      <p className="text-xs text-muted-foreground">{t("settingsPages.integrations.gcloud.replaceHint")}</p>

      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={busy || json.trim() === ""} onClick={() => void save()}>
          {t("settingsPages.integrations.gcloud.save")}
        </Button>
        {connected && (
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmDelete(true)}>
            {t("settingsPages.integrations.gcloud.delete")}
          </Button>
        )}
      </div>

      {/* Remounted when the saved key changes: a browser left open on the old
          service account's answer would otherwise describe a key that is gone. */}
      <GCloudResourcesBrowser key={credential?.updated_at ?? "none"} configured={connected} />

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={(open) => !open && setConfirmDelete(false)}
        title={t("settingsPages.integrations.gcloud.deleteTitle")}
        description={t("settingsPages.integrations.gcloud.deleteDescription")}
        confirmLabel={t("settingsPages.integrations.gcloud.delete")}
        loading={busy}
        onConfirm={remove}
      />
    </Card>
  );
}
