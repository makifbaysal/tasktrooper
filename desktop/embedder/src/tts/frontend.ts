/**
 * Text frontend, ported from EMA Lightning's `frontend.py`: any written
 * Turkish in, text in the model's own alphabet out.
 *
 * normalizer-tr reads numbers, dates, times, money, units and abbreviations
 * aloud (its "fallback" policy speaks notation it cannot resolve rather than
 * keeping it as written). The alphabet step then lowercases the Turkish way
 * and drops whatever the model cannot read. Nothing here throws on text.
 *
 * The port has to agree with Python character for character, because the
 * model was trained on exactly what `frontend.py` produces; the fixtures in
 * `testdata/frontend.json` are that script's own output. That is why
 * whitespace is Python's set (`str.split()`, `\s`), not JavaScript's `\s`,
 * which also matches U+FEFF.
 */

export interface TextNormalizer {
  /** The spoken form, or null when the text was refused. */
  normalize(text: string): string | null;
}

/** normalizer-tr refuses longer input in one call, so text goes through in blocks of at most this many bytes. */
export const BLOCK_BYTES = 8 * 1024;

const PY_SPACE = "\\t\\n\\v\\f\\r\\x1c-\\x1f \\x85\\xa0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000";
export const PY_WHITESPACE = new RegExp(`[${PY_SPACE}]+`, "g");

const TURKISH = new Set("çğıöşüÇĞİÖŞÜ");
const TYPOGRAPHY: Record<string, string> = {
  "’": "'",
  "‘": "'",
  "ʼ": "'",
  "´": "'",
  "`": "'",
  "“": '"',
  "”": '"',
  "„": '"',
  "«": '"',
  "»": '"',
  "–": "-",
  "—": "-",
  "−": "-",
  "…": "...",
};
const TYPOGRAPHY_RE = new RegExp(`[${Object.keys(TYPOGRAPHY).join("")}]`, "g");
// Control characters and bidi overrides: nothing the model can say, and a
// bidi override is how text that reads one way on screen says another.
// eslint-disable-next-line no-control-regex -- matching control characters is the point.
const UNSAFE = /[\x00-\x08\x0b-\x1f\x7f-\x9f؜‎‏‪-‮⁦-⁩]/g;
// Python's encode("utf-8", "ignore") drops lone surrogates; this is the same.
const LONE_SURROGATE = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g;
const MARK = /\p{M}/gu;

const utf8 = new TextEncoder();

export class Frontend {
  readonly #vocab: Set<string>;
  readonly #normalizer: TextNormalizer | null;

  constructor(vocab: readonly string[], normalizer: TextNormalizer | null) {
    this.#vocab = new Set(vocab);
    this.#normalizer = normalizer;
  }

  run(text: string): string {
    const clean = text.replace(LONE_SURROGATE, "").replace(UNSAFE, " ");
    if (pyStrip(clean) === "") return "";
    return this.alphabet(blocks(clean).map((block) => this.spoken(block)).join(" "));
  }

  /** A refused block keeps its words rather than losing the sentence. */
  spoken(text: string): string {
    if (!this.#normalizer) return text;
    try {
      return this.#normalizer.normalize(text) ?? text;
    } catch {
      return text;
    }
  }

  alphabet(text: string): string {
    const lowered = text.replace(TYPOGRAPHY_RE, (ch) => TYPOGRAPHY[ch] ?? ch).replaceAll("İ", "i").replaceAll("I", "ı").toLowerCase();
    let out = "";
    for (let ch of lowered) {
      // Turkish letters stay whole; NFKD would split ç into c and a cedilla.
      // `\p{M}` stands in for Python's `combining(c) != 0`: the two differ only
      // for marks on bases the vocabulary does not have, which become a space
      // either way.
      if (!TURKISH.has(ch)) ch = ch.normalize("NFKD").replace(MARK, "");
      out += ch !== "" && [...ch].every((c) => this.#vocab.has(c)) ? ch : " ";
    }
    return pyStrip(out.replace(PY_WHITESPACE, " "));
  }
}

/** Split at whitespace into blocks the normalizer accepts in one call. */
export function blocks(text: string): string[] {
  const out: string[] = [];
  let block: string[] = [];
  let size = 0;
  for (const word of pyStrip(text).split(PY_WHITESPACE)) {
    if (word === "") continue;
    const n = utf8.encode(word).length + 1;
    if (block.length > 0 && size + n > BLOCK_BYTES) {
      out.push(block.join(" "));
      block = [];
      size = 0;
    }
    block.push(word);
    size += n;
  }
  if (block.length > 0) out.push(block.join(" "));
  return out;
}

const LEADING = new RegExp(`^[${PY_SPACE}]+`);
const TRAILING = new RegExp(`[${PY_SPACE}]+$`);

/** Python's `str.strip()`. */
export function pyStrip(text: string): string {
  return text.replace(LEADING, "").replace(TRAILING, "");
}
