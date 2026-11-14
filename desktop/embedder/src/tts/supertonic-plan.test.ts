import { describe, expect, it } from "vitest";
import {
  CLAUSE_PAUSE,
  englishSpoken,
  latentShape,
  MAX_PASS_CHARS,
  ordinalSuffix,
  passes,
  prepareText,
  SENTENCE_PAUSE,
  sentences,
  speechBounds,
  textIds,
  turkishOrdinals,
} from "./supertonic-plan.js";

describe("prepareText", () => {
  it("decomposes as NFKD, so Turkish letters reach the index as base and mark", () => {
    expect(prepareText("Değişiklik")).toBe("Değişiklik.");
    expect(prepareText("İyi")).toBe("İyi.");
    expect(prepareText("Bekle…")).toBe("Bekle...");
  });

  it("folds typography and drops emoji, decorations and control characters", () => {
    expect(prepareText("“Merhaba” — dedi 😀")).toBe('"Merhaba" - dedi.');
    expect(prepareText("a_b [c] x/y #1 ♥")).toBe("a b c x y 1.");
    expect(prepareText("mail me@x‮evil")).toBe("mail me at x evil.");
  });

  it("closes up space before punctuation and collapses repeated quotes", () => {
    expect(prepareText('Hello , world ! ""Hi""')).toBe('Hello, world! "Hi"');
  });

  it("adds a period only when the text has no end of its own", () => {
    expect(prepareText("Done?")).toBe("Done?");
    expect(prepareText("Wait,")).toBe("Wait,");
    expect(prepareText("  ")).toBe("");
  });
});

describe("textIds", () => {
  const indexer = new Int32Array(0x400).fill(-1);
  [..."<>/trnehlo. "].forEach((ch, i) => {
    indexer[ch.codePointAt(0) ?? 0] = i + 1;
  });

  it("wraps the text in its language tag and maps each code point", () => {
    const ids = Array.from(textIds("hello.", "en", indexer) ?? [], Number);
    const expected = [..."<en>hello.</en>"].map((ch) => indexer[ch.codePointAt(0) ?? 0]);
    expect(ids).toEqual(expected);
  });

  it("leaves out what the index lacks, and is null when nothing sayable is left", () => {
    expect(Array.from(textIds("h🚀e", "en", indexer) ?? [], Number)).toEqual(Array.from(textIds("he", "en", indexer) ?? [], Number));
    expect(textIds("...", "tr", indexer)).toBeNull();
    expect(textIds("🚀", "tr", indexer)).toBeNull();
  });
});

describe("latentShape", () => {
  const config = { ae: { sample_rate: 44_100, base_chunk_size: 512 }, ttl: { latent_dim: 24, chunk_compress_factor: 6 } };

  it("counts samples and the 3072-sample latent frames that hold them", () => {
    expect(latentShape(1, config)).toEqual({ samples: 44_100, frames: 15, channels: 144 });
    expect(latentShape(3072 / 44_100, config).frames).toBe(1);
    expect(latentShape(0, config)).toEqual({ samples: 1, frames: 1, channels: 144 });
  });
});

describe("sentences", () => {
  it("splits after sentence ends, not after an abbreviation or an initial", () => {
    expect(sentences("Dr. Smith is in. J. R. agreed! Fine?  Yes")).toEqual(["Dr. Smith is in.", "J. R. agreed!", "Fine?", "Yes"]);
    expect(sentences("Use e.g. this one. Done.")).toEqual(["Use e.g. this one.", "Done."]);
  });
});

