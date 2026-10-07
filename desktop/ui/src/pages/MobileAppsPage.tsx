import { Smartphone } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type MobileStoreState, type StoreAppView } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { MobileAppDetailDrawer } from "@/components/operations/MobileAppDetailDrawer";
import { StorePlatformIcon } from "@/components/operations/StoreTestBuildParts";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

function stateVariant(state: MobileStoreState): "secondary" | "warning" | "success" {
  switch (state) {
    case "live":
      return "success";
    case "test_ready":
    case "onboarding":
      return "warning";
    default:
      return "secondary";
  }
}

// MobileAppsPage — every repository's iOS and Android app in one list, with
// the store release controls that used to require opening App Store Connect
// or the Play Console by hand. Previously this data was reachable only
// through one repository's deploy settings page (MobileStoreStatusPanel);
// this screen is the cross-repository replacement.
export function MobileAppsPage() {
  const { t } = useI18n();
  const [apps, setApps] = useState<StoreAppView[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setApps((await api.listAllStoreApps()) ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  // Recomputed from the freshly-reloaded list on every render (rather than
  // holding its own snapshot), so a successful action inside the drawer shows
  // up-to-date state the moment `load()`'s refetch lands.
  const selected = useMemo(() => apps.find((app) => app.id === selectedId) ?? null, [apps, selectedId]);

  return (
    <>
      <PageHeader title={t("operations.apps.title")} description={t("operations.apps.description")} />

      {loading ? (
        <div className="space-y-2">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : apps.length === 0 ? (
        <EmptyState
          icon={Smartphone}
          title={t("operations.apps.empty")}
          action={
            <Button variant="outline" asChild>
              <Link to="/repositories">{t("operations.apps.emptyAction")}</Link>
            </Button>
          }
        />
      ) : (
        <div className="divide-y divide-border rounded-lg border border-border">
          {apps.map((app) => (
            <button
              key={app.id}
              type="button"
              onClick={() => setSelectedId(app.id)}
              className="flex w-full flex-wrap items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50"
            >
              <StorePlatformIcon platform={app.platform} />
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium">{app.repository_name}</span>
                  <span className="truncate font-mono text-xs text-muted-foreground">{app.identifier}</span>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={stateVariant(app.state)}>{t(`projectAdmin.prodOps.storeState.${app.state}`)}</Badge>
                  {app.review_state && <Badge variant="outline">{app.review_state}</Badge>}
                </div>
              </div>
              <div className="shrink-0 space-y-0.5 text-right text-xs text-muted-foreground">
                {app.last_released_version && (
                  <div>
                    {t("operations.apps.lastReleased")}: {app.last_released_version}
                  </div>
                )}
                {app.last_submitted_version && (
                  <div>
                    {t("operations.apps.lastSubmitted")}: {app.last_submitted_version}
                  </div>
                )}
              </div>
            </button>
          ))}
        </div>
      )}

      <MobileAppDetailDrawer
        app={selected}
        onOpenChange={(open) => !open && setSelectedId(null)}
        onActed={() => void load()}
      />
    </>
  );
}
