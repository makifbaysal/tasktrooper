import { BarChart3 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { desktopAnalytics, type DesktopAnalyticsState } from "@/lib/desktop-bridge";

/**
 * Settings → General's switch for the anonymous active-install count.
 *
 * Nothing renders when the shell has no analytics bridge or this build carries
 * no analytics configuration: there is then nothing to switch.
 */
export function AnalyticsCard() {
  const { t } = useI18n();
  const analytics = desktopAnalytics();
  const [state, setState] = useState<DesktopAnalyticsState | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!analytics) return;
    void analytics.get().then(setState, () => undefined);
  }, [analytics]);

  const toggle = useCallback(
    async (on: boolean) => {
      if (!analytics) return;
      setSaving(true);
      try {
        setState(await analytics.set(on));
      } catch (e) {
        toast.error(e instanceof Error ? e.message : t("settings.analytics.saveFailed"));
      } finally {
        setSaving(false);
      }
    },
    [analytics, t],
  );

  if (!analytics || !state?.available) return null;

  return (
    <Card className="w-full space-y-3 p-6">
      <div className="flex items-center justify-between gap-4">
        <Label htmlFor="analytics-enabled" className="flex items-center gap-2">
          <BarChart3 className="h-4 w-4" />
          {t("settings.analytics.label")}
        </Label>
        <Switch
          id="analytics-enabled"
          checked={state.enabled}
          disabled={saving || state.forcedOff}
          onCheckedChange={(checked) => void toggle(checked)}
        />
      </div>
      <p className="text-sm text-muted-foreground">
        {state.forcedOff ? t("settings.analytics.forcedOff") : t("settings.analytics.help")}
      </p>
    </Card>
  );
}
