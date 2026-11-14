import { describe, expect, it } from "vitest";
import { pickOsVoice } from "./osVoice";

const v = (name: string, lang = "en-US", localService = true) => ({ name, lang, localService }) as SpeechSynthesisVoice;

describe("pickOsVoice", () => {
  it("never takes a novelty or Eloquence voice, even when it comes first", () => {
    expect(pickOsVoice([v("Albert"), v("Eddy (English (US))"), v("Samantha")], "en")?.name).toBe("Samantha");
  });

  it("prefers a downloaded Premium voice", () => {
    expect(pickOsVoice([v("Samantha"), v("Zoe (Premium)")], "en")?.name).toBe("Zoe (Premium)");
  });

  it("matches the language and has nothing when only novelty voices exist", () => {
    expect(pickOsVoice([v("Yelda", "tr-TR"), v("Samantha")], "tr")?.name).toBe("Yelda");
    expect(pickOsVoice([v("Zarvox"), v("Whisper")], "en")).toBeNull();
  });
});
