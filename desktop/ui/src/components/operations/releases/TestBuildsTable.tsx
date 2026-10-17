import { GitBranch, ScrollText, ShieldCheck, Users, X } from "lucide-react";
import { Fragment, useState } from "react";
import { toast } from "sonner";
import { api, type StoreAppView, type StoreTestBuild, type StoreTestGroup } from "@/api";
import { ExportComplianceDialog } from "@/components/operations/ExportComplianceDialog";
import { OpenToGroupsDialog } from "@/components/operations/OpenToGroupsDialog";
import { ReleaseSection } from "@/components/operations/releases/ReleaseSection";
import { TestBuildPill } from "@/components/operations/releases/StorePills";
import { CopyButton, ExternalAnchor } from "@/components/operations/StoreTestBuildParts";
import {
  isTestBuildActionable,
  isTestBuildActive,
  needsExportCompliance,
  tailLines,
  testBuildGroups,
  testBuildLabel,
  testGroupName,
} from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useStoreAction } from "@/hooks/useMobileReleases";
import { useStoreTestBuilds } from "@/hooks/useStoreTestBuilds";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

export const TEST_BUILD_LIMIT = 20;
const LOG_LINES = 60;
const KNOWN_TRIGGERS = new Set(["human_uat", "manual", "release"]);

const TH = "px-3 py-2 text-left text-micro font-medium tracking-wide whitespace-nowrap text-muted-foreground uppercase";
const TD = "px-3 py-2 align-top";

interface TestBuildsTableProps {
  app: StoreAppView;
  groups: StoreTestGroup[] | null;
  groupsError: string | null;
}

interface Removal {
  build: StoreTestBuild;
  groupId: string;
}

/** The app's recent test builds — task UAT, default-branch and release builds —
 * newest first; re-read every 5 s only while one is still moving on its own. */
