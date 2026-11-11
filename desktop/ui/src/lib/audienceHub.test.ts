import { describe, expect, it } from "vitest";
import { audienceStatusTone, formatMoney, initials, intentTone, parsePrices, studyIsActive, studyStatusTone, verdictTone } from "@/lib/audienceHub";

describe("audienceHub helpers", () => {
  it("reads prices from free text, drops what is not a price and sorts them", () => {
    expect(parsePrices("19, 9; 49\n9")).toEqual([9, 19, 49]);
  });

  it("keeps decimals and ignores junk and negatives", () => {
    expect(parsePrices("4.5 ; abc ; -3 ; 12")).toEqual([4.5, 12]);
    expect(parsePrices("")).toEqual([]);
  });

  it("maps intent scores and verdicts to tones", () => {
    expect(intentTone(9)).toBe("success");
    expect(intentTone(5)).toBe("warning");
    expect(intentTone(1)).toBe("destructive");
    expect(verdictTone("strong")).toBe("success");
    expect(verdictTone("poor")).toBe("destructive");
  });

  it("knows which statuses are still moving", () => {
    expect(studyIsActive("running")).toBe(true);
    expect(studyIsActive("synthesizing")).toBe(true);
    expect(studyIsActive("done")).toBe(false);
    expect(studyStatusTone("done")).toBe("success");
    expect(audienceStatusTone("failed")).toBe("destructive");
  });

  it("formats money in the app language and survives an unknown currency", () => {
    expect(formatMoney(19, "USD", "en")).toBe("$19");
    expect(formatMoney(12.5, "USD", "en")).toBe("$12.50");
    expect(formatMoney(5, "NOPE1", "en")).toBe("5 NOPE1");
  });

  it("makes initials from a name", () => {
    expect(initials("Ayse Demir")).toBe("AD");
    expect(initials("Cher")).toBe("C");
    expect(initials("  ")).toBe("");
    expect(initials("Şule İpek")).toBe("Şİ");
    expect(Array.from(initials("ışık güneş"))).toHaveLength(2);
    expect(initials("𝒜da Lovelace")).toBe("𝒜L");
  });
});
