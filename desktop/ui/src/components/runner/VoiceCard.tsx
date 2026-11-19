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
  tinyvoca: "tiny-voca",
};

type T = ReturnType<typeof useI18n>["t"];

function voiceLabel(t: T, choice: DesktopVoiceChoice): string {
  const engine = choice.engine ? (ENGINE_NAMES[choice.engine] ?? choice.engine) : "";
  if (!choice.style) return engine;
  const gender = /^([FM])\d*$/.exec(choice.style);
  const name = gender ? t(gender[1] === "F" ? "settings.voice.female" : "settings.voice.male") : choice.style;
  return `${engine} — ${name}`;
}

const voiceKey = (choice: DesktopVoiceChoice) => `${choice.engine ?? ""}:${choice.style ?? ""}`;

/** Settings → Voice: talking to agents. Absent where the shell has no voice bridge. */
export function VoiceCard() {
  const { t, lang } = useI18n();
  const voice = desktopVoice();
  const [caps, setCaps] = useState<DesktopVoiceCapabilities | null>(null);
  const [prefs, setPrefs] = useState<VoicePrefs>(readVoicePrefs);
  const [playing, setPlaying] = useState<DesktopVoiceLanguage | null>(null);
  const [shown, setShown] = useState<DesktopVoiceLanguage>(() => voiceLanguageFor(lang) ?? "tr");
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
      {caps && (
        <VoicePicker
          caps={caps}
          prefs={prefs}
          shown={shown}
          playing={playing}
          onShow={setShown}
          onPick={pick}
          onListen={listen}
        />
      )}
    </Card>
  );
}

function VoicePicker({
  caps,
  prefs,
  shown,
  playing,
  onShow,
  onPick,
  onListen,
}: {
  caps: DesktopVoiceCapabilities;
  prefs: VoicePrefs;
  shown: DesktopVoiceLanguage;
  playing: DesktopVoiceLanguage | null;
  onShow: (l: DesktopVoiceLanguage) => void;
  onPick: (l: DesktopVoiceLanguage, choice: DesktopVoiceChoice) => void;
  onListen: (l: DesktopVoiceLanguage, choice: DesktopVoiceChoice) => void;
}) {
  const { t } = useI18n();
  const languages = LANGUAGES.filter((l) => (caps.speakEngines?.[l] ?? []).length > 0);
  if (languages.length === 0) return null;
  const l = languages.includes(shown) ? shown : languages[0];
  const { choice } = effectiveVoice(caps, l, prefs.voices[l]);
  if (!choice?.engine) return null;
  const voices: DesktopVoiceChoice[] = (caps.speakEngines?.[l] ?? []).flatMap((o) =>
    o.styles.length > 0 ? o.styles.map((style) => ({ engine: o.engine, style })) : [{ engine: o.engine }],
  );
  return (
    <div className="space-y-3 border-t border-border pt-4">
      <div className="flex items-center justify-between gap-4">
        <Label htmlFor="voice-language">{t("settings.voice.language")}</Label>
        <Select value={l} onValueChange={(v) => onShow(v as DesktopVoiceLanguage)}>
          <SelectTrigger id="voice-language" className="w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {languages.map((x) => (
              <SelectItem key={x} value={x}>
                {t(x === "tr" ? "settings.voice.languageTr" : "settings.voice.languageEn")}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex items-center justify-between gap-4">
        <Label htmlFor="voice-style">{t("settings.voice.style")}</Label>
        <div className="flex items-center gap-2">
          <Select
            value={voiceKey(choice)}
            onValueChange={(v) => {
              const picked = voices.find((x) => voiceKey(x) === v);
              if (picked) onPick(l, picked);
            }}
          >
            <SelectTrigger id="voice-style" className="w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {voices.map((x) => (
                <SelectItem key={voiceKey(x)} value={voiceKey(x)}>
                  {voiceLabel(t, x)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button type="button" variant="outline" size="sm" onClick={() => onListen(l, choice)}>
            {playing === l ? <Square className="mr-1.5 h-4 w-4" /> : <Play className="mr-1.5 h-4 w-4" />}
            {t(playing === l ? "settings.voice.previewStop" : "settings.voice.preview")}
          </Button>
        </div>
      </div>
    </div>
  );
}
