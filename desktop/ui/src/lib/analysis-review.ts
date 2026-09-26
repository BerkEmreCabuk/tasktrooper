import type { TaskAnnotation, TaskAnnotationStatus, TaskColumn, TaskDocument, TaskDocumentFormat } from "@/api";

export function analysisReviewPath(repositoryId: string, taskId: string, documentId?: string): string {
  const base = `/repositories/${encodeURIComponent(repositoryId)}/tasks/${encodeURIComponent(taskId)}/analysis`;
  return documentId ? `${base}?doc=${encodeURIComponent(documentId)}` : base;
}

export function documentFormat(doc: Pick<TaskDocument, "format">): TaskDocumentFormat {
  return doc.format === "html" ? "html" : "markdown";
}

const ANALIZ_TITLE = /^analiz\s*:/i;

function newestFirst(a: TaskDocument, b: TaskDocument): number {
  return b.updated_at.localeCompare(a.updated_at);
}

/**
 * The document the review page opens on when the URL names none: the
 * architect's single `analiz:` HTML document, else any HTML document, else
 * the first document in the task's own order (older tasks carry markdown
 * `spec:`/`plan:` documents only).
 */
export function pickReviewDocument(documents: TaskDocument[]): TaskDocument | null {
  if (documents.length === 0) return null;
  const html = documents.filter((doc) => documentFormat(doc) === "html").sort(newestFirst);
  const analiz = html.find((doc) => ANALIZ_TITLE.test(doc.title.trim()));
  if (analiz) return analiz;
  if (html.length > 0) return html[0];
  return [...documents].sort((a, b) => a.position - b.position)[0];
}

// While the agent works through submitted annotations the task sits in one of
// these; the server moves it back to analiz_review once every one is resolved.
const REVISING_COLUMNS = new Set<TaskColumn>(["need_revision", "in_progress"]);

export function isRevising(column: TaskColumn | undefined): boolean {
  return column !== undefined && REVISING_COLUMNS.has(column);
}

export type AnnotationCounts = Record<TaskAnnotationStatus, number>;

export function annotationCounts(annotations: TaskAnnotation[]): AnnotationCounts {
  const counts: AnnotationCounts = { open: 0, submitted: 0, resolved: 0 };
  for (const annotation of annotations) {
    if (annotation.status in counts) counts[annotation.status] += 1;
  }
  return counts;
}
