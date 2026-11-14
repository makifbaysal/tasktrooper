import type { DesktopVoiceLanguage } from "@/lib/desktop-bridge";

/**
 * The OS voice that reads a language with no bundled voice.
 *
 * Left to itself, Chromium on macOS takes the first installed voice for the
 * language, which is alphabetically "Albert" — a raspy novelty voice. So the
 * voice is chosen: a downloaded Premium/Enhanced voice first, then the
 * natural standard ones, never the novelty and Eloquence voices.
 */

const NOVELTY = new Set([
  "albert",
  "bad news",
  "bahh",
  "bells",
  "boing",
  "bubbles",
  "cellos",
  "wobble",
  "good news",
  "jester",
  "junior",
  "organ",
  "superstar",
  "trinoids",
  "whisper",
  "zarvox",
  "ralph",
  "fred",
  "kathy",
  // Eloquence voices: robotic next to the standard ones.
  "eddy",
  "flo",
  "grandma",
  "grandpa",
  "reed",
  "rocko",
  "sandy",
  "shelley",
]);

const PREFERRED: Record<DesktopVoiceLanguage, string[]> = {
  en: [
    "ava",
    "zoe",
    "allison",
    "evan",
    "nathan",
    "samantha",
    "tom",
    "susan",
    "daniel",
    "serena",
    "karen",
    "moira",
    "tessa",
  ],
  tr: ["yelda", "cem"],
};

const BCP47: Record<DesktopVoiceLanguage, string> = {
  en: "en-US",
  tr: "tr-TR",
};

function baseName(voice: SpeechSynthesisVoice): string {
  return voice.name
    .replace(/\s*\(.*\)\s*$/, "")
    .trim()
    .toLowerCase();
}

function score(voice: SpeechSynthesisVoice, lang: DesktopVoiceLanguage): number {
  const name = voice.name.toLowerCase();
  let s = 0;
  if (/premium/.test(name)) s += 400;
  else if (/enhanced/.test(name)) s += 300;
  const rank = PREFERRED[lang].indexOf(baseName(voice));
  if (rank >= 0) s += 200 - rank;
  if (voice.lang.replace("_", "-") === BCP47[lang]) s += 20;
  if (voice.localService) s += 5;
  return s;
}

export function pickOsVoice(voices: SpeechSynthesisVoice[], lang: DesktopVoiceLanguage): SpeechSynthesisVoice | null {
  const candidates = voices.filter((v) => v.lang.toLowerCase().startsWith(lang) && !NOVELTY.has(baseName(v)));
  if (candidates.length === 0) return null;
  return candidates.reduce((best, v) => (score(v, lang) > score(best, lang) ? v : best));
}

export function osVoiceLang(lang: DesktopVoiceLanguage): string {
  return BCP47[lang];
}
