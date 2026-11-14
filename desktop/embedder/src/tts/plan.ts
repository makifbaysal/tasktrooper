/**
 * The arithmetic between EMA's three ONNX graphs, ported from `engine.py`
 * (and checked against `parity.py`, which matched the PyTorch package to
 * 4e-4): letter ids and word boundaries for a piece, the frame timeline the
 * durations imply, the decoder's windows, and the sampling noise.
 *
 * Kept apart from `ema.ts` so it can be tested without onnxruntime.
 */

export const RATE = 48_000;
/** A stream's first window is one second (25 frames at 25 Hz), so its first audio comes fast. */
export const FIRST_WINDOW = 25;
/** Four seconds for every later window. */
export const WINDOW = 100;
/** Frames decoded on each side of a window; the decoder reaches 4. */
export const CONTEXT = 8;
export const MAX_WORD_FRAMES = 250;
export const MAX_FRAMES = 3000;

export interface Piece {
  text: string;
  ids: BigInt64Array;
  /** The word each letter belongs to. */
  cw: BigInt64Array;
  /** The first letter of that word. */
  wstart: BigInt64Array;
  words: number;
}

/**
 * A word starts at every non-space letter after a space; the spaces before it
 * belong to the word before. A letter outside the vocabulary is id 1 (`<unk>`),
 * which the frontend has already made impossible.
 */
export function piece(text: string, stoi: ReadonlyMap<string, number>): Piece {
  const letters = [...text];
  const starts: number[] = [];
  letters.forEach((ch, i) => {
    if (ch !== " " && (i === 0 || letters[i - 1] === " ")) starts.push(i);
  });
  if (starts.length === 0) starts.push(0);
  const bounds = [0, ...starts.slice(1), letters.length];
  const n = letters.length;
  const ids = new BigInt64Array(n);
  const cw = new BigInt64Array(n);
  const wstart = new BigInt64Array(n);
  letters.forEach((ch, i) => {
    ids[i] = BigInt(stoi.get(ch) ?? 1);
  });
  for (let w = 0; w + 1 < bounds.length; w++) {
    for (let i = bounds[w]; i < bounds[w + 1]; i++) {
      cw[i] = BigInt(w);
      wstart[i] = BigInt(bounds[w]);
    }
  }
  return { text, ids, cw, wstart, words: bounds.length - 1 };
}

export interface Timeline {
  /** Letter durations divided by speed: what the sound stage is given. */
  dur: Float32Array;
  /** Frames per word. */
  counts: number[];
  frames: number;
  /** The word each frame belongs to. */
  fw: BigInt64Array;
  /** How far through its word each frame is, in [0, 1). */
  fp: Float32Array;
}

/**
 * `Engine.plan` after the text stage. Word lengths are summed in float32 in
 * letter order, as torch's `scatter_add_` does, and rounded half to even, as
 * `torch.round` does, so a word whose duration lands on .5 frames gets the
 * frame count the model was trained to expect.
 */
export function timeline(rawDur: Float32Array, cw: BigInt64Array, words: number, speed: number): Timeline {
  const dur = new Float32Array(rawDur.length);
  for (let i = 0; i < rawDur.length; i++) dur[i] = rawDur[i] / speed;
  const sums = new Float32Array(words);
  for (let i = 0; i < dur.length; i++) {
    const w = Number(cw[i]);
    sums[w] = Math.fround(sums[w] + dur[i]);
  }
  const counts = Array.from(sums, (s) => Math.min(MAX_WORD_FRAMES, Math.max(1, roundHalfEven(s))));
  const total = counts.reduce((a, b) => a + b, 0);
  const frames = Math.min(total, MAX_FRAMES);
  const fw = new BigInt64Array(frames);
  const fp = new Float32Array(frames);
  let f = 0;
  for (let w = 0; w < counts.length && f < frames; w++) {
    const n = counts[w];
    for (let k = 0; k < n && f < frames; k++, f++) {
      fw[f] = BigInt(w);
      fp[f] = k / n;
    }
  }
  return { dur, counts, frames, fw, fp };
}

/** (start, end) of every decoded window: `first` frames first, then WINDOW each. */
export function windows(frames: number, first: number = WINDOW): [number, number][] {
  const spans: [number, number][] = [];
  let s = 0;
  while (s < frames) {
    const e = Math.min(frames, s + (s === 0 ? first : WINDOW));
    spans.push([s, e]);
    s = e;
  }
  return spans;
}

export function roundHalfEven(x: number): number {
  const r = Math.round(x);
  return Math.abs(x % 1) === 0.5 && r % 2 !== 0 ? r - 1 : r;
}

/**
 * Standard normal float32 noise from a seed: xoshiro128** seeded through
 * splitmix32, Box-Muller for the shape. It does not reproduce torch's
 * generator and need not: a seed only has to make the same piece sound the
 * same twice, and different pieces different.
 */
export function gaussianNoise(seed: number, length: number): Float32Array {
  let sm = seed >>> 0;
  const splitmix = (): number => {
    sm = (sm + 0x9e3779b9) >>> 0;
    let z = sm;
    z = Math.imul(z ^ (z >>> 16), 0x85ebca6b) >>> 0;
    z = Math.imul(z ^ (z >>> 13), 0xc2b2ae35) >>> 0;
    return (z ^ (z >>> 16)) >>> 0;
  };
  let a = splitmix();
  let b = splitmix();
  let c = splitmix();
  let d = splitmix();
  const next = (): number => {
    const result = Math.imul(rotl(Math.imul(b, 5), 7), 9) >>> 0;
    const t = b << 9;
    c ^= a;
    d ^= b;
    b ^= c;
    a ^= d;
    c ^= t;
    d = rotl(d, 11);
    return result;
  };
  // (0, 1], never 0, so the log below is finite.
  const uniform = (): number => (next() + 1) / 4294967296;
  const out = new Float32Array(length);
  for (let i = 0; i < length; i += 2) {
    const r = Math.sqrt(-2 * Math.log(uniform()));
    const theta = 2 * Math.PI * uniform();
    out[i] = r * Math.cos(theta);
    if (i + 1 < length) out[i + 1] = r * Math.sin(theta);
  }
  return out;
}

function rotl(x: number, k: number): number {
  return ((x << k) | (x >>> (32 - k))) >>> 0;
}

/** A seed for each piece of one request, as `api.py` derives them, folded to the 32 bits the generator takes. */
export function pieceSeed(seed: number, index: number): number {
  const mixed = (BigInt(seed) * 1_000_003n + BigInt(index)) % (1n << 63n);
  return Number((mixed ^ (mixed >> 32n)) & 0xffffffffn);
}
