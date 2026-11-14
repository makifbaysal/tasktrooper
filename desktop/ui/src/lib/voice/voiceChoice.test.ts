import { describe, expect, it } from "vitest";
import type { DesktopVoiceCapabilities } from "@/lib/desktop-bridge";
import { effectiveVoice } from "./voiceChoice";

const caps = {
  listen: { tr: true, en: true },
  speak: { tr: true, en: true },
  speakEngines: {
    tr: [
      { engine: "ema", styles: [] },
      { engine: "supertonic", styles: ["F1", "M2"] },
    ],
    en: [{ engine: "kokoro", styles: ["af_heart", "bm_george"] }],
  },
} as DesktopVoiceCapabilities;

describe("effectiveVoice", () => {
  it("defaults to the first model and its first voice", () => {
    expect(effectiveVoice(caps, "tr", undefined).choice).toEqual({ engine: "ema" });
    expect(effectiveVoice(caps, "en", undefined).choice).toEqual({ engine: "kokoro", style: "af_heart" });
  });

  it("keeps a pick this computer has, and drops what it no longer has", () => {
    expect(effectiveVoice(caps, "tr", { engine: "supertonic", style: "M2" }).choice).toEqual({
      engine: "supertonic",
      style: "M2",
    });
    expect(effectiveVoice(caps, "tr", { engine: "supertonic", style: "M9" }).choice).toEqual({
      engine: "supertonic",
      style: "F1",
    });
    expect(effectiveVoice(caps, "en", { engine: "supertonic", style: "F1" }).choice).toEqual({
      engine: "kokoro",
      style: "af_heart",
    });
    expect(effectiveVoice(null, "en", { engine: "kokoro" }).choice).toBeUndefined();
  });
});
