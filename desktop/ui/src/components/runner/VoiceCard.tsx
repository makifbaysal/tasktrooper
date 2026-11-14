import { Mic } from "lucide-react";
import { useEffect, useState } from "react";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { voiceLanguageFor } from "@/hooks/useVoiceConversation";
import { desktopVoice, type DesktopVoiceCapabilities } from "@/lib/desktop-bridge";
import { readVoicePrefs, writeVoicePrefs, type VoicePrefs } from "@/lib/voice/prefs";

const SPEEDS = [0.75, 1, 1.25, 1.5];

/** Settings → App: talking to agents. Absent where the shell has no voice bridge. */
export function VoiceCard() {
  const { t, lang } = useI18n();
  const voice = desktopVoice();
  const [caps, setCaps] = useState<DesktopVoiceCapabilities | null>(null);
  const [prefs, setPrefs] = useState<VoicePrefs>(readVoicePrefs);

  useEffect(() => {
    if (!voice) return;
    void voice.capabilities().then(setCaps, () => undefined);
  }, [voice]);

  if (!voice) return null;
  const voiceLang = voiceLanguageFor(lang);
  const available = Boolean(voiceLang && caps?.listen[voiceLang]);

  const update = (patch: Partial<VoicePrefs>) => {
    const next = { ...prefs, ...patch };
    setPrefs(next);
    writeVoicePrefs(next);
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
    </Card>
  );
}
