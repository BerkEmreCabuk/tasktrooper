import { ExternalLink, PackageSearch, Plug, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type VercelProject, type VercelProjectLink, type VercelProjectListing } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Spinner } from "@/components/ui/spinner";
import { tStatic, useI18n } from "@/hooks/useI18n";

interface VercelProjectListingState {
  listing: VercelProjectListing | null;
  loading: boolean;
  /** The failed call's own sentence, never a stand-in for an empty account. */
  error: string;
  reload: () => void;
}

/**
 * Reads the projects the saved Vercel connection can see, on demand.
 *
 * Distinct from the settings card's own project fetch: this one answers with
 * a verdict (`listing_available` + `reason`) rather than a bare array, which
 * is what keeps "cannot enumerate" apart from "this scope is empty".
 */
export function useVercelProjectListing(enabled: boolean): VercelProjectListingState {
  const [answer, setAnswer] = useState<{ listing: VercelProjectListing | null; error: string } | null>(null);
  const [inFlight, setInFlight] = useState(false);
  const requestRef = useRef(0);

  const load = useCallback(async () => {
    if (!enabled) return;
    const seq = ++requestRef.current;
    setInFlight(true);
    try {
      const listing = await api.listVercelProjects();
      if (seq !== requestRef.current) return;
      setAnswer({ listing, error: "" });
    } catch (err) {
      if (seq !== requestRef.current) return;
      setAnswer({
        listing: null,
        error: err instanceof Error ? err.message : tStatic("settingsPages.integrations.vercelPicker.loadFailed"),
      });
    } finally {
      if (seq === requestRef.current) setInFlight(false);
    }
  }, [enabled]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (enabled) return;
    // Going quiet drops whatever is in flight: re-opening asks again rather
    // than showing the previous scope's answer for a frame.
    requestRef.current += 1;
    setAnswer(null);
    setInFlight(false);
  }, [enabled]);

  return {
    listing: answer?.listing ?? null,
    loading: enabled && (answer === null || inFlight),
    error: answer?.error ?? "",
    reload: () => void load(),
  };
}

function RetryButton({ onClick, disabled }: { onClick: () => void; disabled: boolean }) {
  const { t } = useI18n();
  return (
    <Button variant="outline" size="sm" className="gap-2" onClick={onClick} disabled={disabled}>
      <RefreshCw className={`h-3.5 w-3.5 ${disabled ? "animate-spin" : ""}`} />
      {t("settingsPages.integrations.vercelPicker.retry")}
    </Button>
  );
}

function ProjectIdentity({ project }: { project: VercelProject }) {
  return (
    <div className="flex min-w-0 flex-1 items-start justify-between gap-2">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{project.name}</p>
        {(project.root_directory || project.production_url) && (
          <p className="truncate font-mono text-xs text-muted-foreground">
            {project.root_directory ?? ""}
            {project.root_directory && project.production_url ? " · " : ""}
            {project.production_url ?? ""}
          </p>
        )}
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
        {project.framework ? <Badge variant="outline">{project.framework}</Badge> : null}
      </div>
    </div>
  );
}

export interface VercelProjectPickerDialogProps {
  repositoryId: string;
  subProjectPath?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onLinked?: (link: VercelProjectLink) => void;
}

/**
 * Binds a repository scope to one project in the connected Vercel account.
 *
 * Three answers, and which one is offered is the server's, not a guess from an
 * empty array: `not_connected` means no token at all, so a pasted project ID
 * could not be checked against anything and Integrations is the only way
 * forward; `listing_unsupported` means the token is fine but this scope will
 * not enumerate, which is exactly when pasting the ID is the way through; and
 * an account that genuinely holds no projects gets its own empty state.
 */
