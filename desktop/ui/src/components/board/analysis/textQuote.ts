export interface TextQuoteSelector {
  quote: string;
  prefix: string;
  suffix: string;
}

export interface TextSpan {
  start: number;
  end: number;
}

/**
 * A document's visible text, whitespace collapsed to single spaces, with every
 * character traced back to the text node and offset it came from. `charNode`
 * is -1 for the space inserted at a block boundary (`<h1>A</h1><p>B</p>`
 * reads "A B", not "AB"), which belongs to no node.
 */
export interface TextIndex {
  text: string;
  nodes: Text[];
  charNode: number[];
  charOffset: number[];
  spanStart: number[];
  spanEnd: number[];
}

export type TextQuoteKit = ReturnType<typeof textQuoteKit>;

/**
 * Text-quote anchoring (the W3C selector: the quoted passage plus a little
 * text either side) over a live DOM.
 *
 * This factory is also serialized with `Function.prototype.toString` into the
 * script injected into the sandboxed document frame (see frameRuntime.ts), so
 * it must stay self-contained: no imports, nothing from module scope, only its
 * own locals and DOM globals.
 */
export function textQuoteKit() {
  const CONTEXT = 32;
  const SPACE = /\s/;
  const NON_SPACE = /\S/;
  const XHTML = "http://www.w3.org/1999/xhtml";
  const SKIP = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEMPLATE", "TEXTAREA", "SELECT", "OPTION", "HEAD", "TITLE"]);
  const BLOCK = new Set([
    "ADDRESS", "ARTICLE", "ASIDE", "BLOCKQUOTE", "BR", "CAPTION", "DD", "DETAILS", "DIV", "DL", "DT", "FIELDSET",
    "FIGCAPTION", "FIGURE", "FOOTER", "FORM", "H1", "H2", "H3", "H4", "H5", "H6", "HEADER", "HR", "LI", "MAIN",
    "NAV", "OL", "P", "PRE", "SECTION", "SUMMARY", "TABLE", "TBODY", "TD", "TFOOT", "TH", "THEAD", "TR", "UL",
  ]);

  function collapse(value: string): string {
    return value.replace(/\s+/g, " ");
  }

  function collect(root: Node): TextIndex {
    const chars: string[] = [];
    const nodes: Text[] = [];
    const charNode: number[] = [];
    const charOffset: number[] = [];
    const spanStart: number[] = [];
    const spanEnd: number[] = [];
    let breakPending = false;

    const push = (ch: string, node: number, offset: number) => {
      chars.push(ch);
      charNode.push(node);
      charOffset.push(offset);
    };
    const endsInSpace = () => chars.length === 0 || chars[chars.length - 1] === " ";

    const addText = (node: Text) => {
      const n = nodes.length;
      nodes.push(node);
      spanStart.push(chars.length);
      const data = node.data;
      for (let i = 0; i < data.length; i++) {
        const ch = data.charAt(i);
        if (SPACE.test(ch)) {
          if (!endsInSpace()) push(" ", n, i);
          continue;
        }
        if (breakPending && !endsInSpace()) push(" ", -1, -1);
        breakPending = false;
        push(ch, n, i);
      }
      spanEnd.push(chars.length);
    };

    const walk = (parent: Node) => {
      for (let child = parent.firstChild; child; child = child.nextSibling) {
        if (child.nodeType === 3) {
          addText(child as Text);
        } else if (child.nodeType === 1) {
          const tag = child.nodeName.toUpperCase();
          if (SKIP.has(tag)) continue;
          const block = BLOCK.has(tag);
          if (block) breakPending = true;
          walk(child);
          if (block) breakPending = true;
        }
      }
    };

    walk(root);
    return { text: chars.join(""), nodes, charNode, charOffset, spanStart, spanEnd };
  }

  function describe(root: Node, range: Range): TextQuoteSelector | null {
    const index = collect(root);
    let start = -1;
    let end = -1;
    for (let n = 0; n < index.nodes.length; n++) {
      const node = index.nodes[n];
      if (!range.intersectsNode(node)) continue;
      for (let c = index.spanStart[n]; c < index.spanEnd[n]; c++) {
        if (index.charNode[c] !== n) continue;
        const offset = index.charOffset[c];
        if (node === range.startContainer && offset < range.startOffset) continue;
        if (node === range.endContainer && offset >= range.endOffset) continue;
        if (start < 0) start = c;
        end = c + 1;
      }
    }
    if (start < 0) return null;
    const text = index.text;
    while (start < end && text.charAt(start) === " ") start++;
    while (end > start && text.charAt(end - 1) === " ") end--;
    if (start >= end) return null;
    return {
      quote: text.slice(start, end),
      prefix: text.slice(Math.max(0, start - CONTEXT), start),
      suffix: text.slice(end, end + CONTEXT),
    };
  }

  function sharedTail(a: string, b: string): number {
    let n = 0;
    while (n < a.length && n < b.length && a.charAt(a.length - 1 - n) === b.charAt(b.length - 1 - n)) n++;
    return n;
  }

  function sharedHead(a: string, b: string): number {
    let n = 0;
    while (n < a.length && n < b.length && a.charAt(n) === b.charAt(n)) n++;
    return n;
  }

  // Every occurrence of the quote is scored by how much of the stored prefix
  // and suffix its surroundings still match; the best one wins and a tie goes
  // to the earliest, so a repeated phrase lands on the passage that was meant.
  function locate(index: TextIndex, selector: TextQuoteSelector): TextSpan | null {
    const quote = collapse(selector.quote).trim();
    if (!quote) return null;
    const prefix = collapse(selector.prefix || "");
    const suffix = collapse(selector.suffix || "");
    const text = index.text;
    let best = -1;
    let bestScore = -1;
    let from = 0;
    for (let guard = 0; guard < 1000; guard++) {
      const at = text.indexOf(quote, from);
      if (at < 0) break;
      const after = at + quote.length;
      const score =
        sharedTail(text.slice(Math.max(0, at - prefix.length), at), prefix) +
        sharedHead(text.slice(after, after + suffix.length), suffix);
      if (score > bestScore) {
        best = at;
        bestScore = score;
      }
      from = at + 1;
    }
    return best < 0 ? null : { start: best, end: best + quote.length };
  }

  // Splits text nodes at the span's edges and moves each piece into its own
  // element from `make`, so a passage crossing `<b>`/`<a>`/block boundaries
  // becomes several marks. Whitespace-only pieces are left alone: they are the
  // gaps between table cells or list items, where a mark would not render.
  function wrap(index: TextIndex, span: TextSpan, make: () => Element): number {
    const segments: { node: Text; from: number; to: number }[] = [];
    for (let n = 0; n < index.nodes.length; n++) {
      if (index.spanEnd[n] <= span.start || index.spanStart[n] >= span.end) continue;
      let from = -1;
      let to = -1;
      const lo = Math.max(span.start, index.spanStart[n]);
      const hi = Math.min(span.end, index.spanEnd[n]);
      for (let c = lo; c < hi; c++) {
        if (index.charNode[c] !== n) continue;
        if (from < 0) from = index.charOffset[c];
        to = index.charOffset[c] + 1;
      }
      if (from >= 0) segments.push({ node: index.nodes[n], from, to });
    }
    let wrapped = 0;
    for (const segment of segments) {
      const parent = segment.node.parentNode as Element | null;
      if (!parent || parent.namespaceURI !== XHTML) continue;
      if (!NON_SPACE.test(segment.node.data.slice(segment.from, segment.to))) continue;
      let target = segment.node;
      if (segment.to < target.data.length) target.splitText(segment.to);
      if (segment.from > 0) target = target.splitText(segment.from);
      const mark = make();
      parent.insertBefore(mark, target);
      mark.appendChild(target);
      wrapped++;
    }
    return wrapped;
  }

  // Wraps several spans with as few index rebuilds as possible. Working from
  // the last span backwards, splitting a node only shortens it from the end,
  // so an index built before the split still holds for any span that ends
  // before the split point. Only a span overlapping one already wrapped needs
  // a fresh index.
  function wrapAll(root: Node, spans: { span: TextSpan; make: () => Element }[]): void {
    const ordered = spans.slice().sort((a, b) => b.span.start - a.span.start);
    let index: TextIndex | null = null;
    let floor = Infinity;
    for (const item of ordered) {
      if (!index || item.span.end > floor) index = collect(root);
      wrap(index, item.span, item.make);
      floor = item.span.start;
    }
  }

  function clear(root: Element, selector: string): void {
    const marks = root.querySelectorAll(selector);
    const parents: Node[] = [];
    for (let i = 0; i < marks.length; i++) {
      const mark = marks[i];
      const parent = mark.parentNode;
      if (!parent) continue;
      while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
      parent.removeChild(mark);
      parents.push(parent);
    }
    for (const parent of parents) parent.normalize();
  }

  return { collapse, collect, describe, locate, wrap, wrapAll, clear };
}
