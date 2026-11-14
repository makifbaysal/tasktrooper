/**
 * What of an agent's markdown reply is read aloud, and in which pieces.
 *
 * The voice reads the way a person talks: fenced blocks (code, `visual`
 * drawings, tables the agent fenced) are on the screen, not in the voice;
 * markdown marks, link targets, parentheticals, emoji and bare URLs are
 * dropped; list items and headings become sentences. Punctuation stays — the
 * voice uses it for pauses and intonation and never pronounces it.
 */

const FENCE = /^\s*(```|~~~)/;

/** The reply with every fenced block removed, including one still being written. */
export function withoutFences(markdown: string): string {
  const out: string[] = [];
  let inFence = false;
  for (const line of markdown.split("\n")) {
    if (FENCE.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (!inFence) out.push(line);
  }
  return out.join("\n");
}

const EMOJI = /[\p{Extended_Pictographic}\u{1F1E6}-\u{1F1FF}\u{FE0F}\u{200D}]/gu;

/** One markdown line → plain spoken text (may be empty). */
function speakLine(line: string): string {
  let s = line;
  // A table row is data for the eye.
  if (/^\s*\|.*\|\s*$/.test(s)) return "";
  // Horizontal rules, block quotes, headings, list markers.
  if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(s)) return "";
  s = s.replace(/^\s*>+\s?/, "");
  s = s.replace(/^\s*#{1,6}\s+/, "");
  s = s.replace(/^\s*(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?/, "");
  // Images say nothing; links keep their words, never their target.
  s = s.replace(/!\[[^\]]*\]\([^)]*\)/g, "");
  s = s.replace(/\[([^\]]+)\]\([^)]*\)/g, "$1");
  s = s.replace(/<https?:\/\/[^>]+>/g, "");
  s = s.replace(/\bhttps?:\/\/\S+/g, "");
  // Inline code is usually a name the voice would spell; keep the word, drop the ticks.
  s = s.replace(/`([^`]*)`/g, "$1");
  // Emphasis and strike marks.
  s = s.replace(/(\*\*|__|\*|_|~~)(?=\S)([\s\S]*?\S)\1/g, "$2");
  s = s.replace(/[*_~]{2,}/g, "");
  // Asides in brackets are for reading, not for hearing.
  s = s.replace(/\s*\([^()]*\)/g, "");
  s = s.replace(/\s*\[[^\][]*\]/g, "");
  // Tags are dropped until none is left: one pass can leave a new one behind ("<<b>x>").
  for (let prev = ""; prev !== s; ) {
    prev = s;
    s = s.replace(/<[^<>]*>/g, "");
  }
  s = s.replace(EMOJI, "");
  s = s.replace(/[#*_`|<>{}[\]\\^]/g, " ");
  s = s.replace(/\s+/g, " ").replace(/\s+([.,;:!?…])/g, "$1").trim();
  return s;
}

/** The whole spoken text of a finished (or partial) markdown reply. */
export function spokenText(markdown: string): string {
  const lines = withoutFences(markdown)
    .split("\n")
    .map(speakLine)
    .filter((l) => l.length > 0);
  // A heading or a list item ends a thought even without a full stop.
  return lines.map((l) => (/[.!?…:;,]$/.test(l) ? l : `${l}.`)).join(" ").replace(/\s+/g, " ").trim();
}

const SENTENCE_END = /[.!?…]+["'”’)]*\s+/g;

/**
 * Splits off the sentences of `text` that are certainly complete, given that
 * more text may still arrive. Returns them and the offset where the
 * unfinished tail begins.
 */
export function completeSentences(text: string): { sentences: string[]; consumed: number } {
  const sentences: string[] = [];
  let start = 0;
  for (const match of text.matchAll(SENTENCE_END)) {
    const end = (match.index ?? 0) + match[0].length;
    const sentence = text.slice(start, end).trim();
    // "Dr. Ahmet", "3. kat" or "J. Smith": an abbreviation, an ordinal or an initial is not a sentence end.
    const last = /(?:^|\s)([\p{L}.]+|\d+)\.$/u.exec(sentence)?.[1];
    if (last && !/[!?…]$/.test(sentence) && isAbbreviation(last)) continue;
    if (sentence) sentences.push(sentence);
    start = end;
  }
  return { sentences, consumed: start };
}

const ABBREVIATIONS = new Set([
  "dr", "prof", "doç", "av", "bkz", "vs", "vb", "örn", "sn", "yy", "no", "mr", "mrs", "ms", "st", "jr", "sr",
  "e.g", "i.e", "etc", "inc", "ltd", "vol", "fig", "approx",
]);

function isAbbreviation(word: string): boolean {
  return /^\d+$/.test(word) || /^\p{L}$/u.test(word) || ABBREVIATIONS.has(word.toLocaleLowerCase("tr"));
}

/**
 * Feeds a reply that grows token by token and hands out each sentence once,
 * as soon as it is complete. `finish()` hands out whatever is left.
 */
export class SentenceFeed {
  #spokenUpTo = 0;

  /** New sentences of `markdown` (the reply so far). */
  push(markdown: string): string[] {
    const text = spokenText(stableText(markdown));
    if (text.length < this.#spokenUpTo) {
      // The reply was rewritten under us (a stream restart); start over from it.
      this.#spokenUpTo = 0;
    }
    const { sentences, consumed } = completeSentences(text.slice(this.#spokenUpTo) + " ");
    this.#spokenUpTo += Math.min(consumed, text.length - this.#spokenUpTo);
    return sentences;
  }

  /** The rest of the finished reply. */
  finish(markdown: string): string[] {
    const text = spokenText(markdown);
    const rest = text.slice(this.#spokenUpTo).trim();
    this.#spokenUpTo = text.length;
    return rest ? [rest] : [];
  }

  reset(): void {
    this.#spokenUpTo = 0;
  }
}

/**
 * The part of a streaming reply that can be read without changing later:
 * every finished line, plus the finished sentences of the line still being
 * written, up to where something is left open ("**bold", "(aside", a `code`
 * span, a [link) whose closing would change what is said). A line that may
 * become a table row, a fence or a heading is left until it ends.
 */
export function stableText(markdown: string): string {
  const nl = markdown.lastIndexOf("\n");
  const done = nl === -1 ? "" : markdown.slice(0, nl + 1);
  const tail = markdown.slice(nl + 1);
  if (/^\s*(?:\||```|~~~|#)/.test(tail) || insideFence(done)) return done;
  let safe = 0;
  for (const match of tail.matchAll(/[.!?…]+["'”’]*(?=\s)/g)) {
    const end = (match.index ?? 0) + match[0].length;
    if (balanced(tail.slice(0, end))) safe = end;
  }
  return safe === 0 ? done : `${done}${tail.slice(0, safe)}\n`;
}

function insideFence(markdown: string): boolean {
  let open = false;
  for (const line of markdown.split("\n")) if (FENCE.test(line)) open = !open;
  return open;
}

function balanced(text: string): boolean {
  const count = (re: RegExp) => (text.match(re) ?? []).length;
  return (
    count(/\(/g) === count(/\)/g) &&
    count(/\[/g) === count(/\]/g) &&
    count(/`/g) % 2 === 0 &&
    count(/\*\*/g) % 2 === 0 &&
    count(/~~/g) % 2 === 0
  );
}
