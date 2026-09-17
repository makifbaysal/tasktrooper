import { Bell, Globe, Package, Save } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, setStoredLocale, type AppSettings } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { tStatic, useI18n, type Lang } from "@/hooks/useI18n";
import { desktopRunner, type DesktopNotificationPreferences } from "@/lib/desktop-bridge";

const NOTIFICATION_CATEGORY_FIELDS = ["analizReview", "humanUat", "humanNeeded", "agentComments"] as const;
type NotificationCategoryField = (typeof NOTIFICATION_CATEGORY_FIELDS)[number];

const languageOptions = [
  { value: "tr", label: "Türkçe" },
  { value: "en", label: "English" },
];


function BoilerplateCatalogCard() {
  const { t } = useI18n();
  const [repo, setRepo] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      setRepo(data.boilerplate_catalog_repo ?? "");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : tStatic("settings.boilerplate.loadFailed"));
    } finally {
      setLoading(false);
    }
    // Not keyed on `t`: a language switch must not discard the unsaved input.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const save = async (value: string) => {
    setSaving(true);
    try {
      const data = await api.updateSettings({ boilerplate_catalog_repo: value.trim() || "-" });
      setRepo(data.boilerplate_catalog_repo ?? "");
      toast.success(t("common.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="mt-4 w-full space-y-3 p-6">
      <Label htmlFor="boilerplate-repo" className="flex items-center gap-2">
        <Package className="h-4 w-4" />
        {t("settings.boilerplate.title")}
      </Label>
      <p className="text-sm text-muted-foreground">
        {t("settings.boilerplate.descPrefix")}{" "}
        <code className="text-xs">.ai/catalog.yaml</code>
        {t("settings.boilerplate.descMid")} <code className="text-xs">owner/repo</code>,{" "}
        <code className="text-xs">github.com/owner/repo</code> {t("settings.boilerplate.descSuffix")}
      </p>
      {loading ? (
        <Skeleton className="h-10 w-full" />
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Input
            id="boilerplate-repo"
            value={repo}
            onChange={(e) => setRepo(e.target.value)}
            placeholder="github.com/your-org/boilerplates"
            className="max-w-md"
          />
          <Button onClick={() => void save(repo)} disabled={saving}>
            <Save className="mr-2 h-4 w-4" />
            {saving ? t("common.saving") : t("common.save")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => void save("")} disabled={saving}>
            {t("common.resetDefault")}
          </Button>
        </div>
      )}
    </Card>
  );
}

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

  if (!runner) {
    return (
      <Card className="mt-4 w-full space-y-2 p-6">
        <Label className="flex items-center gap-2">
          <Bell className="h-4 w-4" />
          {t("settings.notifications.title")}
        </Label>
        <p className="text-sm text-muted-foreground">{t("settings.notifications.unavailable")}</p>
      </Card>
    );
  }

  return (
    <Card className="mt-4 w-full space-y-4 p-6">
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

export function SettingsPage() {
  const { t, setLang } = useI18n();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [language, setLanguage] = useState<string>("en");

  // Deliberately not depending on `t`: switching the language changes `t`'s
  // identity, which would refire this loader and overwrite the just-picked
  // language with the (still unsaved) server value — making the switcher
  // impossible to use. tStatic reads the active language at call time.
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.getSettings();
      // Only seed the form. Applying the server value to the live UI here made
      // merely opening Settings switch the app's language (and persist it) to
      // whatever the server last stored.
      setLanguage(data.default_language ?? "en");
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

  // Applied only after an explicit Save: the user just chose this language, so
  // the live UI and the persisted locale follow the server's echo of it.
  const applySettings = (data: AppSettings) => {
    const next = data.default_language ?? "en";
    setLanguage(next);
    setStoredLocale(next);
    setLang(next === "tr" ? "tr" : "en");
  };

  // Switch the UI language instantly; the backend default_language is persisted on Save.
  const handleLanguageChange = (value: string) => {
    setLanguage(value);
    setLang(value === "tr" ? ("tr" as Lang) : ("en" as Lang));
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const data = await api.updateSettings({ default_language: language });
      applySettings(data);
      toast.success(t("settings.savedToast"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      {loading ? (
        <Card className="w-full p-6">
          <Skeleton className="mb-4 h-10 w-full" />
          <Skeleton className="h-10 w-32" />
        </Card>
      ) : (
        <Card className="w-full space-y-6 p-6">
          <div className="space-y-2">
            <Label htmlFor="language" className="flex items-center gap-2">
              <Globe className="h-4 w-4" />
              {t("settings.language.label")}
            </Label>
            <Select value={language} onValueChange={handleLanguageChange}>
              <SelectTrigger id="language">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {languageOptions.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{t("settings.language.help")}</p>
          </div>

          <Button onClick={handleSave} disabled={saving}>
            <Save className="mr-2 h-4 w-4" />
            {saving ? t("common.saving") : t("common.save")}
          </Button>
        </Card>
      )}
      <BoilerplateCatalogCard />
      <NotificationsCard />
    </>
  );
}
