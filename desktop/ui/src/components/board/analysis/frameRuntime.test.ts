import { afterEach, describe, expect, it } from "vitest";
import { buildFrameScript, frameRuntime } from "@/components/board/analysis/frameRuntime";
import { textQuoteKit } from "@/components/board/analysis/textQuote";

type Sent = Record<string, unknown>;
type Listener = (event: { source: unknown; data: unknown }) => void;

// Each case gets its own jsdom frame so listeners the runtime puts on one
// document never see another case's events. The window handed to the runtime
// is a stand-in whose `parent` records what is posted to it and whose message
// listener the test drives directly — jsdom's postMessage sets no `source`.
function setup(html: string, install: (win: Window) => void = (win) => frameRuntime(textQuoteKit(), win)) {
  const iframe = document.createElement("iframe");
  document.body.appendChild(iframe);
  const frameWindow = iframe.contentWindow! as Window & typeof globalThis;
  const doc = iframe.contentDocument!;
  doc.body.innerHTML = html;
  const sent: Sent[] = [];
  const host = { postMessage: (message: Sent) => sent.push(message) };
  let listener: Listener | undefined;
  const win = {
    document: doc,
    parent: host,
    getSelection: () => frameWindow.getSelection(),
    setTimeout: (fn: () => void) => {
      fn();
      return 0;
    },
    addEventListener: (type: string, fn: Listener) => {
      if (type === "message") listener = fn;
    },
  } as unknown as Window;
  install(win);
  const deliver = (data: unknown, source: unknown = host) => listener?.({ source, data });
  const last = (type: string) => [...sent].reverse().find((message) => message.type === type);
  return { doc, frameWindow, sent, deliver, last };
}

const item = (overrides: Record<string, unknown> = {}) => ({
  id: "a1",
  quote: "brown fox",
  prefix: "quick ",
  suffix: " jumps",
  status: "open",
  active: false,
  ...overrides,
});

afterEach(() => {
  document.body.innerHTML = "";
});

describe("frameRuntime", () => {
  it("announces itself to the parent once installed", () => {
    const { sent } = setup("<p>Hello</p>");
    expect(sent).toEqual([{ type: "tt:ready" }]);
  });

  it("marks the annotations the parent sends and reports which ones anchored", () => {
    const { doc, deliver, last } = setup("<p>The quick brown fox jumps.</p>");
    deliver({
      type: "tt:annotations",
      items: [item(), item({ id: "a2", quote: "no such passage", status: "resolved", active: true })],
    });

    const marks = doc.querySelectorAll("mark[data-tt-id]");
    expect(marks).toHaveLength(1);
    expect(marks[0].textContent).toBe("brown fox");
    expect(marks[0].getAttribute("data-tt-status")).toBe("open");
    expect(last("tt:anchored")).toEqual({
      type: "tt:anchored",
      results: [
        { id: "a1", found: true },
        { id: "a2", found: false },
      ],
    });
  });

  it("redraws from scratch on every annotations message", () => {
    const { doc, deliver } = setup("<p>The quick brown fox jumps.</p>");
    deliver({ type: "tt:annotations", items: [item()] });
    deliver({ type: "tt:annotations", items: [item({ status: "submitted", active: true })] });

    const marks = doc.querySelectorAll("mark[data-tt-id]");
    expect(marks).toHaveLength(1);
    expect(marks[0].getAttribute("data-tt-status")).toBe("submitted");
    expect(marks[0].getAttribute("data-tt-active")).toBe("true");

    deliver({ type: "tt:annotations", items: [] });
    expect(doc.querySelectorAll("mark")).toHaveLength(0);
    expect(doc.querySelector("p")!.childNodes).toHaveLength(1);
  });

  it("ignores messages that do not come from the parent, and malformed items", () => {
    const { doc, deliver, sent } = setup("<p>The quick brown fox jumps.</p>");
    deliver({ type: "tt:annotations", items: [item()] }, {});
    expect(doc.querySelectorAll("mark")).toHaveLength(0);
    expect(sent).toHaveLength(1);

    deliver({ type: "tt:annotations", items: [item({ status: "bogus" }), item({ id: 42 }), "nope"] });
    expect(doc.querySelectorAll("mark")).toHaveLength(0);
  });

  it("reports a finished selection as a text-quote selector", () => {
    const { doc, frameWindow, last } = setup("<p>The quick <b>brown</b> fox jumps.</p>");
    const range = doc.createRange();
    range.setStart(doc.querySelector("b")!.firstChild!, 0);
    range.setEnd(doc.querySelector("p")!.lastChild!, 4);
    const selection = frameWindow.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    doc.dispatchEvent(new frameWindow.MouseEvent("mouseup", { bubbles: true }));

    expect(last("tt:selection")).toMatchObject({
      type: "tt:selection",
      quote: "brown fox",
      prefix: "The quick ",
      suffix: " jumps.",
    });
  });

  it("reports a click on a mark as focus", () => {
    const { doc, frameWindow, deliver, last } = setup("<p>The quick brown fox jumps.</p>");
    deliver({ type: "tt:annotations", items: [item()] });
    frameWindow.getSelection()!.removeAllRanges();
    doc.querySelector("mark")!.dispatchEvent(new frameWindow.MouseEvent("click", { bubbles: true }));

    expect(last("tt:focus")).toEqual({ type: "tt:focus", id: "a1" });
  });

  it("keeps links from navigating the frame away", () => {
    const { doc, frameWindow } = setup('<p><a href="https://example.com/">out</a></p>');
    const click = new frameWindow.MouseEvent("click", { bubbles: true, cancelable: true });
    doc.querySelector("a")!.dispatchEvent(click);

    expect(click.defaultPrevented).toBe(true);
  });

  it("builds a script that installs the same runtime when evaluated", () => {
    const { sent, deliver, doc } = setup("<p>The quick brown fox jumps.</p>", (win) => {
      new Function("window", buildFrameScript())(win);
    });
    expect(sent[0]).toEqual({ type: "tt:ready" });

    deliver({ type: "tt:annotations", items: [item()] });
    expect(doc.querySelector("mark")?.textContent).toBe("brown fox");
  });
});
