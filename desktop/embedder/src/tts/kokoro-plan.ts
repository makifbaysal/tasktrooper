/**
 * The arithmetic around Kokoro's one ONNX graph: phonemes to token ids, a
 * phoneme string cut into passes the model accepts, and the style row each
 * pass reads. Kept apart from `kokoro.ts` so it can be tested without
 * onnxruntime, as `plan.ts` is for EMA.
 */

export const KOKORO_RATE = 24_000;
/** The model's context is 512 tokens, two of them the pads either side. */
export const MAX_PHONEMES = 510;
export const STYLE_DIM = 256;
/** A voice file holds one style row per input length, 0 to 509 phonemes. */
export const STYLE_ROWS = 510;
/**
 * The first pass of a text is cut at its first clause mark past this many
 * phonemes (about a second of speech), so the first audio comes after one
 * short run rather than a whole long sentence.
 */
export const FIRST_PASS_PHONEMES = 30;

/** Token ids for one pass: `$`, the phonemes the vocabulary has, `$`. */
export function tokenIds(phonemes: string, vocab: ReadonlyMap<string, number>): BigInt64Array {
  const ids: number[] = [0];
  for (const ch of phonemes) {
    const id = vocab.get(ch);
    if (id !== undefined) ids.push(id);
  }
  ids.push(0);
  return BigInt64Array.from(ids, (n) => BigInt(n));
}

/** The style row for a pass of `phonemes` tokens (pads excluded), as kokoro.js indexes it. */
export function styleRow(pack: Float32Array, phonemes: number): Float32Array {
  const row = Math.min(Math.max(phonemes, 0), STYLE_ROWS - 1);
  return pack.subarray(row * STYLE_DIM, (row + 1) * STYLE_DIM);
}

const SENTENCE_END = /(?<=[.!?…]["”)]*) +/;
const CLAUSE = /[,;:—]["”)]*(?= )/g;

/**
 * A phoneme string in passes of at most `max` characters: one sentence per
 * pass, a sentence too long for one cut at its last clause mark that fits,
 * then its last space, then hard. The first sentence is also cut at its first
 * clause mark past FIRST_PASS_PHONEMES, for the reason given there.
 */
export function passes(phonemes: string, max: number = MAX_PHONEMES): string[] {
  const out: string[] = [];
  for (const sentence of phonemes.trim().split(SENTENCE_END)) {
    let rest = sentence.trim();
    if (out.length === 0 && rest.length > FIRST_PASS_PHONEMES) {
      for (const m of rest.matchAll(CLAUSE)) {
        const end = m.index + m[0].length;
        if (end < FIRST_PASS_PHONEMES || end > max) continue;
        if (rest.length - end > 10) {
          out.push(rest.slice(0, end));
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
      out.push(rest.slice(0, cut).trim());
      rest = rest.slice(cut).trim();
    }
    if (/[^\s;:,.!?—…"“”()]/.test(rest)) out.push(rest);
  }
  return out.filter((p) => p !== "");
}
