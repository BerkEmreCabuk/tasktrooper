import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { markdownFrameHtml } from "@/components/board/analysis/srcdoc";
import { I18nProvider } from "@/hooks/useI18n";

// react-dom/server is only needed for the older markdown documents, so it is
// loaded on first use instead of weighing on the main bundle.
export async function renderMarkdownFrameHtml(content: string, theme: "light" | "dark"): Promise<string> {
  const { renderToStaticMarkup } = await import("react-dom/server");
  const body = renderToStaticMarkup(
    <I18nProvider>
      <MarkdownContent content={content} />
    </I18nProvider>,
  );
  return markdownFrameHtml(body, theme);
}
