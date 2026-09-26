import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react";
import type { TaskAnnotationStatus, TaskDocument } from "@/api";
import { buildFrameScript } from "@/components/board/analysis/frameRuntime";
import { renderMarkdownFrameHtml } from "@/components/board/analysis/markdownFrame";
import {
  buildAnalysisSrcdoc,
  createNonce,
  FRAME_SANDBOX,
  parseFrameMessage,
  type FrameSelection,
} from "@/components/board/analysis/srcdoc";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { documentFormat } from "@/lib/analysis-review";
import { cn } from "@/lib/utils";

const FRAME_SCRIPT = buildFrameScript();

export interface FrameAnnotation {
  id: string;
  quote: string;
  prefix: string;
  suffix: string;
  status: TaskAnnotationStatus;
}

export interface AnalysisFrameHandle {
  scrollTo: (id: string) => void;
}

interface AnalysisFrameProps {
  document: Pick<TaskDocument, "id" | "content" | "format">;
  annotations: FrameAnnotation[];
  activeId: string | null;
  theme: "light" | "dark";
  onSelection: (selection: FrameSelection) => void;
  onFocusAnnotation: (id: string) => void;
  onAnchored: (found: Record<string, boolean>) => void;
  className?: string;
}

/**
 * The analysis document, rendered where it cannot reach the app: a sandboxed,
 * opaque-origin iframe (see srcdoc.ts) that talks to this component only by
 * postMessage. Nothing goes in but the document and the annotations to draw.
 */
export const AnalysisFrame = forwardRef<AnalysisFrameHandle, AnalysisFrameProps>(function AnalysisFrame(
  { document: doc, annotations, activeId, theme, onSelection, onFocusAnnotation, onAnchored, className },
  ref,
) {
  const { t } = useI18n();
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [srcdoc, setSrcdoc] = useState<string | null>(null);
  const format = documentFormat(doc);
  const markdownTheme = format === "markdown" ? theme : null;

  useEffect(() => {
    let cancelled = false;
    const build = (html: string) => buildAnalysisSrcdoc(html, { nonce: createNonce(), script: FRAME_SCRIPT });
    if (markdownTheme === null) {
      setSrcdoc(build(doc.content));
      return;
    }
    setSrcdoc(null);
    renderMarkdownFrameHtml(doc.content, markdownTheme)
      .then((html) => {
        if (!cancelled) setSrcdoc(build(html));
      })
      .catch(() => {
        if (!cancelled) setSrcdoc(build(""));
      });
    return () => {
      cancelled = true;
    };
  }, [doc.id, doc.content, markdownTheme]);

  const items = useMemo(
    () =>
      annotations.map((a) => ({
        id: a.id,
        quote: a.quote,
        prefix: a.prefix,
        suffix: a.suffix,
        status: a.status,
        active: a.id === activeId,
      })),
    [annotations, activeId],
  );
  // The board polls while the agent revises, so `annotations` is a new array
  // every few seconds; re-marking the document each time would disturb a
  // selection the reader is in the middle of. Only a real change is sent.
  const payload = JSON.stringify(items);
  const itemsRef = useRef(items);
  itemsRef.current = items;

  const handlers = useRef({ onSelection, onFocusAnnotation, onAnchored });
  handlers.current = { onSelection, onFocusAnnotation, onAnchored };

  const post = useCallback((message: Record<string, unknown>) => {
    iframeRef.current?.contentWindow?.postMessage(message, "*");
  }, []);

  const postItems = useCallback(() => {
    post({ type: "tt:annotations", items: itemsRef.current });
  }, [post]);

  useEffect(() => {
    postItems();
  }, [payload, postItems]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const frameWindow = iframeRef.current?.contentWindow;
      if (!frameWindow || event.source !== frameWindow) return;
      const message = parseFrameMessage(event.data);
      if (!message) return;
      switch (message.type) {
        case "tt:ready":
          postItems();
          break;
        case "tt:selection":
          handlers.current.onSelection({ quote: message.quote, prefix: message.prefix, suffix: message.suffix });
          break;
        case "tt:anchored":
          handlers.current.onAnchored(Object.fromEntries(message.results.map((r) => [r.id, r.found])));
          break;
        case "tt:focus":
          handlers.current.onFocusAnnotation(message.id);
          break;
      }
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [postItems]);

  useImperativeHandle(ref, () => ({ scrollTo: (id: string) => post({ type: "tt:scrollTo", id }) }), [post]);

  return (
    <div className={cn("relative min-h-0 min-w-0", className)}>
      {srcdoc === null ? (
        <div className="flex h-full items-center justify-center">
          <Spinner />
        </div>
      ) : (
        <iframe
          ref={iframeRef}
          title={t("analysisReview.page.frameTitle")}
          sandbox={FRAME_SANDBOX}
          srcDoc={srcdoc}
          referrerPolicy="no-referrer"
          onLoad={postItems}
          className={cn("h-full w-full border-0", format === "html" ? "bg-white" : "bg-background")}
        />
      )}
    </div>
  );
});