export function VercelProjectPickerDialog({
  repositoryId,
  subProjectPath,
  open,
  onOpenChange,
  onLinked,
}: VercelProjectPickerDialogProps) {
  const { t } = useI18n();
  const { listing, loading, error, reload } = useVercelProjectListing(open);
  const [selected, setSelected] = useState("");
  const [manual, setManual] = useState("");
  const [linking, setLinking] = useState(false);

  // A reopen starts over: the previous pick belongs to the previous question.
  useEffect(() => {
    if (!open) return;
    setSelected("");
    setManual("");
  }, [open]);

  const projects = listing?.projects ?? [];
  const canEnumerate = listing?.listing_available ?? false;
  const notConnected = listing?.reason === "not_connected";
  const chosen = canEnumerate ? projects.find((p) => p.id === selected) : undefined;
  const manualId = manual.trim();
  const canLink = notConnected ? false : canEnumerate ? Boolean(chosen) : manualId !== "";

  const link = useCallback(async () => {
    const projectId = chosen?.id ?? manualId;
    if (!projectId) return;
    setLinking(true);
    try {
      const saved = await api.linkVercelProject(repositoryId, projectId, subProjectPath ?? "");
      toast.success(t("settingsPages.integrations.vercelPicker.linked"));
      onLinked?.(saved);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.integrations.vercelPicker.linkFailed"));
    } finally {
      setLinking(false);
    }
  }, [chosen, manualId, repositoryId, subProjectPath, onLinked, onOpenChange, t]);

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("settingsPages.integrations.vercelPicker.title")}
      description={t("settingsPages.integrations.vercelPicker.description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={linking}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void link()} disabled={linking || !canLink}>
            {linking
              ? t("settingsPages.integrations.vercelPicker.linking")
              : t("settingsPages.integrations.vercelPicker.link")}
          </Button>
        </>
      }
    >
      {loading ? (
        <div className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
          <Spinner size="sm" />
          {t("settingsPages.integrations.vercelPicker.loading")}
        </div>
      ) : error ? (
        <div className="space-y-3">
          <Notice variant="error" title={t("settingsPages.integrations.vercelPicker.loadFailed")}>
            {error}
          </Notice>
          <RetryButton onClick={reload} disabled={loading} />
        </div>
      ) : notConnected ? (
        <EmptyState
          className="py-6"
          icon={Plug}
          title={t("settingsPages.integrations.vercelPicker.notConnectedTitle")}
          description={t("settingsPages.integrations.vercelPicker.notConnectedBody")}
          action={
            <Button asChild>
              <Link to="/settings/integrations">
                {t("settingsPages.integrations.vercelPicker.notConnectedAction")}
              </Link>
            </Button>
          }
        />
      ) : canEnumerate ? (
        projects.length === 0 ? (
          <div className="space-y-3">
            <EmptyState
              className="py-6"
              icon={PackageSearch}
              title={t("settingsPages.integrations.vercelPicker.emptyTitle")}
              description={t("settingsPages.integrations.vercelPicker.emptyBody")}
            />
            <div className="flex justify-center">
              <RetryButton onClick={reload} disabled={loading} />
            </div>
          </div>
        ) : (
          <fieldset className="rounded-lg border border-border">
            {/* sr-only legend, rows in their own wrapper: `divide-y` on the
                fieldset would count the legend as a child and draw a stray
                rule above the first project. */}
            <legend className="sr-only">{t("settingsPages.integrations.vercelPicker.projectsLabel")}</legend>
            <div className="divide-y divide-border">
              {projects.map((project) => (
                <label
                  key={project.id}
                  className="flex cursor-pointer items-start gap-3 px-3 py-2.5 transition-colors hover:bg-muted/50 has-[:checked]:bg-muted"
                >
                  <input
                    type="radio"
                    name="vercel-project-choice"
                    className="mt-1 h-4 w-4 shrink-0 accent-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    checked={selected === project.id}
                    onChange={() => setSelected(project.id)}
                  />
                  <ProjectIdentity project={project} />
                </label>
              ))}
            </div>
          </fieldset>
        )
      ) : (
        <div className="space-y-3">
          <Notice variant="info" title={t("settingsPages.integrations.vercelPicker.unavailableTitle")}>
            {t("settingsPages.integrations.vercelPicker.unavailableBody")}
          </Notice>
          <div className="space-y-1">
            <Label htmlFor="vercel-project-id">{t("settingsPages.integrations.vercelPicker.manualLabel")}</Label>
            <Input
              id="vercel-project-id"
              value={manual}
              onChange={(e) => setManual(e.target.value)}
              placeholder={t("settingsPages.integrations.vercelPicker.manualPlaceholder")}
              className="font-mono text-xs"
            />
            <p className="flex items-center gap-1 text-xs text-muted-foreground">
              <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
              {t("settingsPages.integrations.vercelPicker.manualHint")}
            </p>
          </div>
          <RetryButton onClick={reload} disabled={loading} />
        </div>
      )}
    </FormDialog>
  );
}