describe("passes", () => {
  it("is one pass per sentence, a sentence pause after each but the last", () => {
    expect(passes("Merhaba. Nasılsın? İyiyim!")).toEqual([
      { text: "Merhaba.", pause: SENTENCE_PAUSE },
      { text: "Nasılsın?", pause: SENTENCE_PAUSE },
      { text: "İyiyim!", pause: 0 },
    ]);
  });

  it("cuts the first sentence at its first clause mark past twenty characters", () => {
    expect(passes("Derleme dört dakikada bitti, ama ödeme modülünde iki test başarısız oldu. Sonra, tamam.")).toEqual([
      { text: "Derleme dört dakikada bitti,", pause: CLAUSE_PAUSE },
      { text: "ama ödeme modülünde iki test başarısız oldu.", pause: SENTENCE_PAUSE },
      { text: "Sonra, tamam.", pause: 0 },
    ]);
    expect(passes("Sure, I will open the pull request now.")).toEqual([{ text: "Sure, I will open the pull request now.", pause: 0 }]);
    expect(passes("The build finished in four minutes, okay.")).toEqual([{ text: "The build finished in four minutes, okay.", pause: 0 }]);
  });

  it("cuts a sentence longer than the limit at a clause mark, then a space", () => {
    const words = Array.from({ length: 80 }, (_, i) => `word${i}`).join(" ");
    const out = passes(`${words}.`);
    expect(out.length).toBeGreaterThan(1);
    expect(out.every((p) => p.text.length <= MAX_PASS_CHARS)).toBe(true);
    expect(out.map((p) => p.text).join(" ")).toBe(`${words}.`);
  });

  it("leaves out pieces with nothing to say", () => {
    expect(passes("... !!! Hello.")).toEqual([{ text: "Hello.", pause: 0 }]);
    expect(passes("  ")).toEqual([]);
  });
});

describe("Turkish ordinals", () => {
  it.each([
    [1, "inci"],
    [2, "nci"],
    [3, "üncü"],
    [4, "üncü"],
    [5, "inci"],
    [6, "ncı"],
    [7, "nci"],
    [8, "inci"],
    [9, "uncu"],
    [10, "uncu"],
    [20, "nci"],
    [40, "ıncı"],
    [60, "ıncı"],
    [100, "üncü"],
    [1000, "inci"],
    [2_000_000, "uncu"],
    [23, "üncü"],
    [350, "nci"],
  ])("%i takes %s", (n, suffix) => {
    expect(ordinalSuffix(n)).toBe(suffix);
  });

  it("writes a number, a period and a lowercase word as an ordinal", () => {
    expect(turkishOrdinals("Toplantı 3. katta, 10. maddede.")).toBe("Toplantı 3’üncü katta, 10’uncu maddede.");
  });

  it("leaves a sentence end, a decimal or a date alone", () => {
    expect(turkishOrdinals("Sürüm 3. Bu iyi.")).toBe("Sürüm 3. Bu iyi.");
    expect(turkishOrdinals("v1.2. sürüm")).toBe("v1.2. sürüm");
    expect(turkishOrdinals("15.03. tarihinde")).toBe("15.03. tarihinde");
  });
});

describe("englishSpoken", () => {
  it.each([
    ["It cost $1,250.", "It cost one thousand two hundred fifty dollars."],
    ["Pay $1 now", "Pay one dollar now"],
    ["€3.50 each", "three euros and fifty cents each"],
    ["£0.01", "one penny"],
    ["$5.00 flat", "five dollars flat"],
    ["$1.5 million raised", "1.5 million dollars raised"],
    ["no money 1,250 here", "no money 1,250 here"],
    ["CPU usage hit 25%.", "CPU usage hit 25 percent."],
    ["up 3.5 % today", "up 3.5 percent today"],
  ])("%s", (input, spoken) => {
    expect(englishSpoken(input)).toBe(spoken);
  });
});

describe("speechBounds", () => {
  const rate = 1000;

  it("trims the silence either side of the speech, keeping a little", () => {
    const pcm = new Float32Array(2000);
    for (let i = 500; i < 1500; i++) pcm[i] = Math.sin(i) * 0.5;
    const [start, end] = speechBounds(pcm, rate);
    expect(start).toBe(470);
    expect(end).toBe(1560);
  });

  it("is empty for a pass with no sound", () => {
    expect(speechBounds(new Float32Array(1000), rate)).toEqual([0, 0]);
  });
});
