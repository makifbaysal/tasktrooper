import type { DesktopVoiceChoice, DesktopVoiceEngine, DesktopVoiceLanguage } from "@/lib/desktop-bridge";

const ENGINES: readonly DesktopVoiceEngine[] = ["ema", "tinyvoca"];

/** Per-computer voice preferences; a convenience, so an unreadable store just means the defaults. */
export interface VoicePrefs {
  /** The model and voice picked per language; absent = the app's default for it. */
  voices: Partial<Record<DesktopVoiceLanguage, DesktopVoiceChoice>>;
  /** Reading speed, 0.75–1.5. */
  speed: number;
  /** Talking over the agent stops it and it listens. */
  bargeIn: boolean;
}

export const DEFAULT_VOICE_PREFS: VoicePrefs = { speed: 1, bargeIn: true, voices: {} };

function readChoice(raw: unknown): DesktopVoiceChoice | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const { engine, style } = raw as Record<string, unknown>;
  const choice: DesktopVoiceChoice = {};
  if (typeof engine === "string" && (ENGINES as readonly string[]).includes(engine)) {
    choice.engine = engine as DesktopVoiceEngine;
  }
  if (typeof style === "string" && /^[A-Za-z0-9_]{1,32}$/.test(style)) choice.style = style;
  return choice.engine || choice.style ? choice : undefined;
}
const KEY = "tasktrooper.voice.prefs";

export function readVoicePrefs(): VoicePrefs {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Partial<VoicePrefs>;
    const speed = typeof raw.speed === "number" && raw.speed >= 0.75 && raw.speed <= 1.5 ? raw.speed : DEFAULT_VOICE_PREFS.speed;
    const bargeIn = typeof raw.bargeIn === "boolean" ? raw.bargeIn : DEFAULT_VOICE_PREFS.bargeIn;
    const voicesRaw = (raw.voices ?? {}) as Record<string, unknown>;
    const voices: VoicePrefs["voices"] = {};
    for (const l of ["tr", "en"] as const) {
      const choice = readChoice(voicesRaw[l]);
      if (choice) voices[l] = choice;
    }
    return { speed, bargeIn, voices };
  } catch {
    return DEFAULT_VOICE_PREFS;
  }
}

export function writeVoicePrefs(prefs: VoicePrefs): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(prefs));
  } catch {
    // Private window or blocked storage: the choice lasts this session only.
  }
}
