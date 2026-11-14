import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { BLOCK_BYTES, blocks, Frontend, type TextNormalizer } from "./frontend.js";
import { WasmNormalizer } from "./normalizer.js";

/**
 * Expected outputs are `frontend.py`'s own, made by importing it from the
 * EMA Lightning package and running these inputs through it. The alphabet
 * step needs nothing else; the full frontend needs the real normalizer, which
 * is there once `npm run build:embedder` has fetched the voice (or
 * TASKTROOPER_VOICE_MODELS_DIR points at it), and is skipped otherwise.
 */

interface Case {
  input: string;
  output: string;
}
const read = <T>(name: string): T => JSON.parse(readFileSync(path.join(import.meta.dirname, "testdata", name), "utf8")) as T;
const VOCAB = read<{ vocab: string[] }>("ema.json").vocab;
const fixtures = read<{ frontend: Case[]; alphabet: Case[] }>("frontend.json");

function findNormalizer(): string | null {
  const desktop = path.resolve(import.meta.dirname, "../../..");
  const candidates = [
    process.env.TASKTROOPER_VOICE_MODELS_DIR,
    path.join(desktop, ".cache", "voice"),
    path.join(desktop, "dist", "embedder", "voice"),
  ];
  for (const dir of candidates) {
    if (dir && existsSync(path.join(dir, "normalizer.wasm"))) return path.join(dir, "normalizer.wasm");
  }
  return null;
}

describe("Frontend.alphabet", () => {
  const frontend = new Frontend(VOCAB, null);
  it.each(fixtures.alphabet)("matches frontend.py for %j", ({ input, output }) => {
    expect(frontend.alphabet(input)).toBe(output);
  });
});

describe("Frontend.run", () => {
  it("keeps a block the normalizer refuses or throws on", () => {
    const refusing: TextNormalizer = { normalize: () => null };
    const throwing: TextNormalizer = {
      normalize: () => {
        throw new Error("boom");
      },
    };
    expect(new Frontend(VOCAB, refusing).run("İstanbul'da IŞIK")).toBe("istanbul'da ışık");
    expect(new Frontend(VOCAB, throwing).run("Merhaba Dünya")).toBe("merhaba dünya");
  });

  it("says nothing for blank or control-only text", () => {
    const frontend = new Frontend(VOCAB, { normalize: (t) => t });
    expect(frontend.run("")).toBe("");
    expect(frontend.run(" \t\n")).toBe("");
    expect(frontend.run("\u0000\u0007")).toBe("");
  });

  it("hands the normalizer blocks of at most BLOCK_BYTES, split at whitespace", () => {
    const seen: string[] = [];
    const frontend = new Frontend(VOCAB, {
      normalize: (t) => {
        seen.push(t);
        return t;
      },
    });
    const word = "çğıöşü".repeat(10);
    const text = Array.from({ length: 400 }, () => word).join(" ");
    expect(frontend.run(text)).toBe(text);
    expect(seen.length).toBeGreaterThan(1);
    for (const block of seen) expect(new TextEncoder().encode(block).length).toBeLessThanOrEqual(BLOCK_BYTES);
    expect(seen.join(" ")).toBe(text);
  });

  it("splits on Python's whitespace, which does not include U+FEFF", () => {
    expect(blocks("a﻿b c d")).toEqual(["a﻿b c d"]);
  });

  const wasm = findNormalizer();
  describe.skipIf(!wasm)("with the real normalizer", () => {
    const frontend = new Frontend(VOCAB, wasm ? new WasmNormalizer(readFileSync(wasm)) : null);
    it.each(fixtures.frontend)("matches frontend.py for %j", ({ input, output }) => {
      expect(frontend.run(input)).toBe(output);
    });
  });
});
