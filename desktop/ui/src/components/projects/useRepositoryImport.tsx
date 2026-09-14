import { useCallback, useRef, useState } from "react";
import { api, type Repository } from "@/api";
import { InitialSetupDialog } from "@/components/projects/InitialSetupDialog";
import type { RepositoryAddMethod } from "@/components/projects/ProjectRepositoriesSection";
import { type AnalyzePhase, RepositoryAnalyzingDialog } from "@/components/projects/RepositoryAnalyzingDialog";
import {
  CreateRepositoryDialog,
  ImportGitHubDialog,
  OpenRepositoryDialog,
} from "@/components/projects/RepositoryDialogs";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

/**
 * The one-repository-at-a-time import wizard, as state rather than as markup.
 *
 * Extracted out of `ProjectsPage` because the guided first-run sequence has to
 * run the SAME wizard for the first repository — clone / import / create, then
 * the mandatory kind-deploy-command questions, then indexing — and a second
 * implementation of "which dialog is open and which repo still owes answers"
 * is precisely the copy that drifts the first time the questions change.
 *
 * One instance covers every project on a screen, not one per project card:
 * "the next import is only offered once the current repo's questions are
 * answered" has to hold across the page, not merely inside the card the user
 * happened to start from.
 */
export interface RepositoryImport {
  /** The project the open dialog will link into. Null when nothing is in flight. */
  activeProjectId: string | null;
  /** A repository still owes its setup answers. Every add button is disabled meanwhile. */
  pendingSetup: boolean;
  /** The repo was created but its setup form could not be loaded. Shown, never swallowed. */
  setupError: { repositoryId: string; message: string } | null;
  start: (projectId: string, method: RepositoryAddMethod) => void;
  retrySetup: () => void;
  dismissSetupError: () => void;
  /* Internals the two components below read. Not part of the caller's contract. */
  setupRepo: Repository | null;
  openDialog: boolean;
  importDialog: boolean;
  createDialog: boolean;
  setOpenDialog: (open: boolean) => void;
  setImportDialog: (open: boolean) => void;
  setCreateDialog: (open: boolean) => void;
  onAdded: (repositoryId: string) => void;
  closeSetup: () => void;
  /** Drives RepositoryAnalyzingDialog: non-null while a clone/read + detect is
   * in flight, or while the just-added repo is being fetched for setup. */
  analyzing: AnalyzePhase | null;
  startAnalyze: (flow: AnalyzePhase["flow"], label: string) => void;
  endAnalyze: () => void;
}

/**
 * @param onChanged Re-read whatever the caller renders — the project and
 * repository lists — after anything the wizard did lands. Awaited before the
 * setup questions open, so the new row is on screen behind them.
 */
