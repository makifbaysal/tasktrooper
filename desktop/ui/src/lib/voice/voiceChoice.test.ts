import { describe, expect, it } from "vitest";
import type { DesktopVoiceCapabilities } from "@/lib/desktop-bridge";
import { effectiveVoice } from "./voiceChoice";

const caps = {
  listen: { tr: true, en: true },
  speak: { tr: true, en: true },
  speakEngines: {
    tr: [{ engine: "ema", styles: [] }],
    en: [{ engine: "tinyvoca", styles: ["F2", "M2"] }],
  },
} as DesktopVoiceCapabilities;

describe("effectiveVoice", () => {
  it("defaults to the first model and its first voice", () => {
    expect(effectiveVoice(caps, "tr", undefined).choice).toEqual({ engine: "ema" });
    expect(effectiveVoice(caps, "en", undefined).choice).toEqual({ engine: "tinyvoca", style: "F2" });
  });

  it("keeps a pick this computer has, and drops what it no longer has", () => {
    expect(effectiveVoice(caps, "en", { engine: "tinyvoca", style: "M2" }).choice).toEqual({ engine: "tinyvoca", style: "M2" });
    expect(effectiveVoice(caps, "en", { engine: "tinyvoca", style: "M9" }).choice).toEqual({ engine: "tinyvoca", style: "F2" });
    expect(effectiveVoice(caps, "tr", { engine: "tinyvoca", style: "F2" }).choice).toEqual({ engine: "ema" });
    expect(effectiveVoice(null, "en", { engine: "tinyvoca" }).choice).toBeUndefined();
  });
});
