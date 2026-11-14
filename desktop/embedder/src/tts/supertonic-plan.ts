import { cardinalWords } from "./english.js";

/**
 * The arithmetic and text handling around Supertonic 3's four ONNX graphs,
 * kept apart from `supertonic.ts` so it can be tested without onnxruntime, as
 * `plan.ts` is for EMA and `kokoro-plan.ts` for Kokoro.
 *
 * Supertonic reads characters, not phonemes: NFKD-normalized text wrapped in a
 * language tag (`<tr>…</tr>`), each code point mapped through the model's
 * unicode index. `prepareText` is the official runtime's `_preprocess_text`
 * (supertone-inc/supertonic, MIT), less the tag.
 */

export const SUPERTONIC_RATE = 44_100;
/**
 * Flow-matching steps. The official runtime's default is 8; at 5 Apple's
 * recognizer transcribes back as well (the same words on every test sentence,
 * both languages) for 5/8 of the work, and the steps are nearly all of it.
 * At 4 and 3 it starts mishearing words.
 */
export const SUPERTONIC_STEPS = 5;
/** The official runtime's own default speed; ours is relative to it, so 1 sounds like theirs. */
export const BASE_SPEED = 1.05;
/** The official runtime's chunk limit for these languages. */
export const MAX_PASS_CHARS = 300;
/**
 * The first sentence of a text is cut at its first clause mark past this many
 * characters, so the first audio comes after one short pass rather than a
 * whole long sentence; every pass is generated whole before any of it plays.
 */
export const FIRST_PASS_CHARS = 20;
/** Silence after a pass that ends a sentence, and after one cut at a clause mark. */
export const SENTENCE_PAUSE = 0.2;
export const CLAUSE_PAUSE = 0.08;

export interface Pass {
  text: string;
  /** Seconds of silence after it; 0 after the last. */
  pause: number;
}

export interface SupertonicConfig {
  ae: { sample_rate: number; base_chunk_size: number };
  ttl: { latent_dim: number; chunk_compress_factor: number };
}

// eslint-disable-next-line no-control-regex -- matching control characters is the point.
const UNSAFE = /[\x00-\x08\x0b-\x1f\x7f-\x9f؜‎‏‪-‮⁦-⁩]/g;
const EMOJI = /[\u{1F600}-\u{1F64F}\u{1F300}-\u{1F5FF}\u{1F680}-\u{1F6FF}\u{1F700}-\u{1F77F}\u{1F780}-\u{1F7FF}\u{1F800}-\u{1F8FF}\u{1F900}-\u{1F9FF}\u{1FA00}-\u{1FA6F}\u{1FA70}-\u{1FAFF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}\u{1F1E6}-\u{1F1FF}]+/gu;
const SYMBOLS: [string, string][] = [
  ["–", "-"],
  ["‑", "-"],
  ["—", "-"],
  ["_", " "],
  ["“", '"'],
  ["”", '"'],
  ["‘", "'"],
  ["’", "'"],
  ["´", "'"],
  ["`", "'"],
  ["[", " "],
  ["]", " "],
  ["|", " "],
  ["/", " "],
  ["#", " "],
  ["→", " "],
  ["←", " "],
];
const EXPRESSIONS: [string, string][] = [
  ["@", " at "],
  ["e.g.,", "for example, "],
  ["i.e.,", "that is, "],
];
const ENDS_WELL = /[.!?;:,'"')\]}…。」』】〉》›»]$/;

