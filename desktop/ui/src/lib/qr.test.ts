import { describe, expect, it } from "vitest";
import { dataCodewords, encodeQr } from "./qr";

describe("qr", () => {
  it("has the data capacities of the QR specification (versions 1-10, levels M and L)", () => {
    expect([1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map((v) => dataCodewords(v, "M"))).toEqual([16, 28, 44, 64, 86, 108, 124, 154, 182, 216]);
    expect([1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map((v) => dataCodewords(v, "L"))).toEqual([19, 34, 55, 80, 108, 136, 156, 194, 232, 274]);
  });

  it("picks the smallest version and draws the three finder patterns", () => {
    const m = encodeQr("HELLO");
    expect(m.length).toBe(21);
    const finder = (ox: number, oy: number) => {
      for (let y = 0; y < 7; y += 1) {
        for (let x = 0; x < 7; x += 1) {
          const ring = Math.max(Math.abs(x - 3), Math.abs(y - 3));
          expect(m[oy + y]?.[ox + x], `${ox},${oy} ${x},${y}`).toBe(ring !== 2);
        }
      }
    };
    finder(0, 0);
    finder(14, 0);
    finder(0, 14);
    expect(m[13]?.[8]).toBe(true);
  });

  it("fits a pairing link and falls back from M to L when needed", () => {
    const link = `tasktrooper://pair?v=1&u=${encodeURIComponent("https://calm-fox-12.trycloudflare.com")}&c=K7QD2M9WXH4T&n=${encodeURIComponent("Akif's MacBook")}`;
    expect(link.length).toBeLessThan(160);
    const m = encodeQr(link);
    expect(m.length).toBe(m[0]?.length);
    expect(m.length).toBeLessThanOrEqual(57);
    const long = "x".repeat(250);
    expect(encodeQr(long).length).toBeGreaterThan(21);
    expect(() => encodeQr("x".repeat(400))).toThrow(/too long/);
  });
});
