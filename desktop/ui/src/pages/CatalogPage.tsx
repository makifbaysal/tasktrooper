import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type CatalogPending, type CatalogSyncState } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

function formatWhen(iso: string | undefined): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleString();
}

export function CatalogPage() {
  const { t } = useI18n();
  const [configured, setConfigured] = useState<boolean | null>(null);
  const [state, setState] = useState<CatalogSyncState | null>(null);
  const [pending, setPending] = useState<CatalogPending[]>([]);
  const [loading, setLoading] = useState(true);
  const [syncing, setSyncing] = useState(false);
  const [applyingId, setApplyingId] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.getCatalogStatus();
      setConfigured(res.configured);
      setState(res.state ?? null);
      if (!res.configured) return;
      const list = await api.listCatalogPending();
      setPending(list.items ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.catalog.loadFailed"));
      setConfigured(false);
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const syncNow = async () => {
    setSyncing(true);
    try {
      const res = await api.syncCatalog();
      setState(res.state);
      toast.success(t("settingsPages.catalog.syncToast"));
      const list = await api.listCatalogPending();
      setPending(list.items ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.catalog.syncFailed"));
      if (err && typeof err === "object" && "state" in err) {
        setState((err as { state: CatalogSyncState }).state);
      }
    } finally {
      setSyncing(false);
    }
  };

  const dismiss = async (id: string) => {
    try {
      await api.dismissCatalogPending(id);
      setPending((cur) => cur.filter((item) => item.id !== id));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.saveFailed"));
    }
  };

  const applyUpstream = async (id: string) => {
    setApplyingId(id);
    try {
      await api.applyCatalogPending(id);
      toast.success(t("settingsPages.catalog.applyToast"));
      const list = await api.listCatalogPending();
      setPending(list.items ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("settingsPages.catalog.applyFailed"));
    } finally {
      setApplyingId(null);
    }
  };

  if (loading && configured === null) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (configured === false) {
    return (
      <Card className="p-6">
        <h2 className="text-lg font-semibold">{t("settingsPages.catalog.notConfigured")}</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("settingsPages.catalog.notConfiguredHelp")}
        </p>
      </Card>
    );
  }

  const summary = state?.last_summary;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="space-y-1">
          <h2 className="text-lg font-semibold">{t("settingsPages.catalog.title")}</h2>
          <p className="text-sm text-muted-foreground">
            {t("settingsPages.catalog.lastSync")}: {formatWhen(state?.last_sync_at)}
          </p>
        </div>
        <Button onClick={syncNow} disabled={syncing}>
          {syncing ? t("settingsPages.catalog.syncing") : t("settingsPages.catalog.syncNow")}
        </Button>
      </div>

      <Card className="p-6">
        <div className="grid gap-4 text-sm sm:grid-cols-2 lg:grid-cols-5">
          <div>
            <div className="text-muted-foreground">{t("settingsPages.catalog.created")}</div>
            <div className="mt-1 text-2xl font-semibold">{summary?.created ?? 0}</div>
          </div>
          <div>
            <div className="text-muted-foreground">{t("settingsPages.catalog.updated")}</div>
            <div className="mt-1 text-2xl font-semibold">{summary?.updated ?? 0}</div>
          </div>
          <div>
            <div className="text-muted-foreground">{t("settingsPages.catalog.merged")}</div>
            <div className="mt-1 text-2xl font-semibold">{summary?.merged ?? 0}</div>
          </div>
          <div>
            <div className="text-muted-foreground">{t("settingsPages.catalog.skipped")}</div>
            <div className="mt-1 text-2xl font-semibold">{summary?.skipped ?? 0}</div>
          </div>
          <div>
            <div className="text-muted-foreground">{t("settingsPages.catalog.pendingCount", { count: pending.length })}</div>
            <div className="mt-1 text-2xl font-semibold">{pending.length}</div>
          </div>
        </div>
        <div className="mt-4 border-t border-border pt-3 text-sm">
          <span className="text-muted-foreground">{t("settingsPages.catalog.repoRef")}: </span>
          <span className="font-mono">{state?.repo_ref ?? "—"}</span>
          {state?.last_error && (
            <p className="mt-2 text-amber-600">
              {t("settingsPages.catalog.error")}: {state.last_error}
            </p>
          )}
        </div>
      </Card>

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-base font-semibold">{t("settingsPages.catalog.pendingTitle")}</h3>
          <span className="text-sm text-muted-foreground">{pending.length}</span>
        </div>
        {pending.length > 0 && (
          <p className="text-sm text-muted-foreground">{t("settingsPages.catalog.pendingHelp")}</p>
        )}
        {pending.length === 0 ? (
          <Card className="p-6 text-sm text-muted-foreground">{t("settingsPages.catalog.pendingEmpty")}</Card>
        ) : (
          pending.map((item) => (
            <Card key={item.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
              <div className="space-y-0.5 text-sm">
                <div className="font-medium">
                  {item.agent_name || item.agent_slug} · {item.name}
                </div>
                <div className="text-muted-foreground">
                  <span className="font-mono text-xs">
                    {item.kind}/{item.action}
                  </span>{" "}
                  · {item.reason}
                </div>
              </div>
              <div className="flex gap-2">
                <Button size="sm" onClick={() => void applyUpstream(item.id)} disabled={applyingId !== null}>
                  {applyingId === item.id ? t("settingsPages.catalog.applying") : t("settingsPages.catalog.applyUpstream")}
                </Button>
                <Button size="sm" variant="outline" onClick={() => void dismiss(item.id)} disabled={applyingId !== null}>
                  {t("settingsPages.catalog.dismiss")}
                </Button>
              </div>
            </Card>
          ))
        )}
      </div>
    </div>
  );
}