/** The official preprocessing: NFKD, typography folded, emoji and decorations dropped, a period added when the text has no end. */
export function prepareText(text: string): string {
  let t = text.replace(UNSAFE, " ").normalize("NFKD").replace(EMOJI, "");
  for (const [from, to] of SYMBOLS) t = t.replaceAll(from, to);
  t = t.replace(/[♥☆♡©\\]/g, "");
  for (const [from, to] of EXPRESSIONS) t = t.replaceAll(from, to);
  t = t.replace(/ ([,.!?;:'])/g, "$1");
  t = t.replace(/"{2,}/g, '"').replace(/'{2,}/g, "'");
  t = t.replace(/\s+/g, " ").trim();
  if (t !== "" && !ENDS_WELL.test(t)) t += ".";
  return t;
}

/**
 * Token ids for one prepared pass: `<lang>`, the text, `</lang>`, each code
 * point through the index. A character the index has no entry for is left
 * out (the official runtime would hand the model -1). Null when nothing
 * sayable is left.
 */
export function textIds(prepared: string, lang: string, indexer: ArrayLike<number>): BigInt64Array | null {
  const ids: number[] = [];
  let spoken = 0;
  const add = (part: string, counts: boolean): void => {
    for (const ch of part) {
      const cp = ch.codePointAt(0) ?? 0;
      const id = cp < indexer.length ? indexer[cp] : -1;
      if (id < 0) continue;
      ids.push(id);
      if (counts && /[\p{L}\p{N}]/u.test(ch)) spoken++;
    }
  };
  add(`<${lang}>`, false);
  add(prepared, true);
  add(`</${lang}>`, false);
  return spoken === 0 ? null : BigInt64Array.from(ids, (n) => BigInt(n));
}

/** Samples of audio for `seconds`, and the latent frames that hold them. */
export function latentShape(seconds: number, config: SupertonicConfig): { samples: number; frames: number; channels: number } {
  const samples = Math.max(1, Math.floor(seconds * config.ae.sample_rate));
  const chunk = config.ae.base_chunk_size * config.ttl.chunk_compress_factor;
  return { samples, frames: Math.ceil(samples / chunk), channels: config.ttl.latent_dim * config.ttl.chunk_compress_factor };
}

const ABBREVIATIONS = new Set(["mr", "mrs", "ms", "dr", "prof", "sr", "jr", "st", "vs", "etc", "inc", "ltd", "co", "corp", "ave", "blvd", "no", "e.g", "i.e", "ph.d"]);
const SENTENCE_END = /[.!?…]+["'”’)\]]*\s+/g;
const CLAUSE = /[,;:]["'”’)\]]*(?=\s)/g;

/** Sentences, not split after an abbreviation ("Dr.", "e.g.") or a lone capital initial ("J."). */
export function sentences(text: string): string[] {
  const t = text.replace(/\s+/g, " ").trim();
  const out: string[] = [];
  let start = 0;
  for (const m of t.matchAll(SENTENCE_END)) {
    const end = m.index + m[0].length;
    const before = t.slice(start, m.index + 1);
    const word = /(\S+)\.$/.exec(before)?.[1] ?? "";
    if (m[0].startsWith(".") && (ABBREVIATIONS.has(word.toLowerCase()) || /^\p{Lu}$/u.test(word))) continue;
    out.push(t.slice(start, end).trim());
    start = end;
  }
  if (start < t.length) out.push(t.slice(start).trim());
  return out.filter((s) => s !== "");
}

/**
 * A text in passes: one sentence each, a sentence longer than `max` cut at
 * its last clause mark that fits, then its last space, then hard. The first
 * sentence is also cut at its first clause mark past FIRST_PASS_CHARS when
 * enough follows it, for the reason given there. Long passes are not merged:
 * the next pass is made while this one plays, so a pass must not take longer
 * to make than the one before it takes to say.
 */
export function passes(text: string, max: number = MAX_PASS_CHARS): Pass[] {
  const out: Pass[] = [];
  for (const sentence of sentences(text)) {
    let rest = sentence;
    if (out.length === 0) {
      for (const m of rest.matchAll(CLAUSE)) {
        const end = m.index + m[0].length;
        if (end < FIRST_PASS_CHARS) continue;
        if (end <= max && rest.length - end > 10) {
          out.push({ text: rest.slice(0, end), pause: CLAUSE_PAUSE });
          rest = rest.slice(end).trim();
        }
        break;
      }
    }
    while (rest.length > max) {
      const window = rest.slice(0, max + 1);
      let cut = -1;
      for (const m of window.matchAll(CLAUSE)) cut = m.index + m[0].length;
      if (cut <= 0) cut = window.lastIndexOf(" ");
      if (cut <= 0) cut = max;
      out.push({ text: rest.slice(0, cut).trim(), pause: CLAUSE_PAUSE });
      rest = rest.slice(cut).trim();
    }
    if (/[\p{L}\p{N}]/u.test(rest)) out.push({ text: rest, pause: SENTENCE_PAUSE });
  }
  const sayable = out.filter((p) => /[\p{L}\p{N}]/u.test(p.text));
  const last = sayable.at(-1);
  if (last) last.pause = 0;
  return sayable;
}

const UNIT_SUFFIX = ["ıncı", "inci", "nci", "üncü", "üncü", "inci", "ncı", "nci", "inci", "uncu"];
const TEN_SUFFIX = ["", "uncu", "nci", "uncu", "ıncı", "nci", "ıncı", "inci", "inci", "ıncı"];

/** The written ordinal suffix of `n` ("3’üncü", "6’ncı"), from the last word of how it is said. */
export function ordinalSuffix(n: number): string {
  if (n === 0) return "ıncı";
  if (n % 10 !== 0) return UNIT_SUFFIX[n % 10];
  if (n % 100 !== 0) return TEN_SUFFIX[(n % 100) / 10];
  if (n % 1000 !== 0) return "üncü";
  if (n % 1_000_000 !== 0) return "inci";
  if (n % 1_000_000_000 !== 0) return "uncu";
  return "ıncı";
}

/**
 * "3. kat" as "3’üncü kat", before normalizer-tr: a number with a period and
 * then a lowercase word is an ordinal in Turkish, which the normalizer reads
 * as a cardinal and a full stop ("üç. kat"), but it reads "3’üncü" right.
 * Supertonic itself reads "3. kat" right and "%25" or "1.250 TL" wrong; with
 * this the normalizer gets all of them.
 */
export function turkishOrdinals(text: string): string {
  return text.replace(/(?<![\p{L}\p{N}.,:’'])(\d{1,9})\.(?=[ \t]+\p{Ll})/gu, (_m, digits: string) => `${digits}’${ordinalSuffix(Number(digits))}`);
}

const MONEY_UNITS: Record<string, [string, string, string, string]> = {
  $: ["dollar", "dollars", "cent", "cents"],
  "£": ["pound", "pounds", "penny", "pence"],
  "€": ["euro", "euros", "cent", "cents"],
};
const MONEY = /([$£€])(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d+))?(?:\s+(thousand|million|billion|trillion)\b)?(?![\d,.]\d)/g;

/**
 * English text with what Supertonic misreads on its own spelled out: amounts
 * of money ("$1,250" came back as "one done $50") and percentages, which it
 * drops at the end of a short sentence ("CPU usage hit 25%." came back as
 * "CPU usage hit"). Times, dates, ordinals and plain numbers it reads right.
 */
export function englishSpoken(text: string): string {
  return englishMoney(text).replace(/(\d)\s?%/g, "$1 percent");
}

function englishMoney(text: string): string {
  return text.replace(MONEY, (_m, symbol: string, whole: string, frac: string | undefined, scale: string | undefined) => {
    const [one, many, cent, cents] = MONEY_UNITS[symbol];
    if (scale) return `${whole}${frac ? `.${frac}` : ""} ${scale} ${many}`;
    const n = BigInt(whole.replaceAll(",", ""));
    const main = `${cardinalWords(n)} ${n === 1n ? one : many}`;
    if (frac === undefined || frac.length > 2) return frac === undefined ? main : `${cardinalWords(n)} point ${[...frac].map((d) => cardinalWords(BigInt(d))).join(" ")} ${many}`;
    const c = BigInt(frac.padEnd(2, "0"));
    if (c === 0n) return main;
    const change = `${cardinalWords(c)} ${c === 1n ? cent : cents}`;
    return n === 0n ? change : `${main} and ${change}`;
  });
}

/** Frames of 10 ms quieter than this far below the loudest are silence. */
const SILENCE_DB = -45;
const LEAD_KEEP = 0.03;
const TAIL_KEEP = 0.06;

/**
 * Where the speech in a pass starts and ends, with a few milliseconds kept
 * either side. Supertonic pads every pass with about half a second of silence
 * at each end; left in, that is half a second before the first word and a
 * second between sentences. [0, 0] for a pass with no sound in it.
 */
export function speechBounds(pcm: Float32Array, rate: number): [number, number] {
  const frame = Math.max(1, Math.round(rate / 100));
  const n = Math.floor(pcm.length / frame);
  const rms = new Float64Array(n);
  let peak = 0;
  for (let f = 0; f < n; f++) {
    let sum = 0;
    for (let i = f * frame; i < (f + 1) * frame; i++) sum += pcm[i] * pcm[i];
    rms[f] = Math.sqrt(sum / frame);
    peak = Math.max(peak, rms[f]);
  }
  if (peak < 1e-4) return [0, 0];
  const floor = peak * 10 ** (SILENCE_DB / 20);
  let first = 0;
  while (first < n && rms[first] < floor) first++;
  let last = n - 1;
  while (last > first && rms[last] < floor) last--;
  const start = Math.max(0, first * frame - Math.round(LEAD_KEEP * rate));
  const end = Math.min(pcm.length, (last + 1) * frame + Math.round(TAIL_KEEP * rate));
  return [start, end];
}
