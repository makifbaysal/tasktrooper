import { ChevronDown, ChevronRight, Hammer, Loader2, RotateCw, Users } from "lucide-react";
import { useState } from "react";
import type { MobileStoreApp, StoreTestBuild } from "@/api";
import { ExportComplianceDialog } from "@/components/operations/ExportComplianceDialog";
import { OpenToGroupsDialog } from "@/components/operations/OpenToGroupsDialog";
import { hasStoreChannels } from "@/components/operations/StoreReleaseControls";
import {
  CopyButton,
  ExternalAnchor,
  LogTail,
  StorePlatformIcon,
  TestBuildGroupChips,
  TestBuildStatusBadge,
} from "@/components/operations/StoreTestBuildParts";
import {
  isTestBuildActionable,
  needsExportCompliance,
  storePlatformLabelKey,
  testBuildGroups,
  testBuildLabel,
} from "@/components/operations/storeTestBuilds";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import { useStoreTestGroups } from "@/hooks/useStoreTestGroups";
import { formatDate, formatRelativeTime } from "@/lib/utils";

interface TaskPlatformTestBuildProps {
  repositoryId: string;
  app: MobileStoreApp;
  latest: StoreTestBuild | null;
  older: StoreTestBuild[];
  /** False while the panel header already offers the one "Build for test" for every platform. */
  offerFirstBuild: boolean;
  starting: boolean;
  onBuild: () => void;
  onChanged: (build: StoreTestBuild) => void;
}

