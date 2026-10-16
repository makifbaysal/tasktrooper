import { Plus, RefreshCw, Users } from "lucide-react";
import { useState } from "react";
import { api, type StoreAppView, type StoreTestGroup } from "@/api";
import { CreateTestGroupDialog } from "@/components/operations/releases/CreateTestGroupDialog";
import { GroupTestersDialog } from "@/components/operations/releases/GroupTestersDialog";
import { ReleaseSection } from "@/components/operations/releases/ReleaseSection";
import { CopyButton, ExternalAnchor } from "@/components/operations/StoreTestBuildParts";
import { autoGroupSet, needsBetaReview, testGroupKindLabelKey } from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { useStoreAction } from "@/hooks/useMobileReleases";
import type { StoreTestGroupsState } from "@/hooks/useStoreTestGroups";
import { cn } from "@/lib/utils";

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide whitespace-nowrap text-muted-foreground uppercase";
const TD = "px-3 py-2 align-middle";

/** Where the app's test builds can be opened — TestFlight groups on iOS, Play
 * testing tracks on Android — and which of them get every new task build. */
export function TestGroupsTable({ app, state }: { app: StoreAppView; state: StoreTestGroupsState }) {
  const { t } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.storeTest.${key}`, params);
  const g = (key: string) => t(`operations.mobileReleases.groups.${key}`);
  const { groups, error, loading, reload, update } = state;
  const { busy, run } = useStoreAction();
  const [creating, setCreating] = useState(false);
  const [testersFor, setTestersFor] = useState<StoreTestGroup | null>(null);
  const ios = app.platform === "ios";

  const toggleAuto = async (group: StoreTestGroup, on: boolean) => {
    const before = groups ?? [];
    const set = autoGroupSet(before, group.id, on);
    update((prev) => prev.map((x) => (x.id === group.id ? { ...x, auto_distribute: on } : x)));
    const ok = await run("auto", () => api.setStoreTestAutoGroups(app.repository_id, app.platform, set), s("autoSaved"));
    if (!ok) update(() => before);
  };

  const changeCount = (groupId: string, delta: number) =>
    update((prev) =>
      prev.map((x) => (x.id === groupId && x.tester_count >= 0 ? { ...x, tester_count: Math.max(0, x.tester_count + delta) } : x)),
    );

  return (
    <ReleaseSection
      title={s(ios ? "groupsTitleIos" : "groupsTitleAndroid")}
      actions={
        <>
          <Button
            size="icon"
            variant="ghost"
            className="h-8 w-8"
            disabled={loading}
            onClick={() => void reload()}
            aria-label={s("refreshGroups")}
            title={s("refreshGroups")}
          >
            <RefreshCw className={cn(loading && "animate-spin")} aria-hidden />
          </Button>
          {ios && (
            <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
              <Plus aria-hidden />
              {s("newGroup")}
            </Button>
          )}
        </>
      }
    >
      <p className="text-caption text-muted-foreground">{s("autoHint")}</p>
      {error && (
        <Notice variant="warning" title={s("groupsFailed")}>
          {error}
        </Notice>
      )}
      {groups === null && !error && <Skeleton className="h-20 w-full" />}
      {groups !== null && groups.length === 0 && (
        <Card className="px-4 py-6 text-center text-caption text-muted-foreground">{s("noGroups")}</Card>
      )}
      {groups !== null && groups.length > 0 && (
        <Card className="overflow-hidden">
          <div className="relative overflow-x-auto">
            <table className="w-full min-w-[640px] border-collapse text-caption" aria-label={s(ios ? "groupsTitleIos" : "groupsTitleAndroid")}>
              <thead className="bg-muted/40">
                <tr className="border-b border-border">
                  <th scope="col" className={TH}>{g("columns.name")}</th>
                  <th scope="col" className={TH}>{g("columns.kind")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>{g("columns.testers")}</th>
                  <th scope="col" className={TH}>{g("columns.build")}</th>
                  <th scope="col" className={TH}>{s("autoLabel")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>
                    <span className="sr-only">{g("columns.actions")}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {groups.map((group) => (
                  <tr key={group.id} className="border-b border-border/60 last:border-0">
                    <td className={cn(TD, "max-w-64")}>
                      <span className="block truncate text-[13px] font-medium">{group.name}</span>
                      {group.public_link && (
                        <span className="inline-flex items-center">
                          <ExternalAnchor href={group.public_link}>{s("publicLink")}</ExternalAnchor>
                          <CopyButton value={group.public_link} label={s("copyLink")} />
                        </span>
                      )}
                    </td>
                    <td className={TD}>
                      <span className="flex flex-wrap items-center gap-1">
                        <Badge variant="outline" className="py-0 font-normal">
                          {t(testGroupKindLabelKey(group.kind))}
                        </Badge>
                        {needsBetaReview(group) && (
                          <Badge variant="warning" className="py-0">
                            {s("betaReview")}
                          </Badge>
                        )}
                      </span>
                    </td>
                    <td className={cn(TD, "text-right font-mono tabular-nums")}>
                      {group.tester_count >= 0 ? group.tester_count : <span className="text-muted-foreground">—</span>}
                    </td>
                    <td className={cn(TD, "font-mono tabular-nums")}>
                      {group.current_build || <span className="text-muted-foreground">—</span>}
                    </td>
                    <td className={TD}>
                      <span className="flex items-center gap-2">
                        <Switch
                          checked={group.auto_distribute || Boolean(group.all_builds)}
                          disabled={Boolean(group.all_builds) || busy !== null}
                          onCheckedChange={(on) => void toggleAuto(group, on)}
                          aria-label={s("autoFor", { group: group.name })}
                        />
                        {group.all_builds && <span className="text-micro text-muted-foreground">{s("allBuilds")}</span>}
                      </span>
                    </td>
                    <td className={cn(TD, "text-right")}>
                      {ios && (
                        <Button size="sm" variant="ghost" className="h-7" onClick={() => setTestersFor(group)}>
                          <Users aria-hidden />
                          {s("testers")}
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {ios && (
        <>
          <CreateTestGroupDialog
            repositoryId={app.repository_id}
            platform={app.platform}
            open={creating}
            onOpenChange={setCreating}
            onCreated={(group) => update((prev) => [...prev.filter((x) => x.id !== group.id), group])}
          />
          <GroupTestersDialog
            repositoryId={app.repository_id}
            group={testersFor}
            onOpenChange={(open) => !open && setTestersFor(null)}
            onCountChange={changeCount}
          />
        </>
      )}
    </ReleaseSection>
  );
}