export function TestBuildsTable({ app, groups, groupsError }: TestBuildsTableProps) {
  const { t, lang } = useI18n();
  const b = (key: string, params?: Record<string, string | number>) => t(`operations.mobileReleases.builds.${key}`, params);
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.storeTest.${key}`, params);
  const repositoryId = app.repository_id;
  const ios = app.platform === "ios";
  const { builds, error, unavailable, merge } = useStoreTestBuilds(repositoryId, { platform: app.platform, limit: TEST_BUILD_LIMIT });
  const { busy, run } = useStoreAction();
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [openFor, setOpenFor] = useState<StoreTestBuild | null>(null);
  const [complianceFor, setComplianceFor] = useState<StoreTestBuild | null>(null);
  const [removal, setRemoval] = useState<Removal | null>(null);
  const [removing, setRemoving] = useState(false);

  if (unavailable) return null;

  const moving = builds?.some((build) => isTestBuildActive(build.status)) ?? false;

  const start = () =>
    run(
      "start",
      () => api.startStoreTestBuilds(repositoryId, { platforms: [app.platform] }),
      (res) => {
        merge(res.builds ?? []);
        if (!res.error) return s("buildStarted");
        toast.warning(s("buildStartedPartial", { error: res.error }));
      },
    );

  const remove = async () => {
    if (!removal) return;
    const name = testGroupName(removal.groupId, groups);
    setRemoving(true);
    await run("close", () => api.closeStoreTestBuild(repositoryId, removal.build.id, [removal.groupId]), (build) => {
      merge([build]);
      return s("removed", { group: name });
    });
    setRemoving(false);
  };

  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const removalName = removal ? testGroupName(removal.groupId, groups) : "";

  return (
    <ReleaseSection
      title={s("buildsTitle")}
      actions={
        <>
          {moving && (
            <span className="inline-flex items-center gap-1.5 text-micro text-muted-foreground">
              <span className="h-1.5 w-1.5 rounded-full bg-info" aria-hidden />
              {b("live")}
            </span>
          )}
          <Button size="sm" variant="outline" disabled={busy !== null} onClick={() => void start()}>
            <GitBranch aria-hidden />
            {s("buildDefaultBranch")}
          </Button>
        </>
      }
    >
      {error && (
        <Notice variant="warning" title={s("buildsFailed")}>
          {error}
        </Notice>
      )}
      {builds === null ? (
        <Skeleton className="h-24 w-full" />
      ) : builds.length === 0 ? (
        !error && <Card className="px-4 py-6 text-center text-caption text-muted-foreground">{s("buildsEmpty")}</Card>
      ) : (
        <Card className="overflow-hidden">
          <div className="relative overflow-x-auto">
            <table className="w-full min-w-[720px] border-collapse text-caption" aria-label={s("buildsTitle")}>
              <thead className="bg-muted/40">
                <tr className="border-b border-border">
                  <th scope="col" className={TH}>{b("columns.status")}</th>
                  <th scope="col" className={TH}>{b("columns.build")}</th>
                  <th scope="col" className={TH}>{b("columns.source")}</th>
                  <th scope="col" className={TH}>{b("columns.groups")}</th>
                  <th scope="col" className={TH}>{b("columns.started")}</th>
                  <th scope="col" className={cn(TH, "text-right")}>
                    <span className="sr-only">{b("columns.actions")}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {builds.map((build) => {
                  const actionable = isTestBuildActionable(build.status);
                  const ready = build.status === "ready";
                  const groupIds = testBuildGroups(build);
                  const log = tailLines(build.log_tail, LOG_LINES);
                  const hasDetail = Boolean(log) || (build.status === "failed" && Boolean(build.failure));
                  const open = expanded.has(build.id);
                  const detailId = `build-detail-${build.id}`;
                  const label = testBuildLabel(build);
                  const trigger = b(`triggers.${KNOWN_TRIGGERS.has(build.trigger) ? build.trigger : "unknown"}`);
                  return (
                    <Fragment key={build.id}>
                      <tr className={cn("border-b border-border/60", open && "border-b-0")}>
                        <td className={TD}>
                          <TestBuildPill status={build.status} />
                        </td>
                        <td className={cn(TD, "font-mono tabular-nums whitespace-nowrap")}>
                          <span className="font-semibold">{build.build_number}</span>
                          {build.version_name && <span className="text-muted-foreground"> · {build.version_name}</span>}
                        </td>
                        <td className={TD}>
                          <span className="block max-w-52 truncate font-mono">{label || build.branch || s("defaultBranch")}</span>
                          <span className="block max-w-52 truncate text-micro text-muted-foreground">
                            {trigger}
                            {label && build.branch ? ` · ${build.branch}` : ""}
                          </span>
                        </td>
                        <td className={TD}>
                          {!actionable || groupIds.length === 0 ? (
                            <span className="text-muted-foreground" title={actionable ? s("notOpen") : undefined}>
                              —{actionable && <span className="sr-only">{s("notOpen")}</span>}
                            </span>
                          ) : (
                            <span className="flex max-w-60 flex-wrap gap-1">
                              {groupIds.map((id) => {
                                const name = testGroupName(id, groups);
                                const removable = ios && !groups?.find((g) => g.id === id)?.all_builds;
                                return (
                                  <Badge key={id} variant="outline" className="gap-1 py-0 pr-1 font-normal">
                                    <span className="max-w-36 truncate">{name}</span>
                                    {removable && (
                                      <Button
                                        type="button"
                                        size="icon"
                                        variant="ghost"
                                        className="h-4 w-4 rounded-full p-0 [&_svg]:size-3"
                                        disabled={removing}
                                        onClick={() => setRemoval({ build, groupId: id })}
                                        aria-label={s("removeFromGroup", { group: name })}
                                        title={s("removeFromGroup", { group: name })}
                                      >
                                        <X aria-hidden />
                                      </Button>
                                    )}
                                  </Badge>
                                );
                              })}
                            </span>
                          )}
                        </td>
                        <td className={cn(TD, "whitespace-nowrap text-muted-foreground")} title={formatDate(build.created_at)}>
                          {formatRelativeTime(build.created_at, lang)}
                        </td>
                        <td className={cn(TD, "text-right")}>
                          <span className="inline-flex flex-wrap items-center justify-end gap-1">
                            {actionable && needsExportCompliance(build) && (
                              <Button size="sm" className="h-7" onClick={() => setComplianceFor(build)}>
                                <ShieldCheck aria-hidden />
                                {b("compliance")}
                              </Button>
                            )}
                            {actionable && ready && (
                              <Button size="sm" variant="outline" className="h-7" onClick={() => setOpenFor(build)}>
                                <Users aria-hidden />
                                {s("openTo")}
                              </Button>
                            )}
                            {ready && !ios && build.install_url && (
                              <span className="inline-flex items-center">
                                <ExternalAnchor href={build.install_url}>{s("installLink")}</ExternalAnchor>
                                <CopyButton value={build.install_url} label={s("copyLink")} />
                              </span>
                            )}
                            {build.run_url && <ExternalAnchor href={build.run_url}>{s("runLink")}</ExternalAnchor>}
                            {hasDetail && (
                              <Button
                                size="icon"
                                variant="ghost"
                                className="h-7 w-7"
                                aria-expanded={open}
                                aria-controls={detailId}
                                aria-label={open ? s("hideLog") : s("showLog")}
                                title={open ? s("hideLog") : s("showLog")}
                                onClick={() => toggle(build.id)}
                              >
                                <ScrollText aria-hidden />
                              </Button>
                            )}
                          </span>
                        </td>
                      </tr>
                      {open && (
                        <tr id={detailId} className="border-b border-border/60 bg-muted/20">
                          <td colSpan={6} className="px-3 pt-0 pb-3">
                            {build.failure && build.status === "failed" && (
                              <p className="mb-1.5 text-caption whitespace-pre-line text-destructive">{build.failure}</p>
                            )}
                            {log && (
                              <pre
                                aria-label={b("logLabel", { build: build.build_number })}
                                className="max-h-64 overflow-auto rounded-md border border-border bg-surface-sunken p-2 font-mono text-[11px] leading-snug whitespace-pre-wrap text-muted-foreground"
                              >
                                {log}
                              </pre>
                            )}
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <OpenToGroupsDialog
        repositoryId={repositoryId}
        build={openFor}
        groups={groups}
        groupsError={groupsError}
        open={openFor !== null}
        onOpenChange={(next) => !next && setOpenFor(null)}
        onOpened={(build) => merge([build])}
      />
      <ExportComplianceDialog
        repositoryId={repositoryId}
        build={complianceFor}
        open={complianceFor !== null}
        onOpenChange={(next) => !next && setComplianceFor(null)}
        onAnswered={(build) => merge([build])}
      />
      <ConfirmDialog
        open={removal !== null}
        onOpenChange={(next) => !next && setRemoval(null)}
        title={s("removeTitle", { group: removalName })}
        description={s("removeDescription", { group: removalName, build: removal?.build.build_number ?? "" })}
        confirmLabel={s("removeConfirm")}
        loading={removing}
        onConfirm={remove}
      />
    </ReleaseSection>
  );
}
