import { useCallback, useEffect, useRef, useState } from "react";
import { desktopVoice, type DesktopVoiceCapabilities, type DesktopVoiceLanguage } from "@/lib/desktop-bridge";
import type { Lang } from "@/lib/languages";
import { VoiceConversation, type VoiceSnapshot } from "@/lib/voice/conversation";
import { readVoicePrefs } from "@/lib/voice/prefs";

const OFF: VoiceSnapshot = { state: "off", transcript: "", level: 0, error: null };

/** The voice language for the app language, or null when it has no voice (only Turkish and English do). */
export function voiceLanguageFor(lang: Lang): DesktopVoiceLanguage | null {
  return lang === "tr" || lang === "en" ? lang : null;
}

export interface VoiceControls {
  /** This app, computer and language can hold a voice conversation. */
  available: boolean;
  lang: DesktopVoiceLanguage | null;
  snapshot: VoiceSnapshot;
  active: boolean;
  start(): void;
  stop(): void;
  replyProgress(markdown: string): void;
  replyDone(markdown: string | null): void;
}

export function useVoiceConversation(
  appLang: Lang,
  handlers: { onUtterance(text: string, lang: DesktopVoiceLanguage): void; onInterrupt(): void },
): VoiceControls {
  const host = desktopVoice();
  const lang = voiceLanguageFor(appLang);
  const [caps, setCaps] = useState<DesktopVoiceCapabilities | null>(null);
  const [snapshot, setSnapshot] = useState<VoiceSnapshot>(OFF);
  const convRef = useRef<VoiceConversation | null>(null);
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;

  useEffect(() => {
    if (!host) return;
    let live = true;
    host.capabilities().then(
      (c) => live && setCaps(c),
      () => live && setCaps(null),
    );
    return () => {
      live = false;
    };
  }, [host]);

  const stop = useCallback(() => {
    convRef.current?.stop();
    convRef.current = null;
  }, []);

  useEffect(() => stop, [stop]);
  // A language switch mid-conversation would keep listening in the old one.
  useEffect(() => stop(), [lang, stop]);

  const available = Boolean(host && lang && caps?.listen[lang]);

  const start = useCallback(() => {
    if (!host || !lang || !caps?.listen[lang] || convRef.current) return;
    const prefs = readVoicePrefs();
    const conv = new VoiceConversation({
      host,
      lang,
      bundledVoice: caps.speak[lang],
      speed: prefs.speed,
      bargeIn: prefs.bargeIn,
      onChange: (s) => {
        setSnapshot(s);
        if (s.state === "off" && convRef.current === conv) convRef.current = null;
      },
      onUtterance: (text) => handlersRef.current.onUtterance(text, lang),
      onInterrupt: () => handlersRef.current.onInterrupt(),
    });
    convRef.current = conv;
    void conv.start();
  }, [host, lang, caps]);

  const replyProgress = useCallback((markdown: string) => convRef.current?.replyProgress(markdown), []);
  const replyDone = useCallback((markdown: string | null) => convRef.current?.replyDone(markdown), []);

  return {
    available,
    lang,
    snapshot,
    active: snapshot.state !== "off",
    start,
    stop,
    replyProgress,
    replyDone,
  };
}
