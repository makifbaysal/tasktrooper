import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { gunzipSync } from "node:zlib";
import { describe, expect, it } from "vitest";
import { applyStress, cardinalWords, EnglishG2P, letterToSound, ordinalWords, split, yearWords, type LexiconData } from "./english.js";

/**
 * `testdata/english-lexicon.json` is a slice of misaki's us_gold/us_silver
 * (Apache-2.0) holding the words these cases need, so the G2P is tested on
 * misaki's own entries without the 1.4 MB dictionary in the repository.
 */
const lexicon = JSON.parse(readFileSync(path.join(import.meta.dirname, "testdata/english-lexicon.json"), "utf8")) as LexiconData;
const g2p = new EnglishG2P(lexicon, false);
const vocab = new Set(Object.keys((JSON.parse(readFileSync(path.join(import.meta.dirname, "testdata/kokoro.json"), "utf8")) as { vocab: object }).vocab));

describe("EnglishG2P with misaki's dictionary", () => {
  it.each([
    ["Hello, world!", "həlˈO, wˈɜɹld!"],
    ["The app failed.", "ði ˈæp fˈAld."],
    ["The build failed.", "ðə bˈɪld fˈAld."],
    ["I want to use it.", "ˌI wˈɑnt tə jˈuz ɪt."],
    ["Would you like me to check?", "wˌʊd ju lˈIk mˌi tə ʧˈɛk?"],
  ])("reads common words: %j", (text, ps) => {
    expect(g2p.phonemize(text)).toBe(ps);
  });

  it("reads contractions as whole words", () => {
    expect(g2p.phonemize("Don't worry, it's fine; I'll look.")).toMatch(/^dˈOnt .*, ɪts .*; ˌIl lˈʊk\.$/);
    expect(g2p.phonemize("I couldn't.")).toBe("ˌI kˈʊdᵊnt.");
  });

  it("stems plurals, past tenses and gerunds it has no entry for", () => {
    expect(g2p.phonemize("deployed")).toBe("dəplˈYd");
    expect(g2p.phonemize("users")).toBe("jˈuzəɹz");
    expect(g2p.phonemize("deploying")).toBe("dəplˈYɪŋ");
  });

  it("keeps a capitalized word's reading and spells an all-capitals one it does not know", () => {
    expect(g2p.phonemize("HELLO")).toBe(g2p.phonemize("hello"));
    expect(g2p.phonemize("API")).toBe("ˌApˌiˈI");
    expect(g2p.phonemize("XKCD")).toBe("ˌɛkskˌAsˌidˈi");
  });

  it.each([
    ["3 tests", "θɹˈi tˈɛsts"],
    ["1,234", "wˈʌn θˈWzᵊnd tˈu hˈʌndɹəd θˈɜɹTi fˈɔɹ"],
    ["In 2024, we shipped", "ɪn twˈɛnti twˈɛnti fˈɔɹ, wi ʃˈɪpt"],
    ["1999", "nˌIntˈin nˈIndi nˈIn"],
    ["the 3rd", "ðə θˈɜɹd"],
    ["$4.99", "fˈɔɹ dˈɑləɹz ænd nˈIndi nˈIn sˈɛnts"],
    ["$1", "wˈʌn dˈɑləɹ"],
    ["75%", "sˈɛvənti fˈIv pəɹsˈɛnt"],
    ["2.5", "tˈu pYnt fˈIv"],
    ["-4", "mˈInəs fˈɔɹ"],
    ["at 10:30", "æt tˈɛn θˈɜɹTi"],
    ["at 3:00", "æt θɹˈi əklˈɑk"],
    ["2-3", "tˈu tə θɹˈi"],
  ])("reads numbers: %j", (text, ps) => {
    expect(g2p.phonemize(text)).toBe(ps);
  });

  it("reads compounds, camelCase and file names word by word", () => {
    expect(g2p.phonemize("webhook")).toBe("wˈɛbhˌʊk");
    expect(g2p.phonemize("useState")).toBe("jˈuzstˌAt");
    expect(g2p.phonemize("TaskTrooper")).toBe("tˈæsktɹˌupəɹ");
    expect(g2p.phonemize("index.ts")).toBe("ˈɪndˌɛks dˈɑt tˌiˈɛs");
  });

  it("spells a short word with no vowel letter and sounds out anything else unknown", () => {
    expect(g2p.phonemize("npm")).toBe("ˌɛnpˌiˈɛm");
    const unknown = g2p.phonemize("blorfing");
    expect(unknown).toMatch(/^bl.*ˈ/);
    expect(unknown.length).toBeGreaterThan(4);
  });

  it("reads the software words the dictionaries lack", () => {
    expect(g2p.phonemize("JSON config")).toBe("ʤˈAsᵊn kˈɑnfɪɡ");
  });

  it("writes only symbols Kokoro has, and nothing for what it cannot read", () => {
    const ps = g2p.phonemize("Café — “quoted” (aside) 🚀 日本 x² ½ tab\tnew\nline… done?!");
    for (const ch of ps) expect(vocab.has(ch)).toBe(true);
    expect(g2p.phonemize("🚀 日本")).toBe("");
    expect(g2p.phonemize("")).toBe("");
  });

  it("writes the US flap as T, the way Kokoro v1.0 was trained", () => {
    expect(g2p.phonemize("thirty")).toBe("θˈɜɹTi");
  });
});

