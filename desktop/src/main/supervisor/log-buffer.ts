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
/** The longest line a ring stores; anything longer is cut, with a note saying so. */
export const MAX_LINE_LENGTH = 4000;

export class LogRing {
  readonly #capacity: number;
  readonly #maxLineLength: number;
  // A fixed array used circularly: `#start` is the oldest line. Dropping the
  // oldest line once full is an index bump, where an array shift would copy
  // the whole buffer on every push.
  #slots: (LogLine | undefined)[];
  #start = 0;
  #size = 0;
  #dropped = 0;

  constructor(capacity: number, maxLineLength = MAX_LINE_LENGTH) {
    this.#capacity = Math.max(1, capacity);
    this.#maxLineLength = maxLineLength;
    this.#slots = new Array<LogLine | undefined>(this.#capacity);
  }

  push(line: LogLine): LogLine {
    // A single pathological line — a stack trace, a base64 body, a binary
    // blob on stderr — can be megabytes on its own, which makes a
    // line-counted cap no cap at all.
    const stored: LogLine =
      line.text.length > this.#maxLineLength
        ? { ...line, text: `${line.text.slice(0, this.#maxLineLength)}… [${line.text.length} chars, truncated]` }
        : line;
    if (this.#size < this.#capacity) {
      this.#slots[(this.#start + this.#size) % this.#capacity] = stored;
      this.#size += 1;
    } else {
      this.#slots[this.#start] = stored;
      this.#start = (this.#start + 1) % this.#capacity;
      this.#dropped += 1;
    }
    return stored;
  }

  #at(index: number): LogLine {
    return this.#slots[(this.#start + index) % this.#capacity] as LogLine;
  }

  /** Lines newer than `afterSeq`, oldest first, at most `limit` of them. */
  read(afterSeq = 0, limit = Number.MAX_SAFE_INTEGER): LogLine[] {
    // Sequence numbers only grow, so the first line past `afterSeq` is found
    // by bisection rather than by scanning every line.
    let lo = 0;
    let hi = this.#size;
    while (lo < hi) {
      const mid = (lo + hi) >>> 1;
      if (this.#at(mid).seq > afterSeq) hi = mid;
      else lo = mid + 1;
    }
    const from = Math.max(lo, this.#size - limit);
    const out: LogLine[] = [];
    for (let i = from; i < this.#size; i++) out.push(this.#at(i));
    return out;
  }

  /** The newest line, if there is one. */
  last(): LogLine | undefined {
    return this.#size > 0 ? this.#at(this.#size - 1) : undefined;
  }

  clear(): void {
    this.#slots = new Array<LogLine | undefined>(this.#capacity);
    this.#start = 0;
    this.#size = 0;
    this.#dropped = 0;
  }

  get size(): number {
    return this.#size;
  }

  get dropped(): number {
    return this.#dropped;
  }
}

/**
 * Turns a stored line into the text a person reads. Applied when a line is
 * READ, not when it arrives: the backend writes far more lines than anyone
 * looks at, and parsing each one on the main thread as it arrives is work
 * done for nobody.
 */
export type LogRenderer = (text: string) => string;

/** All the rings, plus the sequence numbers that order lines across them. */
export class LogStore {
  readonly #rings = new Map<ChildId | "supervisor", LogRing>();
  readonly #capacity: number;
  readonly #renderers: Partial<Record<ChildId | "supervisor", LogRenderer>>;
  // Lines whose text is already the rendered one (see `append`).
  readonly #rendered = new WeakSet<LogLine>();
  #seq = 0;

  constructor(capacity = 2000, renderers: Partial<Record<ChildId | "supervisor", LogRenderer>> = {}) {
    this.#capacity = capacity;
    this.#renderers = renderers;
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
    // A line the ring would cut is rendered now and cut after: cut first, a
    // JSON line is no longer JSON and could only ever be shown raw.
    const renderer = this.#renderers[child];
    const early = renderer !== undefined && text.length > MAX_LINE_LENGTH;
    const line: LogLine = {
      seq: this.#seq,
      child,
      at: Date.now(),
      stream,
      text: early ? renderer(text) : text,
      ...(level ? { level } : {}),
    };
    const stored = this.#ring(child).push(line);
    if (early) this.#rendered.add(stored);
    return stored;
  }

  /** A stored line as it is shown: rendered by its child's renderer, when it has one. */
  render(line: LogLine): LogLine {
    const renderer = this.#renderers[line.child];
    if (!renderer || this.#rendered.has(line)) return line;
    const text = renderer(line.text);
    return text === line.text ? line : { ...line, text };
  }

  /**
   * Read one child's ring, or every ring merged back into sequence order. The
   * merge is what makes the status page's unfiltered view read like one
   * interleaved stream.
   */
  read(child?: ChildId | "supervisor", afterSeq = 0, limit?: number): LogLine[] {
    if (child) return this.#ring(child).read(afterSeq, limit ?? Number.MAX_SAFE_INTEGER).map((l) => this.render(l));
    const merged = [...this.#rings.values()].flatMap((ring) => ring.read(afterSeq, limit ?? Number.MAX_SAFE_INTEGER));
    merged.sort((a, b) => a.seq - b.seq);
    const window = limit !== undefined && merged.length > limit ? merged.slice(merged.length - limit) : merged;
    return window.map((l) => this.render(l));
  }

  /** One child's newest line, rendered. */
  last(child: ChildId | "supervisor"): LogLine | undefined {
    const line = this.#rings.get(child)?.last();
    return line ? this.render(line) : undefined;
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
