import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { FIRST_PASS_PHONEMES, MAX_PHONEMES, passes, STYLE_DIM, STYLE_ROWS, styleRow, tokenIds } from "./kokoro-plan.js";

const vocab = new Map(
  Object.entries((JSON.parse(readFileSync(path.join(import.meta.dirname, "testdata/kokoro.json"), "utf8")) as { vocab: Record<string, number> }).vocab),
);

describe("tokenIds", () => {
  it("pads with $ either side and drops what the vocabulary lacks", () => {
    expect(Array.from(tokenIds("həlˈO!", vocab), Number)).toEqual([0, 50, 83, 54, 156, 31, 5, 0]);
    expect(Array.from(tokenIds("h🚀", vocab), Number)).toEqual([0, 50, 0]);
  });
});

describe("styleRow", () => {
  it("takes the row for the pass's length, clamped to the last", () => {
    const pack = Float32Array.from({ length: STYLE_ROWS * STYLE_DIM }, (_, i) => Math.floor(i / STYLE_DIM));
    expect(styleRow(pack, 12)[0]).toBe(12);
    expect(styleRow(pack, 12)).toHaveLength(STYLE_DIM);
    expect(styleRow(pack, 5000)[0]).toBe(STYLE_ROWS - 1);
  });
});

describe("passes", () => {
  it("is one pass per sentence", () => {
    expect(passes("həlˈO. wˈɜɹld! ɹˈIt?")).toEqual(["həlˈO.", "wˈɜɹld!", "ɹˈIt?"]);
  });

  it("cuts a long first sentence at its first clause mark past the first-pass length", () => {
    const head = "a".repeat(FIRST_PASS_PHONEMES + 5) + ",";
    const tail = "b".repeat(40) + ", " + "c".repeat(40) + ".";
    expect(passes(`${head} ${tail} dˈʌn.`)).toEqual([head, tail, "dˈʌn."]);
  });

  it("keeps every pass within the model's limit, preferring clause marks, then spaces", () => {
    const clause = `${"ab ".repeat(100).trim()}, `;
    const text = `${clause.repeat(6).trim()}.`;
    const out = passes(`ðə. ${text}`);
    expect(out[0]).toBe("ðə.");
    for (const p of out) expect(p.length).toBeLessThanOrEqual(MAX_PHONEMES);
    expect(out.slice(1, -1).every((p) => p.endsWith(","))).toBe(true);
    expect(out.join(" ").replace(/ +/g, " ")).toBe(`ðə. ${text}`.replace(/ +/g, " "));
    const spaces = passes(`ðə. ${"abc ".repeat(400).trim()}`);
    for (const p of spaces) expect(p.length).toBeLessThanOrEqual(MAX_PHONEMES);
    const solid = passes(`ðə. ${"x".repeat(1200)}`);
    expect(solid.slice(1).map((p) => p.length)).toEqual([MAX_PHONEMES, MAX_PHONEMES, 180]);
  });

  it("drops passes with nothing to say", () => {
    expect(passes("… . !")).toEqual([]);
    expect(passes("")).toEqual([]);
  });
});