export function useRepositoryImport(onChanged: () => void | Promise<void>): RepositoryImport {
  const { t } = useI18n();
  const [activeProjectId, setActiveProjectId] = useState<string | null>(null);
  const [openDialog, setOpenDialog] = useState(false);
  const [importDialog, setImportDialog] = useState(false);
  const [createDialog, setCreateDialog] = useState(false);
  const [setupRepo, setSetupRepo] = useState<Repository | null>(null);
  const [setupError, setSetupError] = useState<{ repositoryId: string; message: string } | null>(null);
  const [analyzing, setAnalyzing] = useState<AnalyzePhase | null>(null);
  // The dialog components only report a label; onAdded needs the flow too, to
  // keep showing the right wording through the finalizing phase.
  const analyzeFlow = useRef<AnalyzePhase["flow"]>("import");

  const pendingSetup = setupRepo !== null || setupError !== null;

  const startAnalyze = useCallback((flow: AnalyzePhase["flow"], label: string) => {
    analyzeFlow.current = flow;
    setAnalyzing({ flow, label });
  }, []);
  const endAnalyze = useCallback(() => setAnalyzing(null), []);

  const start = useCallback(
    (projectId: string, method: RepositoryAddMethod) => {
      if (pendingSetup) return;
      setActiveProjectId(projectId);
      if (method === "open") setOpenDialog(true);
      else if (method === "import") setImportDialog(true);
      else setCreateDialog(true);
    },
    [pendingSetup],
  );

  const loadForSetup = useCallback(
    async (repositoryId: string) => {
      try {
        setSetupRepo(await api.getRepository(repositoryId));
        setSetupError(null);
      } catch (e) {
        // Stubbing this silently (skipping the mandatory questions because the
        // follow-up fetch failed) is exactly what the brief rules out. The repo
        // itself was created fine — only loading its setup form failed — so
        // this stays on screen as something to retry rather than a toast that
        // scrolls away.
        setSetupError({
          repositoryId,
          message: e instanceof Error ? e.message : t("projectAdmin.projects.setupLoadFailed"),
        });
      }
    },
    [t],
  );

  // Every add path (open / import / create) chains into the same follow-up:
  // reload the lists, then require the initial setup questions for the repo
  // that was just added.
  const onAdded = useCallback(
    (repositoryId: string) => {
      // Covers the gap between "import/open call succeeded" and the setup
      // dialog mounting (list reload + repo re-fetch): without this, the
      // analyzing dialog's onAnalyzeEnd() and this cover the same instant, so
      // React batches the two setAnalyzing calls and there is no flicker.
      setAnalyzing({ flow: analyzeFlow.current, label: t("projectAdmin.analyzing.finalizing") });
      void (async () => {
        try {
          await onChanged();
          if (!repositoryId) return;
          await loadForSetup(repositoryId);
        } finally {
          setAnalyzing(null);
        }
      })();
    },
    [onChanged, loadForSetup, t],
  );

  const closeSetup = useCallback(() => {
    setSetupRepo(null);
    setActiveProjectId(null);
    void onChanged();
  }, [onChanged]);

  const retrySetup = useCallback(() => {
    if (setupError) void loadForSetup(setupError.repositoryId);
  }, [setupError, loadForSetup]);

  const dismissSetupError = useCallback(() => setSetupError(null), []);

  return {
    activeProjectId,
    pendingSetup,
    setupError,
    start,
    retrySetup,
    dismissSetupError,
    setupRepo,
    openDialog,
    importDialog,
    createDialog,
    setOpenDialog,
    setImportDialog,
    setCreateDialog,
    onAdded,
    closeSetup,
    analyzing,
    startAnalyze,
    endAnalyze,
  };
}

/** The four dialogs the wizard drives. Render once per screen, next to the lists. */
export function RepositoryImportDialogs({ state }: { state: RepositoryImport }) {
  const locked = state.activeProjectId ? { lockedProjectId: state.activeProjectId } : {};
  return (
    <>
      <ImportGitHubDialog
        open={state.importDialog}
        onOpenChange={state.setImportDialog}
        onSuccess={state.onAdded}
        onAnalyzeStart={(label) => state.startAnalyze("import", label)}
        onAnalyzeEnd={state.endAnalyze}
        {...locked}
      />
      <OpenRepositoryDialog
        open={state.openDialog}
        onOpenChange={state.setOpenDialog}
        onSuccess={state.onAdded}
        onAnalyzeStart={(label) => state.startAnalyze("open", label)}
        onAnalyzeEnd={state.endAnalyze}
        {...locked}
      />
      <CreateRepositoryDialog
        open={state.createDialog}
        onOpenChange={state.setCreateDialog}
        onSuccess={state.onAdded}
        {...locked}
      />
      {state.setupRepo && <InitialSetupDialog repository={state.setupRepo} onClose={state.closeSetup} />}
      <RepositoryAnalyzingDialog phase={state.analyzing} />
    </>
  );
}

/** "The repository landed but its questions could not be loaded" — retry or dismiss. */
export function RepositoryImportErrorNotice({
  state,
  className,
}: {
  state: RepositoryImport;
  className?: string;
}) {
  const { t } = useI18n();
  if (!state.setupError) return null;
  return (
    <Notice variant="error" title={t("projectAdmin.projects.setupLoadFailed")} className={className}>
      <p>{state.setupError.message}</p>
      <div className="mt-2 flex gap-2">
        <Button variant="outline" size="sm" onClick={state.retrySetup}>
          {t("common.refresh")}
        </Button>
        <Button variant="ghost" size="sm" onClick={state.dismissSetupError}>
          {t("projectAdmin.projects.setupLoadDismiss")}
        </Button>
      </div>
    </Notice>
  );
}
