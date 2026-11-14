import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { FIRST_WINDOW, gaussianNoise, MAX_FRAMES, MAX_WORD_FRAMES, piece, pieceSeed, roundHalfEven, timeline, windows } from "./plan.js";

/**
 * `testdata/timeline.json` is `engine.py`'s plan, run in torch on the real
 * text stage's durations for one sentence at three speeds, plus a made-up
 * case long enough to hit both the per-word and the per-piece frame caps.
 */
interface Case {
  text: string;
  ids: number[];
  cw: number[];
  wstart: number[];
  dur: number[];
  speed: number;
  scaledDur: number[];
  counts: number[];
  frames: number;
  fw: number[];
  fp: number[];
  firstWindows: [number, number][];
  laterWindows: [number, number][];
}

const read = <T>(name: string): T => JSON.parse(readFileSync(path.join(import.meta.dirname, "testdata", name), "utf8")) as T;
const cases = read<Case[]>("timeline.json");
const stoi = new Map(read<{ vocab: string[] }>("ema.json").vocab.map((ch, i) => [ch, i]));

describe("piece", () => {
  it.each(cases.map((c) => [c.text.slice(0, 30), c] as const))("letters and words match engine.py for %j", (_label, c) => {
    const p = piece(c.text, stoi);
    expect(Array.from(p.ids, Number)).toEqual(c.ids);
    expect(Array.from(p.cw, Number)).toEqual(c.cw);
    expect(Array.from(p.wstart, Number)).toEqual(c.wstart);
    expect(p.words).toBe(c.cw[c.cw.length - 1] + 1);
  });
});

describe("timeline", () => {
  it.each(cases.map((c) => [c.text.slice(0, 30), c.speed, c] as const))("matches engine.py for %j at speed %d", (_label, _speed, c) => {
    const p = piece(c.text, stoi);
    const t = timeline(Float32Array.from(c.dur), p.cw, p.words, c.speed);
    expect(Array.from(t.dur)).toEqual(Array.from(Float32Array.from(c.scaledDur)));
    expect(t.counts).toEqual(c.counts);
    expect(t.frames).toBe(c.frames);
    expect(Array.from(t.fw, Number)).toEqual(c.fw);
    expect(Array.from(t.fp)).toEqual(Array.from(Float32Array.from(c.fp)));
    expect(windows(t.frames, FIRST_WINDOW)).toEqual(c.firstWindows);
    expect(windows(t.frames)).toEqual(c.laterWindows);
  });

  it("caps a word and the piece", () => {
    const c = cases[cases.length - 1];
    expect(c.counts.every((n) => n === MAX_WORD_FRAMES)).toBe(true);
    expect(c.frames).toBe(MAX_FRAMES);
  });

  it("gives every word at least one frame", () => {
    const p = piece("a b c", stoi);
    expect(timeline(new Float32Array(5), p.cw, p.words, 1).counts).toEqual([1, 1, 1]);
  });
});

describe("roundHalfEven", () => {
  it("rounds like torch.round", () => {
    expect([0.5, 1.5, 2.5, 3.5, 2.4999, 2.5001, 7].map(roundHalfEven)).toEqual([0, 2, 2, 4, 2, 3, 7]);
  });
});

describe("gaussianNoise", () => {
  it("is the same for a seed and different across seeds", () => {
    expect(gaussianNoise(7, 9)).toEqual(gaussianNoise(7, 9));
    expect(gaussianNoise(7, 9)).not.toEqual(gaussianNoise(8, 9));
    expect(pieceSeed(5, 0)).not.toBe(pieceSeed(5, 1));
  });

  it("is standard normal", () => {
    const x = gaussianNoise(123, 200_000);
    const mean = x.reduce((a, b) => a + b, 0) / x.length;
    const variance = x.reduce((a, b) => a + (b - mean) ** 2, 0) / x.length;
    expect(Math.abs(mean)).toBeLessThan(0.01);
    expect(Math.abs(variance - 1)).toBeLessThan(0.02);
    expect(x.every(Number.isFinite)).toBe(true);
  });
});
