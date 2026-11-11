import { Activity, CheckCircle2, KeyRound, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { api, type TelemetrySettings } from "@/api";
import { IntegrationCard } from "@/components/admin/IntegrationCard";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

type Probe = { state: "idle" } | { state: "testing" } | { state: "ok" } | { state: "failed"; error?: string };

/** The telemetry product's address and key. The key is write-only: the server
 * never returns it, so the field is empty after a save and "configured" is
 * all the page can say about what is stored. */
export function TelemetryCard() {
  const { t } = useI18n();
  const s = (key: string) => t(`telemetryHub.settings.${key}`);
  const [saved, setSaved] = useState<TelemetrySettings | null>(null);
  const [loadError, setLoadError] = useState<string | undefined>(undefined);
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [probe, setProbe] = useState<Probe>({ state: "idle" });

  const load = useCallback(async () => {
    try {
      const next = await api.telemetrySettings();
      setSaved(next);
      setBaseUrl(next.base_url);
      setLoadError(undefined);
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : s("loadFailed"));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const persist = async (body: { base_url: string; api_key?: string }, done: string) => {
    setSaving(true);
    try {
      const next = await api.saveTelemetrySettings(body);
      setSaved(next);
      setBaseUrl(next.base_url);
      setApiKey("");
      setProbe({ state: "idle" });
      toast.success(done);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : s("saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const key = apiKey.trim();
    void persist({ base_url: baseUrl.trim(), ...(key ? { api_key: key } : {}) }, s("saved"));
  };

  const configured = saved?.configured ?? false;
  const dirty = saved !== null && (baseUrl.trim() !== saved.base_url || apiKey.trim() !== "");

  const test = async () => {
    setProbe({ state: "testing" });
    try {
      const key = apiKey.trim();
      const result = await api.testTelemetry(dirty ? { base_url: baseUrl.trim(), ...(key ? { api_key: key } : {}) } : undefined);
      setProbe(result.ok ? { state: "ok" } : { state: "failed", error: result.error });
    } catch (e) {
      setProbe({ state: "failed", error: e instanceof Error ? e.message : undefined });
    }
  };

  const status =
    saved === null
      ? undefined
      : probe.state === "ok"
        ? { tone: "connected" as const, label: s("statusConnected") }
        : probe.state === "failed"
          ? { tone: "attention" as const, label: s("statusUnreachable") }
          : configured
            ? { tone: "connected" as const, label: s("statusConfigured") }
            : { tone: "idle" as const, label: s("statusNotConfigured") };
  const canSave = baseUrl.trim() !== "" && dirty && !saving;
  const canTest = (configured && !dirty) || (baseUrl.trim() !== "" && (apiKey.trim() !== "" || configured));

  return (
    <IntegrationCard
      icon={<Activity className="h-5 w-5" aria-hidden />}
      name={s("title")}
      description={s("description")}
      {...(status ? { status } : {})}
    >
      {loadError ? (
        <Notice variant="error" title={s("loadFailed")}>
          {loadError}
        </Notice>
      ) : saved === null ? (
        <div className="space-y-3">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      ) : (
        <form className="space-y-4" onSubmit={submit}>
          <div className="space-y-2">
            <Label htmlFor="telemetry-base-url">{s("urlLabel")}</Label>
            <Input
              id="telemetry-base-url"
              type="url"
              inputMode="url"
              autoComplete="off"
              spellCheck={false}
              placeholder="https://telemetry.example.com"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
            />
            <p className="text-caption text-muted-foreground">{s("urlHelp")}</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="telemetry-api-key">{s("keyLabel")}</Label>
            <Input
              id="telemetry-api-key"
              type="password"
              autoComplete="off"
              spellCheck={false}
              placeholder={configured ? s("keyPlaceholderSaved") : s("keyPlaceholder")}
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
            />
            <p className="flex items-center gap-1.5 text-caption text-muted-foreground">
              <KeyRound className="h-3.5 w-3.5 shrink-0" aria-hidden />
              {configured ? s("keyHelpSaved") : s("keyHelp")}
            </p>
          </div>

          {probe.state === "ok" && (
            <Notice variant="info" title={s("testOk")} role="status" />
          )}
          {probe.state === "failed" && (
            <Notice variant="error" title={s("testFailed")}>
              {probe.error}
            </Notice>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Button type="submit" disabled={!canSave}>
              {saving ? s("saving") : s("save")}
            </Button>
            <Button type="button" variant="outline" onClick={() => void test()} disabled={!canTest || saving || probe.state === "testing"}>
              <CheckCircle2 aria-hidden />
              {probe.state === "testing" ? s("testing") : s("test")}
            </Button>
            {configured && (
              <Button
                type="button"
                variant="ghost"
                className="text-destructive hover:text-destructive"
                onClick={() => void persist({ base_url: saved.base_url, api_key: "" }, s("keyRemoved"))}
                disabled={saving}
              >
                <Trash2 aria-hidden />
                {s("removeKey")}
              </Button>
            )}
          </div>
        </form>
      )}
    </IntegrationCard>
  );
}
