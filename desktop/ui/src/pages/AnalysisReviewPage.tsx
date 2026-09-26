import { FileText, Loader2, SearchX, Send } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { api, type BoardTask, type TaskAnnotation, type TaskDocument } from "@/api";
import { AnalysisFrame, type AnalysisFrameHandle } from "@/components/board/analysis/AnalysisFrame";
import { AnnotationsPanel } from "@/components/board/analysis/AnnotationsPanel";
import { SubmitAnnotationsDialog } from "@/components/board/analysis/SubmitAnnotationsDialog";
import type { FrameSelection } from "@/components/board/analysis/srcdoc";
import { PageBackLink } from "@/components/layout/PageBackLink";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { useTheme } from "@/hooks/useTheme";
import { annotationCounts, isRevising, pickReviewDocument } from "@/lib/analysis-review";

const REVISION_POLL_MS = 3000;

/**
 * Full-window review of a task's analysis document: the document in a
 * sandboxed frame on the left, the reader's passage comments on the right, and
 * one button that sends every open comment to the agent at once. A page rather
 * than another dialog inside the task drawer, which is itself a dialog.
 */
export function AnalysisReviewPage() {
  const { repositoryId = "", taskId = "" } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const { t } = useI18n();
  const { theme } = useTheme();
  const [task, setTask] = useState<BoardTask | null>(null);
  const [documents, setDocuments] = useState<TaskDocument[]>([]);
  const [annotations, setAnnotations] = useState<TaskAnnotation[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [anchored, setAnchored] = useState<Record<string, boolean>>({});
  const [activeId, setActiveId] = useState<string | null>(null);
  const [pending, setPending] = useState<FrameSelection | null>(null);
  const [submitOpen, setSubmitOpen] = useState(false);
  const [approveConfirmOpen, setApproveConfirmOpen] = useState(false);
  const [approving, setApproving] = useState(false);
  const frameRef = useRef<AnalysisFrameHandle>(null);
  // Bumped by every local annotation change, so a poll that left before the
  // change cannot land after it and put the old list back.
  const mutationVersion = useRef(0);

  const boardPath = `/board?task=${encodeURIComponent(taskId)}`;

  const refresh = useCallback(async () => {
    const version = mutationVersion.current;
    const [tasks, docs, notes] = await Promise.allSettled([
      api.listRepositoryTasks(repositoryId),
      api.listTaskDocuments(repositoryId, taskId),
      api.listTaskAnnotations(repositoryId, taskId),
    ]);
    if (tasks.status === "fulfilled") setTask((tasks.value.tasks ?? []).find((item) => item.id === taskId) ?? null);
    if (docs.status === "fulfilled") setDocuments(docs.value.documents ?? []);
    if (notes.status === "fulfilled" && version === mutationVersion.current) {
      setAnnotations(notes.value.annotations ?? []);
    }
    const failed = [tasks, docs].find((result) => result.status === "rejected");
    return failed ? (failed.reason as unknown) : null;
  }, [repositoryId, taskId]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    void refresh().then((error) => {
      if (cancelled) return;
      if (error) setLoadError(error instanceof Error ? error.message : t("analysisReview.page.loadFailed"));
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refresh]);

  const revising = isRevising(task?.column);
  const inReview = task?.column === "analiz_review";
  const poll = useCallback(async () => {
    await refresh();
  }, [refresh]);
  usePolling(poll, REVISION_POLL_MS, revising);

  // The poll that sees the task back in review fetched the document in the
  // same round trip, possibly a moment before the agent's last write — one
  // more read makes the reloaded document the final one.
  const wasRevising = useRef(false);
  useEffect(() => {
    if (wasRevising.current && !revising) void refresh();
    wasRevising.current = revising;
  }, [revising, refresh]);

  useEffect(() => {
    if (revising) setPending(null);
  }, [revising]);

  const requestedDocId = searchParams.get("doc");
  const currentDoc = useMemo(
    () => documents.find((doc) => doc.id === requestedDocId) ?? pickReviewDocument(documents),
    [documents, requestedDocId],
  );
  const currentDocId = currentDoc?.id ?? null;
  const docAnnotations = useMemo(
    () => annotations.filter((annotation) => annotation.document_id === currentDocId),
    [annotations, currentDocId],
  );
  const openCount = annotationCounts(annotations).open;

  useEffect(() => {
    setPending(null);
    setActiveId(null);
    setAnchored({});
  }, [currentDocId]);

  const selectDocument = (id: string) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set("doc", id);
        return next;
      },
      { replace: true },
    );
  };

  const upsert = useCallback((annotation: TaskAnnotation) => {
    mutationVersion.current += 1;
    setAnnotations((prev) =>
      prev.some((item) => item.id === annotation.id)
        ? prev.map((item) => (item.id === annotation.id ? annotation : item))
        : [...prev, annotation],
    );
  }, []);

  const remove = useCallback((id: string) => {
    mutationVersion.current += 1;
    setAnnotations((prev) => prev.filter((item) => item.id !== id));
    setActiveId((prev) => (prev === id ? null : prev));
  }, []);

  const activate = (id: string) => {
    setActiveId(id);
    frameRef.current?.scrollTo(id);
  };

  const approve = async () => {
    setApproving(true);
    try {
      await api.updateRepositoryTask(repositoryId, taskId, { column: "done" });
      toast.success(t("boardArea.components.taskDetail.analizReviewApproved"));
      navigate(boardPath);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("boardArea.components.taskDetail.updateFailed"));
    } finally {
      setApproving(false);
    }
  };

  const submit = async (note: string) => {
    try {
      const result = await api.submitTaskAnnotations(repositoryId, taskId, note || undefined);
      setSubmitOpen(false);
      toast.success(t("analysisReview.submit.sent", { count: result.submitted }));
      navigate(boardPath);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("analysisReview.submit.failed"));
    }
  };

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Spinner />
      </div>
    );
  }

  if (!task) {
    return (
      <div className="p-page">
        <PageBackLink to={boardPath} label={t("analysisReview.page.back")} />
        <EmptyState
          icon={SearchX}
          variant={loadError ? "critical" : "default"}
          title={loadError ? t("analysisReview.page.loadFailed") : t("analysisReview.page.taskNotFound")}
          description={loadError ?? t("analysisReview.page.taskNotFoundBody")}
        />
      </div>
    );
  }

  const frameAnnotations = docAnnotations.map(({ id, quote, prefix, suffix, status }) => ({
    id,
    quote,
    prefix,
    suffix,
    status,
  }));

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b border-border px-6 py-3">
        <PageBackLink to={boardPath} label={t("analysisReview.page.back")} className="mb-0" />
        <div className="min-w-0 flex-1">
          <p className="font-mono text-caption text-muted-foreground">{task.key}</p>
          <h1 className="truncate text-title font-semibold">{task.title}</h1>
        </div>
        {documents.length > 1 && currentDoc && (
          <Select value={currentDoc.id} onValueChange={selectDocument}>
            <SelectTrigger className="w-64" aria-label={t("analysisReview.page.documentLabel")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {documents.map((doc) => (
                <SelectItem key={doc.id} value={doc.id}>
                  {doc.title}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {inReview && (
          <Button
            variant="outline"
            onClick={() => (openCount > 0 ? setApproveConfirmOpen(true) : void approve())}
            disabled={approving}
          >
            {approving && <Loader2 className="animate-spin" />}
            {t("boardArea.components.taskDetail.analizReviewApprove")}
          </Button>
        )}
        <Button
          onClick={() => setSubmitOpen(true)}
          disabled={!inReview || openCount === 0}
          title={inReview ? undefined : t("analysisReview.page.submitOnlyInReview")}
        >
          <Send />
          {t("analysisReview.page.submit", { count: openCount })}
        </Button>
      </header>
      {revising && (
        <Notice variant="info" title={t("analysisReview.page.revising")} className="mx-6 mt-3">
          {t("analysisReview.page.revisingBody")}
        </Notice>
      )}
      <div className="flex min-h-0 flex-1">
        {currentDoc ? (
          <AnalysisFrame
            ref={frameRef}
            className="flex-1"
            document={currentDoc}
            annotations={frameAnnotations}
            activeId={activeId}
            theme={theme}
            onSelection={(selection) => {
              if (!revising) setPending(selection);
            }}
            onFocusAnnotation={setActiveId}
            onAnchored={setAnchored}
          />
        ) : (
          <EmptyState
            className="flex-1"
            icon={FileText}
            title={t("analysisReview.page.noDocuments")}
            description={t("analysisReview.page.noDocumentsBody")}
          />
        )}
        <AnnotationsPanel
          className="w-[360px] shrink-0 border-l border-border"
          repositoryId={repositoryId}
          taskId={taskId}
          documentId={currentDocId}
          annotations={docAnnotations}
          anchored={anchored}
          activeId={activeId}
          pending={pending}
          canComment={!revising && currentDoc !== null}
          onActivate={activate}
          onCancelPending={() => setPending(null)}
          onUpsert={upsert}
          onRemove={remove}
        />
      </div>
      <SubmitAnnotationsDialog open={submitOpen} onOpenChange={setSubmitOpen} openCount={openCount} onSubmit={submit} />
      <ConfirmDialog
        open={approveConfirmOpen}
        onOpenChange={setApproveConfirmOpen}
        title={t("analysisReview.page.approveWithOpenTitle")}
        description={t("analysisReview.page.approveWithOpenBody", { count: openCount })}
        confirmLabel={t("boardArea.components.taskDetail.analizReviewApprove")}
        variant="default"
        loading={approving}
        onConfirm={approve}
      />
    </div>
  );
}
