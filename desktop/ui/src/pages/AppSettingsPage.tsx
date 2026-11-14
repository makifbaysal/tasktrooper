import { AnalyticsCard } from "@/components/runner/AnalyticsCard";
import { AppUpdatesCard } from "@/components/runner/AppUpdatesCard";
import { WhatsNewCard } from "@/components/runner/WhatsNewCard";
import { SettingsSection } from "@/components/layout/SettingsSection";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { desktopUpdates } from "@/lib/desktop-bridge";

export function AppSettingsPage() {
  const { t } = useI18n();
  if (desktopUpdates() === null) {
    return (
      <div className="max-w-3xl">
        <Notice variant="info" title={t("settings.sections.desktopOnly")} />
      </div>
    );
  }
  return (
    <div className="max-w-3xl space-y-10">
      <SettingsSection title={t("settings.sections.app.title")} description={t("settings.sections.app.description")}>
        <AppUpdatesCard />
        <WhatsNewCard />
        <AnalyticsCard />
      </SettingsSection>
    </div>
  );
}