/** One platform's latest test build of a task, with what a reviewer needs to install and share it. */
export function TaskPlatformTestBuild({
  repositoryId,
  app,
  latest,
  older,
  offerFirstBuild,
  starting,
  onBuild,
  onChanged,
}: TaskPlatformTestBuildProps) {
  const { t, lang } = useI18n();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [complianceOpen, setComplianceOpen] = useState(false);
  const platform = app.platform;
  // Mirrors storeops.testableApp: the server refuses a build for anything else.
  const testable = hasStoreChannels(app);
  const ios = platform === "ios";
  const groupIds = latest ? testBuildGroups(latest) : [];
  const ready = latest?.status === "ready";
  // Android chips read without a lookup (a track's id is its name); iOS ids need the group list.
  const { groups, error: groupsError } = useStoreTestGroups(
    repositoryId,
    platform,
    pickerOpen || (ios && ready && groupIds.length > 0),
  );

  const heading = (
    <div className="flex flex-wrap items-center gap-2">
      <StorePlatformIcon platform={platform} />
      <span className="text-sm font-medium">{t(storePlatformLabelKey(platform))}</span>
      {latest && (
        <>
          <TestBuildStatusBadge status={latest.status} />
          <span className="font-mono text-xs tabular-nums">{latest.build_number}</span>
          <span className="text-xs text-muted-foreground">{testBuildLabel(latest)}</span>
          <span className="ml-auto text-xs text-muted-foreground" title={formatDate(latest.created_at)}>
            {formatRelativeTime(latest.created_at, lang)}
          </span>
        </>
      )}
    </div>
  );

  if (!latest) {
    return (
      <div className="space-y-2 py-2.5 first:pt-0 last:pb-0">
        {heading}
        {testable ? (
          <div className="flex flex-wrap items-center gap-2">
            <p className="text-xs text-muted-foreground">{t("operations.storeTest.noBuildYet")}</p>
            {offerFirstBuild && (
              <Button size="sm" variant="outline" disabled={starting} onClick={onBuild}>
                {starting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Hammer className="h-3.5 w-3.5" />}
                {t("operations.storeTest.buildForTest")}
              </Button>
            )}
          </div>
        ) : (
          <p className="text-xs text-muted-foreground">{t("operations.storeTest.notTestable")}</p>
        )}
      </div>
    );
  }

  const actionable = isTestBuildActionable(latest.status);
  const compliance = actionable && needsExportCompliance(latest);

  return (
    <div className="space-y-2 py-2.5 first:pt-0 last:pb-0">
      {heading}

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {latest.version_name && <span>{t("operations.storeTest.version", { version: latest.version_name })}</span>}
        {latest.engine && <span>{t(`projectAdmin.mobileStore.engines.${latest.engine}`)}</span>}
        {latest.run_url && <ExternalAnchor href={latest.run_url}>{t("operations.storeTest.runLink")}</ExternalAnchor>}
      </div>

      {ready && ios && <p className="text-xs text-muted-foreground">{t("operations.storeTest.iosHint")}</p>}
      {ready && !ios && latest.install_url && (
        <div className="space-y-0.5">
          <div className="flex min-w-0 items-center gap-1">
            <ExternalAnchor href={latest.install_url} className="min-w-0 truncate">
              {t("operations.storeTest.installLink")}
            </ExternalAnchor>
            <CopyButton value={latest.install_url} label={t("operations.storeTest.copyLink")} />
          </div>
          <p className="text-xs text-muted-foreground">{t("operations.storeTest.androidHint")}</p>
        </div>
      )}
      {(ready || groupIds.length > 0) && actionable && <TestBuildGroupChips ids={groupIds} groups={groups} />}

      {latest.status === "failed" && latest.failure && (
        <p className="whitespace-pre-line text-xs text-destructive">{latest.failure}</p>
      )}
      {latest.status === "failed" && <LogTail text={latest.log_tail} />}

      {compliance && !complianceOpen && (
        <Notice variant="warning" title={t("operations.storeTest.complianceTitle")}>
          <p>{t("operations.storeTest.complianceBody")}</p>
          <Button size="sm" className="mt-2" onClick={() => setComplianceOpen(true)}>
            {t("operations.storeTest.complianceAnswer")}
          </Button>
        </Notice>
      )}
      <ExportComplianceDialog
        inline
        repositoryId={repositoryId}
        build={compliance ? latest : null}
        open={complianceOpen}
        onOpenChange={setComplianceOpen}
        onAnswered={onChanged}
      />

      {!pickerOpen && (
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" disabled={starting || !testable} onClick={onBuild}>
            {starting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RotateCw className="h-3.5 w-3.5" />}
            {t("operations.storeTest.buildAgain")}
          </Button>
          {ready && (
            <Button size="sm" variant="outline" onClick={() => setPickerOpen(true)}>
              <Users className="h-3.5 w-3.5" />
              {t("operations.storeTest.openTo")}
            </Button>
          )}
        </div>
      )}
      <OpenToGroupsDialog
        inline
        repositoryId={repositoryId}
        build={ready ? latest : null}
        groups={groups}
        groupsError={groupsError}
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onOpened={onChanged}
      />

      {older.length > 0 && <OlderAttempts builds={older} />}
    </div>
  );
}

function OlderAttempts({ builds }: { builds: StoreTestBuild[] }) {
  const { t, lang } = useI18n();
  const [open, setOpen] = useState(false);
  return (
    <div className="space-y-1">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        className="h-6 px-1.5 text-xs text-muted-foreground"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        {t("operations.storeTest.olderAttempts", { count: builds.length })}
      </Button>
      {open && (
        <ul className="space-y-1 pl-2">
          {builds.map((build) => (
            <li key={build.id} className="flex flex-wrap items-center gap-2 text-xs">
              <span className="font-mono tabular-nums">{build.build_number}</span>
              <span className="text-muted-foreground">{testBuildLabel(build)}</span>
              <TestBuildStatusBadge status={build.status} />
              {build.run_url && <ExternalAnchor href={build.run_url}>{t("operations.storeTest.runLink")}</ExternalAnchor>}
              <span className="ml-auto text-muted-foreground" title={formatDate(build.created_at)}>
                {formatRelativeTime(build.created_at, lang)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
