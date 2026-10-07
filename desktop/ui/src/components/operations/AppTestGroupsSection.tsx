import { Plus, RefreshCw, Users } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api, type StoreAppView, type StoreTestGroup } from "@/api";
import { NewTestGroupDialog } from "@/components/operations/NewTestGroupDialog";
import { StoreTestersDialog } from "@/components/operations/StoreTestersDialog";
import { CopyButton, ExternalAnchor } from "@/components/operations/StoreTestBuildParts";
import { autoGroupSet, needsBetaReview, testGroupKindLabelKey } from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import type { StoreTestGroupsState } from "@/hooks/useStoreTestGroups";
import { cn } from "@/lib/utils";

interface AppTestGroupsSectionProps {
  app: StoreAppView;
  state: StoreTestGroupsState;
}

/**
 * Where an app's test builds can be opened: TestFlight groups on iOS, Play
 * testing tracks on Android. The "auto" switches are one saved set — the
 * groups a new task build is opened to as soon as it is ready.
 */
export function AppTestGroupsSection({ app, state }: AppTestGroupsSectionProps) {
  const { t } = useI18n();
  const { groups, error, loading, reload, update } = state;
  const [savingAuto, setSavingAuto] = useState(false);
  const [creating, setCreating] = useState(false);
  const [testersFor, setTestersFor] = useState<StoreTestGroup | null>(null);
  const ios = app.platform === "ios";

  const toggleAuto = async (group: StoreTestGroup, on: boolean) => {
    const before = groups ?? [];
    const set = autoGroupSet(before, group.id, on);
    update((prev) => prev.map((g) => (g.id === group.id ? { ...g, auto_distribute: on } : g)));
    setSavingAuto(true);
    try {
      await api.setStoreTestAutoGroups(app.repository_id, app.platform, set);
      toast.success(t("operations.storeTest.autoSaved"));
    } catch (e) {
      update(() => before);
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setSavingAuto(false);
    }
  };

  const changeCount = (groupId: string, delta: number) => {
    update((prev) =>
      prev.map((g) => (g.id === groupId && g.tester_count >= 0 ? { ...g, tester_count: Math.max(0, g.tester_count + delta) } : g)),
    );
  };

  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">
          {t(ios ? "operations.storeTest.groupsTitleIos" : "operations.storeTest.groupsTitleAndroid")}
        </h3>
        <div className="flex items-center gap-1">
          <Button
            size="icon"
            variant="ghost"
            className="h-8 w-8"
            disabled={loading}
            onClick={() => void reload()}
            aria-label={t("operations.storeTest.refreshGroups")}
            title={t("operations.storeTest.refreshGroups")}
          >
            <RefreshCw className={cn("h-4 w-4", loading && "animate-spin")} />
          </Button>
          {ios && (
            <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
              <Plus className="h-4 w-4" />
              {t("operations.storeTest.newGroup")}
            </Button>
          )}
        </div>
      </div>
      <p className="text-xs text-muted-foreground">{t("operations.storeTest.autoHint")}</p>

      {error && (
        <Notice variant="warning" title={t("operations.storeTest.groupsFailed")}>
          {error}
        </Notice>
      )}
      {groups === null && !error && <Skeleton className="h-20 w-full" />}
      {groups !== null && groups.length === 0 && (
        <p className="text-sm text-muted-foreground">{t("operations.storeTest.noGroups")}</p>
      )}

      {groups !== null && groups.length > 0 && (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {groups.map((group) => {
            const facts = [
              group.tester_count >= 0 ? t("operations.storeTest.testerCount", { count: group.tester_count }) : null,
              group.current_build ? t("operations.storeTest.currentBuild", { build: group.current_build }) : null,
            ].filter(Boolean);
            return (
              <li key={group.id} className="flex flex-wrap items-start gap-3 px-3 py-2.5">
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="truncate text-sm font-medium">{group.name}</span>
                    <Badge variant="outline" className="py-0 font-normal">
                      {t(testGroupKindLabelKey(group.kind))}
                    </Badge>
                    {needsBetaReview(group) && (
                      <Badge variant="warning" className="py-0">
                        {t("operations.storeTest.betaReview")}
                      </Badge>
                    )}
                  </div>
                  {facts.length > 0 && <p className="text-xs text-muted-foreground">{facts.join(" · ")}</p>}
                  {group.public_link && (
                    <div className="flex min-w-0 items-center gap-0.5">
                      <ExternalAnchor href={group.public_link} className="min-w-0 truncate">
                        {t("operations.storeTest.publicLink")}
                      </ExternalAnchor>
                      <CopyButton value={group.public_link} label={t("operations.storeTest.copyLink")} />
                    </div>
                  )}
                  {group.all_builds && <p className="text-xs text-muted-foreground">{t("operations.storeTest.autoAlways")}</p>}
                </div>
                <div className="flex items-center gap-2">
                  {ios && (
                    <Button size="sm" variant="ghost" className="h-8" onClick={() => setTestersFor(group)}>
                      <Users className="h-4 w-4" />
                      {t("operations.storeTest.testers")}
                    </Button>
                  )}
                  <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <Switch
                      checked={group.auto_distribute || Boolean(group.all_builds)}
                      disabled={Boolean(group.all_builds) || savingAuto}
                      onCheckedChange={(on) => void toggleAuto(group, on)}
                      aria-label={t("operations.storeTest.autoFor", { group: group.name })}
                    />
                    {t("operations.storeTest.autoLabel")}
                  </label>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {ios && (
        <NewTestGroupDialog
          repositoryId={app.repository_id}
          platform={app.platform}
          open={creating}
          onOpenChange={setCreating}
          onCreated={(group) => update((prev) => [...prev.filter((g) => g.id !== group.id), group])}
        />
      )}
      {ios && (
        <StoreTestersDialog
          repositoryId={app.repository_id}
          group={testersFor}
          onOpenChange={(open) => !open && setTestersFor(null)}
          onCountChange={changeCount}
        />
      )}
    </section>
  );
}