describe("the pieces", () => {
  it("applyStress follows misaki", () => {
    expect(applyStress("həlˈO", -1)).toBe("həlˌO");
    expect(applyStress("həlˈO", -2)).toBe("həlO");
    expect(applyStress("ɪt", 2)).toBe("ˈɪt");
    expect(applyStress("ɪt", 0.5)).toBe("ˌɪt");
    expect(applyStress("ɪt", null)).toBe("ɪt");
  });

  it("spells numbers as num2words does, without its ands", () => {
    expect(cardinalWords(0n)).toBe("zero");
    expect(cardinalWords(115n)).toBe("one hundred fifteen");
    expect(cardinalWords(2_000_021n)).toBe("two million twenty-one");
    expect(ordinalWords(21n)).toBe("twenty-first");
    expect(ordinalWords(12n)).toBe("twelfth");
    expect(ordinalWords(40n)).toBe("fortieth");
    expect(yearWords(1905)).toBe("nineteen oh-five");
    expect(yearWords(1900)).toBe("nineteen hundred");
    expect(yearWords(2005)).toBe("two thousand five");
    expect(yearWords(2024)).toBe("twenty twenty-four");
  });

  it("splits tokens at symbols, digits and camelCase humps", () => {
    expect(split("useState").map((p) => [p.text, p.glued])).toEqual([
      ["use", false],
      ["State", true],
    ]);
    expect(split("v0.3.5").map((p) => p.text)).toEqual(["v", "0.3.5"]);
    expect(split("HTTPServer").map((p) => p.text)).toEqual(["HTTP", "Server"]);
    expect(split("a@b.com").map((p) => p.text)).toEqual(["a", "@", "b", ".", "com"]);
  });

  it("sounds out words by rule with one primary stress", () => {
    for (const word of ["blorf", "zanterix", "kokoro", "quixotical"]) {
      const ps = letterToSound(word);
      expect(ps.split("ˈ")).toHaveLength(2);
      for (const ch of ps) expect(vocab.has(ch)).toBe(true);
    }
    expect(letterToSound("make")).toBe("mˈAk");
    expect(letterToSound("ship")).toBe("ʃˈɪp");
  });
});

/**
 * The full dictionaries, when a build has fetched them into `.cache/voice/`:
 * misaki's own output (with spaCy, en_core_web_sm) for sentences where tags
 * do not change the reading.
 */
const fullLexicon = path.resolve(import.meta.dirname, "../../../.cache/voice/misaki_us.json.gz");
describe.skipIf(!existsSync(fullLexicon))("EnglishG2P against misaki", () => {
  const full = new EnglishG2P(JSON.parse(gunzipSync(readFileSync(fullLexicon)).toString("utf8")) as LexiconData, false);
  const cases = JSON.parse(readFileSync(path.join(import.meta.dirname, "testdata/english.json"), "utf8")) as { text: string; ps: string }[];
  it.each(cases.map((c) => [c.text, c.ps]))("reads %j as misaki does", (text, ps) => {
    expect(full.phonemize(text)).toBe(ps);
  });
});
