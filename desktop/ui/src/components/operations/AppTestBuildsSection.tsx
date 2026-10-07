import { GitBranch, Loader2, ShieldCheck, Users } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api, type StoreAppView, type StoreTestBuild, type StoreTestGroup } from "@/api";
import { ExportComplianceDialog } from "@/components/operations/ExportComplianceDialog";
import { OpenToGroupsDialog } from "@/components/operations/OpenToGroupsDialog";
import {
  CopyButton,
  ExternalAnchor,
  LogTail,
  TestBuildGroupChips,
  TestBuildStatusBadge,
} from "@/components/operations/StoreTestBuildParts";
import {
  isTestBuildActionable,
  needsExportCompliance,
  testBuildGroups,
  testBuildLabel,
  testGroupName,
} from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useStoreTestBuilds } from "@/hooks/useStoreTestBuilds";
import { formatDate, formatRelativeTime } from "@/lib/utils";

interface AppTestBuildsSectionProps {
  app: StoreAppView;
  /** The app's groups/tracks, shared with the groups section so a chip and a row say the same name. */
  groups: StoreTestGroup[] | null;
  groupsError: string | null;
}

interface PendingRemoval {
  build: StoreTestBuild;
  groupId: string;
}

/** Every test build of one app — task builds, default-branch builds, release builds — newest first. */
export function AppTestBuildsSection({ app, groups, groupsError }: AppTestBuildsSectionProps) {
  const { t, lang } = useI18n();
  const repositoryId = app.repository_id;
  const { builds, error, unavailable, merge } = useStoreTestBuilds(repositoryId, { platform: app.platform, limit: 20 });
  const [starting, setStarting] = useState(false);
  const [openFor, setOpenFor] = useState<StoreTestBuild | null>(null);
  const [complianceFor, setComplianceFor] = useState<StoreTestBuild | null>(null);
  const [removal, setRemoval] = useState<PendingRemoval | null>(null);
  const [removing, setRemoving] = useState(false);
  const ios = app.platform === "ios";

  const buildDefaultBranch = async () => {
    setStarting(true);
    try {
      const res = await api.startStoreTestBuilds(repositoryId, { platforms: [app.platform] });
      merge(res.builds ?? []);
      if (res.error) toast.warning(t("operations.storeTest.buildStartedPartial", { error: res.error }));
      else toast.success(t("operations.storeTest.buildStarted"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setStarting(false);
    }
  };

  const removeFromGroup = async () => {
    if (!removal) return;
    const name = testGroupName(removal.groupId, groups);
    setRemoving(true);
    try {
      merge([await api.closeStoreTestBuild(repositoryId, removal.build.id, [removal.groupId])]);
      toast.success(t("operations.storeTest.removed", { group: name }));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setRemoving(false);
    }
  };

  const removalName = removal ? testGroupName(removal.groupId, groups) : "";

  return (
    <section className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">{t("operations.storeTest.buildsTitle")}</h3>
        {!unavailable && (
          <Button size="sm" variant="outline" disabled={starting} onClick={() => void buildDefaultBranch()}>
            {starting ? <Loader2 className="h-4 w-4 animate-spin" /> : <GitBranch className="h-4 w-4" />}
            {t("operations.storeTest.buildDefaultBranch")}
          </Button>
        )}
      </div>

      {error && (
        <Notice variant="warning" title={t("operations.storeTest.buildsFailed")}>
          {error}
        </Notice>
      )}
      {builds === null && <Skeleton className="h-24 w-full" />}
      {builds !== null && builds.length === 0 && !error && (
        <p className="text-sm text-muted-foreground">{t("operations.storeTest.buildsEmpty")}</p>
      )}

      {builds !== null && builds.length > 0 && (
        <ul className="divide-y divide-border rounded-lg border border-border">
          {builds.map((build) => {
            const actionable = isTestBuildActionable(build.status);
            const ready = build.status === "ready";
            const groupIds = testBuildGroups(build);
            return (
              <li key={build.id} className="space-y-1.5 px-3 py-2.5">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-sm tabular-nums">{build.build_number}</span>
                  <span className="text-xs text-muted-foreground">
                    {testBuildLabel(build) || build.branch || t("operations.storeTest.defaultBranch")}
                  </span>
                  {build.trigger === "release" && (
                    <Badge variant="secondary" className="py-0">
                      {t("operations.storeTest.releaseTrigger")}
                    </Badge>
                  )}
                  <TestBuildStatusBadge status={build.status} />
                  <span className="ml-auto text-xs text-muted-foreground" title={formatDate(build.created_at)}>
                    {formatRelativeTime(build.created_at, lang)}
                  </span>
                </div>

                {actionable && (ready || groupIds.length > 0) && (
                  <TestBuildGroupChips
                    ids={groupIds}
                    groups={groups}
                    onRemove={ios ? (groupId) => setRemoval({ build, groupId }) : undefined}
                    disabled={removing}
                  />
                )}

                {build.status === "failed" && build.failure && (
                  <p className="whitespace-pre-line text-xs text-destructive">{build.failure}</p>
                )}
                {build.status === "failed" && <LogTail text={build.log_tail} />}

                <div className="flex flex-wrap items-center gap-2">
                  {actionable && ready && (
                    <Button size="sm" variant="outline" className="h-7" onClick={() => setOpenFor(build)}>
                      <Users className="h-3.5 w-3.5" />
                      {t("operations.storeTest.openTo")}
                    </Button>
                  )}
                  {actionable && needsExportCompliance(build) && (
                    <Button size="sm" className="h-7" onClick={() => setComplianceFor(build)}>
                      <ShieldCheck className="h-3.5 w-3.5" />
                      {t("operations.storeTest.complianceAnswer")}
                    </Button>
                  )}
                  {ready && !ios && build.install_url && (
                    <span className="flex items-center gap-0.5">
                      <ExternalAnchor href={build.install_url}>{t("operations.storeTest.installLink")}</ExternalAnchor>
                      <CopyButton value={build.install_url} label={t("operations.storeTest.copyLink")} />
                    </span>
                  )}
                  {build.run_url && (
                    <ExternalAnchor href={build.run_url}>{t("operations.storeTest.runLink")}</ExternalAnchor>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}

      <OpenToGroupsDialog
        repositoryId={repositoryId}
        build={openFor}
        groups={groups}
        groupsError={groupsError}
        open={openFor !== null}
        onOpenChange={(open) => !open && setOpenFor(null)}
        onOpened={(build) => merge([build])}
      />
      <ExportComplianceDialog
        repositoryId={repositoryId}
        build={complianceFor}
        open={complianceFor !== null}
        onOpenChange={(open) => !open && setComplianceFor(null)}
        onAnswered={(build) => merge([build])}
      />
      <ConfirmDialog
        open={removal !== null}
        onOpenChange={(open) => !open && setRemoval(null)}
        title={t("operations.storeTest.removeTitle", { group: removalName })}
        description={t("operations.storeTest.removeDescription", {
          group: removalName,
          build: removal?.build.build_number ?? "",
        })}
        confirmLabel={t("operations.storeTest.removeConfirm")}
        loading={removing}
        onConfirm={removeFromGroup}
      />
    </section>
  );
}
