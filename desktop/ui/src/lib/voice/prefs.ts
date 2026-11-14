/** Per-computer voice preferences; a convenience, so an unreadable store just means the defaults. */
export interface VoicePrefs {
  /** Reading speed, 0.75–1.5. */
  speed: number;
  /** Talking over the agent stops it and it listens. */
  bargeIn: boolean;
}

export const DEFAULT_VOICE_PREFS: VoicePrefs = { speed: 1, bargeIn: true };
const KEY = "tasktrooper.voice.prefs";

export function readVoicePrefs(): VoicePrefs {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Partial<VoicePrefs>;
    const speed = typeof raw.speed === "number" && raw.speed >= 0.75 && raw.speed <= 1.5 ? raw.speed : DEFAULT_VOICE_PREFS.speed;
    const bargeIn = typeof raw.bargeIn === "boolean" ? raw.bargeIn : DEFAULT_VOICE_PREFS.bargeIn;
    return { speed, bargeIn };
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
