import { pyStrip } from "./frontend.js";

/**
 * Greedy chunking, ported from EMA Lightning's `chunker.py`: spoken text in,
 * pieces short enough for one pass of the model out.
 *
 * Text that fits in about ten seconds of speech stays one piece; the model
 * reads its punctuation itself. Longer text is cut at the last good spot
 * inside each window: a sentence end, then a clause mark, then a space, and
 * only when there is none of those, exactly at the limit.
 *
 * Indexes are UTF-16 units where Python's are code points. They agree on what
 * this is given, the frontend's output, which is all one alphabet in the BMP.
 */

const LETTERS_PER_SECOND = 18.0;
const MAX_SECONDS = 10.0;
export const MAX_LETTERS = 250;
export const SENTENCE_PAUSE = 0.25;
export const CLAUSE_PAUSE = 0.12;

// Python's whitespace for `\S`, so a cut lands where `chunker.py` would cut.
const NOT_SPACE = "[^\\t\\n\\v\\f\\r\\x1c-\\x1f \\x85\\xa0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000]";
const CUTS: readonly [RegExp, number][] = [
  [/[.!?]+["')]*(?= )/g, SENTENCE_PAUSE],
  [/[,;:](?= )/g, CLAUSE_PAUSE],
  [new RegExp(`${NOT_SPACE}(?= )`, "g"), CLAUSE_PAUSE],
];
// Python's `[^\W\d_]`: anything alphanumeric that is not a decimal digit.
const LETTER = /[\p{L}\p{Nl}\p{No}]/u;

export interface Chunk {
  text: string;
  /** Seconds of silence after it; 0 after the last. */
  pause: number;
}

export function chunk(text: string, speed: number): Chunk[] {
  const limit = Math.trunc(Math.min(MAX_LETTERS, LETTERS_PER_SECOND * MAX_SECONDS * speed));
  const pieces: Chunk[] = [];
  let rest = pyStrip(text);
  while (rest !== "") {
    let cut = rest.length;
    let pause = 0.0;
    if (rest.length > limit) {
      cut = limit;
      // Python's finditer(rest, 0, limit + 1) sees the string as ending there,
      // so a lookahead cannot reach past it either.
      const window = rest.slice(0, limit + 1);
      for (const [pattern, gap] of CUTS) {
        let last = -1;
        for (const m of window.matchAll(pattern)) last = m.index + m[0].length;
        if (last !== -1) {
          cut = last;
          pause = gap;
          break;
        }
      }
    }
    const piece = pyStrip(rest.slice(0, cut));
    rest = pyStrip(rest.slice(cut));
    if (LETTER.test(piece)) pieces.push({ text: finish(piece), pause });
  }
  const last = pieces.at(-1);
  if (last) last.pause = 0.0;
  return pieces;
}

/** End every piece the way the model was trained: on a sentence end. */
function finish(piece: string): string {
  const end = piece.replace(/["')]+$/, "").slice(-1);
  if (end === "." || end === "!" || end === "?") return piece;
  return piece.replace(/[,;:\- ]+$/, "") + ".";
}
