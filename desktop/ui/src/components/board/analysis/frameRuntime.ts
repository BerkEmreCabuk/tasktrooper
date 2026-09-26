import { textQuoteKit, type TextQuoteKit, type TextSpan } from "@/components/board/analysis/textQuote";

/**
 * The only script that runs inside the sandboxed analysis frame; everything
 * the document itself carries is blocked by the frame's CSP. It reports the
 * reader's text selections as text-quote selectors, draws the annotations the
 * page sends it as `<mark>`s, and does nothing else — it has no network access
 * and sees nothing of the app but the messages below.
 *
 * Frame → page: `tt:ready`, `tt:selection` {quote, prefix, suffix, rect},
 * `tt:anchored` {results: [{id, found}]}, `tt:focus` {id}.
 * Page → frame: `tt:annotations` {items: [{id, quote, prefix, suffix, status,
 * active}]}, `tt:scrollTo` {id}.
 *
 * Serialized with `Function.prototype.toString` (see buildFrameScript), so,
 * like textQuoteKit, it must not reach anything outside its own body.
 */
export function frameRuntime(kit: TextQuoteKit, win: Window): void {
  const doc = win.document;
  const host = win.parent;
  const MARK = "mark[data-tt-id]";
  const STATUSES = ["open", "submitted", "resolved"];
  const MAX_ITEMS = 500;

  interface Item {
    id: string;
    quote: string;
    prefix: string;
    suffix: string;
    status: string;
    active: boolean;
  }

  let items: Item[] = [];
  let lastSelection = "";

  const send = (message: Record<string, unknown>) => {
    host.postMessage(message, "*");
  };

  const isItem = (value: unknown): value is Item => {
    if (!value || typeof value !== "object") return false;
    const v = value as Record<string, unknown>;
    return (
      typeof v.id === "string" &&
      v.id.length > 0 &&
      v.id.length <= 200 &&
      typeof v.quote === "string" &&
      typeof v.prefix === "string" &&
      typeof v.suffix === "string" &&
      typeof v.status === "string" &&
      STATUSES.indexOf(v.status) >= 0
    );
  };

  const paint = () => {
    const body = doc.body;
    if (!body) return;
    kit.clear(body, MARK);
    const index = kit.collect(body);
    const results: { id: string; found: boolean }[] = [];
    const spans: { span: TextSpan; make: () => Element }[] = [];
    for (const item of items) {
      const span = kit.locate(index, item);
      results.push({ id: item.id, found: span !== null });
      if (!span) continue;
      spans.push({
        span,
        make: () => {
          const mark = doc.createElement("mark");
          mark.setAttribute("data-tt-id", item.id);
          mark.setAttribute("data-tt-status", item.status);
          if (item.active) mark.setAttribute("data-tt-active", "true");
          return mark;
        },
      });
    }
    kit.wrapAll(body, spans);
    send({ type: "tt:anchored", results });
  };

  const reportSelection = () => {
    const selection = win.getSelection();
    if (!selection || selection.rangeCount === 0 || selection.isCollapsed) {
      lastSelection = "";
      return;
    }
    const range = selection.getRangeAt(0);
    const body = doc.body;
    if (!body || !body.contains(range.commonAncestorContainer)) return;
    const selector = kit.describe(body, range);
    if (!selector) return;
    const key = selector.prefix + "\u0000" + selector.quote + "\u0000" + selector.suffix;
    if (key === lastSelection) return;
    lastSelection = key;
    const box = typeof range.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null;
    send({
      type: "tt:selection",
      quote: selector.quote,
      prefix: selector.prefix,
      suffix: selector.suffix,
      rect: box
        ? { top: box.top, left: box.left, width: box.width, height: box.height }
        : { top: 0, left: 0, width: 0, height: 0 },
    });
  };

  const elementOf = (target: EventTarget | null): Element | null => {
    const node = target as Node | null;
    if (!node) return null;
    return node.nodeType === 1 ? (node as Element) : node.parentElement;
  };

  const findMark = (id: string): Element | null => {
    const marks = doc.querySelectorAll(MARK);
    for (let i = 0; i < marks.length; i++) {
      if (marks[i].getAttribute("data-tt-id") === id) return marks[i];
    }
    return null;
  };

  doc.addEventListener("mousedown", () => {
    lastSelection = "";
  });
  doc.addEventListener("mouseup", () => {
    win.setTimeout(reportSelection, 0);
  });
  doc.addEventListener("keyup", reportSelection);

  // A sandboxed frame may still navigate itself, so an ordinary link would
  // replace the document with whatever page it points at. Only in-document
  // anchors do anything.
  doc.addEventListener("click", (event) => {
    const element = elementOf(event.target);
    if (!element) return;
    const link = element.closest("a[href]");
    if (link) {
      event.preventDefault();
      const href = link.getAttribute("href") || "";
      if (href.charAt(0) === "#" && href.length > 1) {
        let destination: Element | null = null;
        try {
          destination = doc.getElementById(decodeURIComponent(href.slice(1)));
        } catch {
          destination = null;
        }
        if (destination && typeof destination.scrollIntoView === "function") {
          destination.scrollIntoView({ block: "start" });
        }
      }
    }
    const selection = win.getSelection();
    if (selection && !selection.isCollapsed) return;
    const mark = element.closest(MARK);
    const id = mark ? mark.getAttribute("data-tt-id") : null;
    if (id) send({ type: "tt:focus", id });
  });

  win.addEventListener("message", (event) => {
    if (event.source !== host) return;
    const data = event.data;
    if (!data || typeof data !== "object") return;
    if (data.type === "tt:annotations" && Array.isArray(data.items)) {
      items = data.items.filter(isItem).slice(0, MAX_ITEMS);
      paint();
    } else if (data.type === "tt:scrollTo" && typeof data.id === "string") {
      const mark = findMark(data.id);
      if (mark && typeof mark.scrollIntoView === "function") {
        mark.scrollIntoView({ block: "center", behavior: "smooth" });
      }
    }
  });

  send({ type: "tt:ready" });
}

export function buildFrameScript(): string {
  return `(${frameRuntime.toString()})((${textQuoteKit.toString()})(), window);`;
}
