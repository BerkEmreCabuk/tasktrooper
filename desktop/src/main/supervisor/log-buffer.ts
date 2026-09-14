import type { ChildId, LogLine } from "../../ipc/types.js";

/**
 * A bounded, per-child ring of log lines.
 *
 * A terminal has scrollback and a person who closes it. This app is expected
 * to run for days on a machine the user is also working on, so "something else
 * deals with it" is not available: an unbounded array of a session's transcript
 * log is a memory leak with a schedule.
 *
 * So each child gets its own fixed-capacity ring. Old lines are dropped, not
 * spilled to disk — a supervisor that writes logs somewhere is a supervisor
 * that has to rotate, prune and secure them, and the lines worth keeping past
 * a restart are already in the children's own logging.
 */
export class LogRing {
  readonly #capacity: number;
  readonly #maxLineLength: number;
  #lines: LogLine[] = [];
  #dropped = 0;

  constructor(capacity: number, maxLineLength = 4000) {
    this.#capacity = capacity;
    this.#maxLineLength = maxLineLength;
  }

  push(line: LogLine): LogLine {
    // A single pathological line — a stack trace, a base64 body, a binary
    // blob on stderr — can be megabytes on its own, which makes a
    // line-counted cap no cap at all.
    const stored: LogLine =
      line.text.length > this.#maxLineLength
        ? { ...line, text: `${line.text.slice(0, this.#maxLineLength)}… [${line.text.length} chars, truncated]` }
        : line;
    this.#lines.push(stored);
    if (this.#lines.length > this.#capacity) {
      this.#lines.splice(0, this.#lines.length - this.#capacity);
      this.#dropped += 1;
    }
    return stored;
  }

  /** Lines newer than `afterSeq`, oldest first, at most `limit` of them. */
  read(afterSeq = 0, limit = Number.MAX_SAFE_INTEGER): LogLine[] {
    const slice = afterSeq > 0 ? this.#lines.filter((l) => l.seq > afterSeq) : this.#lines;
    return slice.length > limit ? slice.slice(slice.length - limit) : [...slice];
  }

  clear(): void {
    this.#lines = [];
    this.#dropped = 0;
  }

  get size(): number {
    return this.#lines.length;
  }

  get dropped(): number {
    return this.#dropped;
  }
}

/** All the rings, plus the sequence numbers that order lines across them. */
export class LogStore {
  readonly #rings = new Map<ChildId | "supervisor", LogRing>();
  readonly #capacity: number;
  #seq = 0;

  constructor(capacity = 2000) {
    this.#capacity = capacity;
  }

  #ring(child: ChildId | "supervisor"): LogRing {
    let ring = this.#rings.get(child);
    if (!ring) {
      ring = new LogRing(this.#capacity);
      this.#rings.set(child, ring);
    }
    return ring;
  }

  append(child: ChildId | "supervisor", stream: "stdout" | "stderr", text: string, level?: string): LogLine {
    this.#seq += 1;
    const line: LogLine = { seq: this.#seq, child, at: Date.now(), stream, text, ...(level ? { level } : {}) };
    return this.#ring(child).push(line);
  }

  /**
   * Read one child's ring, or every ring merged back into sequence order. The
   * merge is what makes the status page's unfiltered view read like one
   * interleaved stream.
   */
  read(child?: ChildId | "supervisor", afterSeq = 0, limit?: number): LogLine[] {
    if (child) return this.#ring(child).read(afterSeq, limit ?? Number.MAX_SAFE_INTEGER);
    const merged = [...this.#rings.values()].flatMap((ring) => ring.read(afterSeq));
    merged.sort((a, b) => a.seq - b.seq);
    return limit !== undefined && merged.length > limit ? merged.slice(merged.length - limit) : merged;
  }

  clear(): void {
    for (const ring of this.#rings.values()) ring.clear();
  }
}

/**
 * Splits a stream of Buffers into lines without unbounded buffering.
 *
 * A child that writes a gigabyte with no newline in it — which is what a
 * corrupted binary on stdout looks like — must not be able to grow this
 * process's heap by a gigabyte while the splitter waits for a delimiter that
 * is not coming.
 */
export class LineSplitter {
  #partial = "";
  readonly #limit: number;

  constructor(limit = 64 * 1024) {
    this.#limit = limit;
  }

  push(chunk: Buffer | string): string[] {
    this.#partial += typeof chunk === "string" ? chunk : chunk.toString("utf8");
    const parts = this.#partial.split("\n");
    this.#partial = parts.pop() ?? "";
    if (this.#partial.length > this.#limit) {
      parts.push(this.#partial.slice(0, this.#limit));
      this.#partial = "";
    }
    return parts.map((line) => (line.endsWith("\r") ? line.slice(0, -1) : line)).filter((line) => line !== "");
  }

  /** Whatever is left when the stream ends. */
  flush(): string[] {
    const rest = this.#partial;
    this.#partial = "";
    return rest === "" ? [] : [rest];
  }
}
