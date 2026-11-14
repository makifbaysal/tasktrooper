import { describe, expect, it } from "vitest";
import { completeSentences, SentenceFeed, spokenText, withoutFences } from "./speechText";

describe("spokenText", () => {
  it("reads like a person: no markdown marks, brackets, links, urls or emoji", () => {
    const md = [
      "## Özet",
      "",
      "**Tamam**, hallettim (birkaç dosyada) 🎉.",
      "- İlk adım: [dokümana](https://example.com/doc) bak",
      "- `npm test` çalıştır",
      "Detay: https://example.com/x",
    ].join("\n");
    expect(spokenText(md)).toBe("Özet. Tamam, hallettim. İlk adım: dokümana bak. npm test çalıştır. Detay:");
  });

  it("never reads a fenced block, even one still open", () => {
    const md = "Ekranda çizdim.\n```visual\n<svg><text>A</text></svg>\n```\nBakabilirsin.\n```ts\nconst x = 1";
    expect(spokenText(md)).toBe("Ekranda çizdim. Bakabilirsin.");
    expect(withoutFences("a\n```\nb\n```\nc")).toBe("a\nc");
  });

  it("skips tables and rules", () => {
    expect(spokenText("Sonuç şu.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n---\nBitti.")).toBe("Sonuç şu. Bitti.");
  });
});

describe("completeSentences", () => {
  it("splits finished sentences and leaves the tail", () => {
    expect(completeSentences("Merhaba! Nasılsın? Bugün ")).toEqual({
      sentences: ["Merhaba!", "Nasılsın?"],
      consumed: "Merhaba! Nasılsın? ".length,
    });
  });

  it("does not end a sentence on an abbreviation or an ordinal", () => {
    expect(completeSentences("Dr. Ahmet 3. katta. Sonra ").sentences).toEqual(["Dr. Ahmet 3. katta."]);
  });
});

describe("SentenceFeed", () => {
  it("hands out each sentence once as the reply streams, then the rest", () => {
    const feed = new SentenceFeed();
    expect(feed.push("Merhaba. Bir şey")).toEqual([]);
    expect(feed.push("Merhaba. Bir şey çizdim.\n")).toEqual(["Merhaba.", "Bir şey çizdim."]);
    expect(feed.push("Merhaba. Bir şey çizdim.\n```visual\n<svg>")).toEqual([]);
    expect(feed.push("Merhaba. Bir şey çizdim.\n```visual\n<svg>\n```\nEkrana bak")).toEqual([]);
    expect(feed.finish("Merhaba. Bir şey çizdim.\n```visual\n<svg>\n```\nEkrana bak")).toEqual(["Ekrana bak."]);
    expect(feed.finish("Merhaba. Bir şey çizdim.\n```visual\n<svg>\n```\nEkrana bak")).toEqual([]);
  });
});
