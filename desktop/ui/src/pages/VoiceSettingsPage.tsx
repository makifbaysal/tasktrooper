import { SettingsSection } from "@/components/layout/SettingsSection";
import { VoiceCard } from "@/components/runner/VoiceCard";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { desktopVoice } from "@/lib/desktop-bridge";

export function VoiceSettingsPage() {
  const { t } = useI18n();
  return (
    <div className="max-w-3xl space-y-10">
      <SettingsSection title={t("settings.sections.voice.title")} description={t("settings.sections.voice.description")}>
        {desktopVoice() ? <VoiceCard /> : <Notice variant="info" title={t("settings.voice.unavailable")} />}
      </SettingsSection>
    </div>
  );
}
