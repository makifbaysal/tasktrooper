import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { VoiceCard } from "@/components/runner/VoiceCard";
import { I18nProvider } from "@/hooks/useI18n";
import type { DesktopVoiceCapabilities } from "@/lib/desktop-bridge";

const CAPS: DesktopVoiceCapabilities = {
  listen: { tr: true, en: true },
  speak: { tr: true, en: true },
  speakEngines: {
    tr: [{ engine: "ema", styles: [] }],
    en: [{ engine: "tinyvoca", styles: ["F2", "M2"] }],
  },
};

function renderCard() {
  window.__tasktrooperDesktop = { voice: { capabilities: vi.fn().mockResolvedValue(CAPS) } } as never;
  return render(
    <I18nProvider>
      <VoiceCard />
    </I18nProvider>,
  );
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
  localStorage.clear();
});

describe("VoiceCard", () => {
  it("shows the app's language and, under it, that language's default voice", async () => {
    renderCard();
    expect(await screen.findByRole("combobox", { name: "Language" })).toHaveTextContent("English");
    expect(screen.getByRole("combobox", { name: "Voice" })).toHaveTextContent("tiny-voca — Female");
  });

  it("shows the stored voice for the language", async () => {
    localStorage.setItem("tasktrooper.voice.prefs", JSON.stringify({ voices: { en: { engine: "tinyvoca", style: "M2" } } }));
    renderCard();
    expect(await screen.findByRole("combobox", { name: "Voice" })).toHaveTextContent("tiny-voca — Male");
  });

  it("falls back to the default for a voice from a removed model", async () => {
    localStorage.setItem("tasktrooper.voice.prefs", JSON.stringify({ voices: { en: { engine: "kokoro", style: "af_heart" } } }));
    renderCard();
    expect(await screen.findByRole("combobox", { name: "Voice" })).toHaveTextContent("tiny-voca — Female");
  });
});
