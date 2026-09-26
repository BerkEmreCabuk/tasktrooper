import { afterEach, describe, expect, it } from "vitest";
import { textQuoteKit } from "@/components/board/analysis/textQuote";

const kit = textQuoteKit();

function mount(html: string): HTMLElement {
  const root = document.createElement("div");
  root.innerHTML = html;
  document.body.appendChild(root);
  return root;
}

function textNode(root: HTMLElement, selector: string): Text {
  return root.querySelector(selector)!.firstChild as Text;
}

function mark(id: string) {
  return () => {
    const el = document.createElement("mark");
    el.setAttribute("data-tt-id", id);
    return el;
  };
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("textQuoteKit.collect", () => {
  it("collapses whitespace and keeps block boundaries apart", () => {
    const root = mount("<h1>Title</h1><p>Body   text\n  here</p><ul><li>one</li><li>two</li></ul>");
    expect(kit.collect(root).text).toBe("Title Body text here one two");
  });

  it("leaves out script, style and template text", () => {
    const root = mount("<p>Seen</p><script>hidden()</script><style>p{}</style><template>tpl</template><p>too</p>");
    expect(kit.collect(root).text).toBe("Seen too");
  });
});

describe("textQuoteKit.describe", () => {
  it("returns the selection with up to 32 characters of context either side", () => {
    const root = mount("<p>The quick brown fox jumps over the lazy dog and keeps on running far away.</p>");
    const text = textNode(root, "p");
    const range = document.createRange();
    range.setStart(text, 10);
    range.setEnd(text, 19);

    expect(kit.describe(root, range)).toEqual({
      quote: "brown fox",
      prefix: "The quick ",
      suffix: " jumps over the lazy dog and kee",
    });
  });

  it("reads a selection across elements as the collapsed visible text", () => {
    const root = mount("<p>Alpha <b>beta</b>   gamma</p><p>delta epsilon</p>");
    const range = document.createRange();
    range.setStart(textNode(root, "p"), 3);
    range.setEnd(textNode(root, "p:last-child"), 2);

    expect(kit.describe(root, range)?.quote).toBe("ha beta gamma de");
  });

  it("trims whitespace at the edges of the selection", () => {
    const root = mount("<p>one   two   three</p>");
    const text = textNode(root, "p");
    const range = document.createRange();
    range.setStart(text, 3);
    range.setEnd(text, 11);

    expect(kit.describe(root, range)).toEqual({ quote: "two", prefix: "one ", suffix: " three" });
  });

  it("returns null for a whitespace-only selection", () => {
    const root = mount("<p>one   two</p>");
    const text = textNode(root, "p");
    const range = document.createRange();
    range.setStart(text, 3);
    range.setEnd(text, 6);

    expect(kit.describe(root, range)).toBeNull();
  });
});

describe("textQuoteKit.locate", () => {
  const source = "<p>Use the cache here. Later, use the cache there.</p>";

  it("prefers the occurrence whose prefix and suffix match", () => {
    const root = mount(source);
    const index = kit.collect(root);
    const span = kit.locate(index, { quote: "the cache", prefix: "Later, use ", suffix: " there." });

    expect(span).toEqual({ start: index.text.lastIndexOf("the cache"), end: index.text.lastIndexOf("the cache") + 9 });
  });

  it("falls back to the first occurrence when the context matches nothing", () => {
    const root = mount(source);
    const index = kit.collect(root);

    expect(kit.locate(index, { quote: "the cache", prefix: "zzz", suffix: "qqq" })?.start).toBe(
      index.text.indexOf("the cache"),
    );
  });

  it("matches a quote whose whitespace differs from the document's", () => {
    const root = mount("<p>line one\n\n   line two</p>");
    const index = kit.collect(root);

    expect(kit.locate(index, { quote: "one\nline", prefix: "", suffix: "" })).not.toBeNull();
  });

  it("returns null when the passage is gone", () => {
    const root = mount(source);

    expect(kit.locate(kit.collect(root), { quote: "rewritten away", prefix: "", suffix: "" })).toBeNull();
  });
});

describe("textQuoteKit.wrap / clear", () => {
  it("wraps the disambiguated occurrence, not the first", () => {
    const root = mount("<p>Use the cache here. Later, use the cache there.</p>");
    const index = kit.collect(root);
    const span = kit.locate(index, { quote: "the cache", prefix: "use ", suffix: " there." })!;
    kit.wrap(index, span, mark("a1"));

    const marks = root.querySelectorAll("mark");
    expect(marks).toHaveLength(1);
    expect(marks[0].textContent).toBe("the cache");
    expect(marks[0].nextSibling?.textContent).toBe(" there.");
  });

  it("splits a passage crossing element boundaries into one mark per text node", () => {
    const root = mount("<p>one <em>two</em> three</p><p>four</p>");
    const index = kit.collect(root);
    const span = kit.locate(index, { quote: "two three four", prefix: "", suffix: "" })!;
    const wrapped = kit.wrap(index, span, mark("a1"));

    const marks = Array.from(root.querySelectorAll("mark"));
    expect(wrapped).toBe(3);
    expect(marks.map((m) => m.textContent)).toEqual(["two", " three", "four"]);
    expect(marks[0].parentElement?.tagName).toBe("EM");
  });

  it("wraps overlapping spans and clear restores the original markup", () => {
    const original = "<p>alpha beta gamma delta</p>";
    const root = mount(original);
    const index = kit.collect(root);
    const first = kit.locate(index, { quote: "alpha beta gamma", prefix: "", suffix: "" })!;
    const second = kit.locate(index, { quote: "gamma delta", prefix: "", suffix: "" })!;
    kit.wrapAll(root, [
      { span: first, make: mark("a1") },
      { span: second, make: mark("a2") },
    ]);

    const texts = (id: string) =>
      Array.from(root.querySelectorAll(`mark[data-tt-id="${id}"]`))
        .map((m) => m.textContent)
        .join("");
    expect(texts("a1")).toBe("alpha beta gamma");
    expect(texts("a2")).toBe("gamma delta");

    kit.clear(root, "mark[data-tt-id]");
    expect(root.innerHTML).toBe(original);
    expect(root.querySelector("p")!.childNodes).toHaveLength(1);
  });
});
