import type { DesktopVoiceAudioEvent, DesktopVoiceChoice, DesktopVoiceHost, DesktopVoiceLanguage } from "@/lib/desktop-bridge";
import { PcmPlayer } from "./audio";

const SAMPLE: Record<DesktopVoiceLanguage, string> = {
  tr: "Merhaba, ben ürün yöneticinim. Bugün ne üzerinde çalışalım?",
  en: "Hi, I'm your product manager. What shall we work on today?",
};

/**
 * Reads a sample sentence in `voice`, for Settings. Resolves when it has
 * finished playing; the returned stop() cuts it short.
 */
export function previewVoice(
  host: DesktopVoiceHost,
  lang: DesktopVoiceLanguage,
  voice: DesktopVoiceChoice | undefined,
  speed: number,
): { done: Promise<void>; stop: () => void } {
  const player = new PcmPlayer();
  let id: string | null = null;
  // Audio can arrive before speak() answers with its id.
  const early: DesktopVoiceAudioEvent[] = [];
  let finish: () => void = () => undefined;
  let timer: ReturnType<typeof setTimeout> | null = null;
  const done = new Promise<void>((resolve) => (finish = resolve));

  const end = () => {
    off();
    if (timer) clearTimeout(timer);
    player.close();
    finish();
  };
  const onEvent = (e: DesktopVoiceAudioEvent) => {
    if (e.pcm) player.play(e.pcm, e.sampleRate);
    if (e.error) end();
    else if (e.done) timer = setTimeout(end, player.remaining() * 1000 + 100);
  };
  const off = host.onAudio((e) => {
    if (id === null) early.push(e);
    else if (e.id === id) onEvent(e);
  });

  host.speak(lang, SAMPLE[lang], speed, voice).then(
    (r) => {
      id = r.id;
      for (const e of early.splice(0)) if (e.id === id) onEvent(e);
    },
    () => end(),
  );

  return {
    done,
    stop: () => {
      if (id) void host.speakCancel(id);
      end();
    },
  };
}
