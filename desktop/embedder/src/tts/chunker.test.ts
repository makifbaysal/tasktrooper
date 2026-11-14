import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { chunk } from "./chunker.js";

interface Case {
  input: string;
  speed: number;
  pieces: [string, number][];
}
const fixtures = JSON.parse(readFileSync(path.join(import.meta.dirname, "testdata/chunker.json"), "utf8")) as Case[];

/** Expected pieces and pauses are `chunker.py`'s own output for the same input and speed. */
describe("chunk", () => {
  it.each(fixtures.map((f) => [f.input.slice(0, 40), f.speed, f] as const))("matches chunker.py for %j at speed %d", (_label, _speed, f) => {
    expect(chunk(f.input, f.speed).map((p) => [p.text, p.pause])).toEqual(f.pieces);
  });

  it("keeps every piece within the limit, ending on a sentence end", () => {
    const text = "bir iki üç dört beş altı yedi sekiz dokuz on ".repeat(30);
    for (const piece of chunk(text, 1)) {
      expect(piece.text.length).toBeLessThanOrEqual(181);
      expect(piece.text).toMatch(/[.!?]$/);
    }
  });
});
