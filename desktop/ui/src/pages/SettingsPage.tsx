import { Bell, Globe } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "@/api";
import { AccountCard } from "@/components/runner/AccountCard";
import { SettingsSection } from "@/components/layout/SettingsSection";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { tStatic, useI18n } from "@/hooks/useI18n";
import { desktopRunner, desktopUpdates, type DesktopNotificationPreferences } from "@/lib/desktop-bridge";
import { LANGUAGE_OPTIONS, normalizeLang, type Lang } from "@/lib/languages";
import { logEvent } from "@/lib/analytics";

const NOTIFICATION_CATEGORY_FIELDS = [
  "analizReview",
  "humanUat",
  "humanNeeded",
  "agentComments",
  "agentChatReplies",
] as const;
type NotificationCategoryField = (typeof NOTIFICATION_CATEGORY_FIELDS)[number];

function NotificationsCard() {
  const { t } = useI18n();
  const runner = desktopRunner();
  const [prefs, setPrefs] = useState<DesktopNotificationPreferences | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    if (!runner) {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const data = await runner.settings();
      setPrefs(data.notifications);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : tStatic("settings.notifications.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runner]);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async (next: DesktopNotificationPreferences) => {
    if (!runner) return;
    setSaving(true);
    try {
      const data = await runner.setPreferences({ notifications: next });
      setPrefs(data.notifications);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settings.notifications.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="w-full space-y-4 p-6">
      <div className="space-y-1">
        <Label className="flex items-center gap-2">
          <Bell className="h-4 w-4" />
          {t("settings.notifications.title")}
        </Label>
        <p className="text-sm text-muted-foreground">{t("settings.notifications.help")}</p>
      </div>
      {loading || !prefs ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <Label htmlFor="notifications-enabled">{t("settings.notifications.enabled")}</Label>
            <Switch
              id="notifications-enabled"
              checked={prefs.enabled}
              disabled={saving}
              onCheckedChange={(checked) => void save({ ...prefs, enabled: checked })}
            />
          </div>
          {NOTIFICATION_CATEGORY_FIELDS.map((field: NotificationCategoryField) => (
            <div key={field} className="flex items-center justify-between pl-4">
              <Label htmlFor={`notifications-${field}`} className="text-muted-foreground">
                {t(`settings.notifications.${field}`)}
              </Label>
              <Switch
                id={`notifications-${field}`}
                checked={prefs[field]}
                disabled={saving || !prefs.enabled}
                onCheckedChange={(checked) => void save({ ...prefs, [field]: checked })}
              />
            </div>
          ))}
        </div>
      )}
    </Card>
  );
}

function LanguageCard({
  value,
  disabled,
  onChange,
}: {
  value: Lang;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useI18n();
  return (
    <Card className="flex w-full flex-col gap-3 p-6 sm:flex-row sm:items-center sm:justify-between sm:gap-6">
      <div className="min-w-0 space-y-1">
        <Label htmlFor="language" className="flex items-center gap-2">
          <Globe className="h-4 w-4" />
          {t("settings.language.label")}
        </Label>
        <p className="text-sm text-muted-foreground">{t("settings.language.help")}</p>
      </div>
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger id="language" className="w-full sm:w-48 sm:shrink-0">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {LANGUAGE_OPTIONS.map((opt) => (
            <SelectItem key={opt.value} value={opt.value}>
              {opt.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Card>
  );
}

export function SettingsPage() {
  const { t, setLang } = useI18n();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [language, setLanguage] = useState<Lang>("en");
  const inDesktopApp = desktopUpdates() !== null;
  const hasNotifications = desktopRunner() !== null;

  // Deliberately not depending on `t`: switching the language changes `t`'s
  // identity, which would refire this loader and overwrite the just-picked
  // language with the server value it is still saving.
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      // Only seed the picker. Applying the server value to the live UI here
      // made merely opening Settings switch the app's language.
      setLanguage(normalizeLang(data.default_language));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : tStatic("settings.loadFailed"));
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // A pick is shown and saved at once; a refused save puts the previous
  // language back, so the UI never shows one the server did not keep.
  const handleLanguageChange = async (value: string) => {
    const next = normalizeLang(value);
    const previous = language;
    setLanguage(next);
    setLang(next);
    setSaving(true);
    try {
      const data = await api.updateSettings({ default_language: next });
      const saved = normalizeLang(data.default_language);
      setLanguage(saved);
      setLang(saved);
      if (saved !== previous) logEvent("language_change", { language: saved });
      toast.success(tStatic("settings.savedToast"));
    } catch (e) {
      setLanguage(previous);
      setLang(previous);
      toast.error(e instanceof Error ? e.message : tStatic("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-3xl space-y-10">
      <SettingsSection title={t("settings.sections.account.title")} description={t("settings.sections.account.description")}>
        <AccountCard />
      </SettingsSection>

      <SettingsSection
        title={t("settings.sections.preferences.title")}
        description={t("settings.sections.preferences.description")}
      >
        {loading ? (
          <Card className="w-full p-6">
            <Skeleton className="h-10 w-full" />
          </Card>
        ) : (
          <LanguageCard value={language} disabled={saving} onChange={(value) => void handleLanguageChange(value)} />
        )}
        {hasNotifications && <NotificationsCard />}
      </SettingsSection>

      {!inDesktopApp && <Notice variant="info" title={t("settings.sections.desktopOnly")} />}
    </div>
  );
}
