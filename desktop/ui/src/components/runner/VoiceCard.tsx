import { Mic, Play, Square } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { voiceLanguageFor } from "@/hooks/useVoiceConversation";
import {
  desktopVoice,
  type DesktopVoiceCapabilities,
  type DesktopVoiceChoice,
  type DesktopVoiceEngine,
  type DesktopVoiceLanguage,
} from "@/lib/desktop-bridge";
import { readVoicePrefs, writeVoicePrefs, type VoicePrefs } from "@/lib/voice/prefs";
import { previewVoice } from "@/lib/voice/preview";
import { effectiveVoice } from "@/lib/voice/voiceChoice";

const SPEEDS = [0.75, 1, 1.25, 1.5];
const LANGUAGES: DesktopVoiceLanguage[] = ["tr", "en"];
const ENGINE_NAMES: Record<DesktopVoiceEngine, string> = {
  ema: "EMA Lightning",
  kokoro: "Kokoro",
  supertonic: "Supertonic 3",
};

type T = ReturnType<typeof useI18n>["t"];

function styleLabel(t: T, engine: DesktopVoiceEngine, style: string): string {
  const supertonic = /^([FM])(\d+)$/.exec(style);
  if (engine === "supertonic" && supertonic) {
    return t(supertonic[1] === "F" ? "settings.voice.female" : "settings.voice.male", { n: supertonic[2] });
  }
  // Kokoro ids: accent (a = American, b = British), gender (f/m), name.
  const kokoro = /^([ab])([fm])_(\w+)$/.exec(style);
  if (engine === "kokoro" && kokoro) {
    return t("settings.voice.kokoroStyle", {
      name: kokoro[3].charAt(0).toUpperCase() + kokoro[3].slice(1),
      accent: t(kokoro[1] === "a" ? "settings.voice.us" : "settings.voice.gb"),
      gender: t(kokoro[2] === "f" ? "settings.voice.woman" : "settings.voice.man"),
    });
  }
  return style;
}

/** Settings → Voice: talking to agents. Absent where the shell has no voice bridge. */
export function VoiceCard() {
  const { t, lang } = useI18n();
  const voice = desktopVoice();
  const [caps, setCaps] = useState<DesktopVoiceCapabilities | null>(null);
  const [prefs, setPrefs] = useState<VoicePrefs>(readVoicePrefs);
  const [playing, setPlaying] = useState<DesktopVoiceLanguage | null>(null);
  const stopPreview = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!voice) return;
    void voice.capabilities().then(setCaps, () => undefined);
  }, [voice]);
  useEffect(() => () => stopPreview.current?.(), []);

  if (!voice) return null;
  const voiceLang = voiceLanguageFor(lang);
  const available = Boolean(voiceLang && caps?.listen[voiceLang]);

  const update = (patch: Partial<VoicePrefs>) => {
    const next = { ...prefs, ...patch };
    setPrefs(next);
    writeVoicePrefs(next);
  };
  const pick = (l: DesktopVoiceLanguage, choice: DesktopVoiceChoice) =>
    update({ voices: { ...prefs.voices, [l]: choice } });

  const listen = (l: DesktopVoiceLanguage, choice: DesktopVoiceChoice | undefined) => {
    stopPreview.current?.();
    if (playing === l) {
      setPlaying(null);
      return;
    }
    const run = previewVoice(voice, l, choice, prefs.speed);
    stopPreview.current = run.stop;
    setPlaying(l);
    void run.done.then(() => {
      if (stopPreview.current === run.stop) {
        stopPreview.current = null;
        setPlaying(null);
      }
    });
  };

  return (
    <Card className="w-full space-y-4 p-6">
      <div className="space-y-1">
        <Label className="flex items-center gap-2">
          <Mic className="h-4 w-4" />
          {t("settings.voice.title")}
        </Label>
        <p className="text-sm text-muted-foreground">
          {available ? t("settings.voice.help") : t("settings.voice.unavailable")}
        </p>
      </div>
      {available && (
        <>
          <div className="flex items-center justify-between gap-4">
            <Label htmlFor="voice-speed">{t("settings.voice.speed")}</Label>
            <Select value={String(prefs.speed)} onValueChange={(v) => update({ speed: Number(v) })}>
              <SelectTrigger id="voice-speed" className="w-28">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SPEEDS.map((s) => (
                  <SelectItem key={s} value={String(s)}>
                    {`${s}×`}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-center justify-between gap-4">
            <div className="space-y-1">
              <Label htmlFor="voice-barge-in">{t("settings.voice.bargeIn")}</Label>
              <p className="text-sm text-muted-foreground">{t("settings.voice.bargeInHelp")}</p>
            </div>
            <Switch id="voice-barge-in" checked={prefs.bargeIn} onCheckedChange={(on) => update({ bargeIn: on })} />
          </div>
        </>
      )}
      {LANGUAGES.map((l) => {
        const options = caps?.speakEngines?.[l] ?? [];
        if (options.length === 0) return null;
        const { option, choice } = effectiveVoice(caps, l, prefs.voices[l]);
        if (!option || !choice?.engine) return null;
        const engine = choice.engine;
        return (
          <div key={l} className="space-y-2 border-t border-border pt-4">
            <Label>{t(l === "tr" ? "settings.voice.languageTr" : "settings.voice.languageEn")}</Label>
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={engine}
                onValueChange={(v) => pick(l, { engine: v as DesktopVoiceEngine })}
              >
                <SelectTrigger className="w-44" aria-label={t("settings.voice.model")}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {options.map((o) => (
                    <SelectItem key={o.engine} value={o.engine}>
                      {ENGINE_NAMES[o.engine] ?? o.engine}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {option.styles.length > 0 && (
                <Select value={choice.style} onValueChange={(v) => pick(l, { engine, style: v })}>
                  <SelectTrigger className="w-56" aria-label={t("settings.voice.style")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {option.styles.map((s) => (
                      <SelectItem key={s} value={s}>
                        {styleLabel(t, engine, s)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              <Button type="button" variant="outline" size="sm" onClick={() => listen(l, choice)}>
                {playing === l ? <Square className="mr-1.5 h-4 w-4" /> : <Play className="mr-1.5 h-4 w-4" />}
                {t(playing === l ? "settings.voice.previewStop" : "settings.voice.preview")}
              </Button>
            </div>
          </div>
        );
      })}
    </Card>
  );
}
