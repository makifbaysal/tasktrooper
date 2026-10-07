import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type StoreTestBuild, type StoreTestGroup } from "@/api";
import { ActionSurface } from "@/components/operations/StoreTestBuildParts";
import { needsBetaReview, testBuildGroups, testGroupKindLabelKey } from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

interface OpenToGroupsDialogProps {
  repositoryId: string;
  build: StoreTestBuild | null;
  /** The platform's groups/tracks; null while they load. Owned by the caller, which also names chips with them. */
  groups: StoreTestGroup[] | null;
  groupsError?: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onOpened: (build: StoreTestBuild) => void;
  inline?: boolean;
}

/**
 * Opens a ready test build to more TestFlight groups or Play testing tracks.
 * What it is already open to is checked and locked: closing is a separate,
 * iOS-only action. An all_builds group is locked too — it gets every build.
 */
export function OpenToGroupsDialog({
  repositoryId,
  build,
  groups,
  groupsError,
  open,
  onOpenChange,
  onOpened,
  inline = false,
}: OpenToGroupsDialogProps) {
  const { t } = useI18n();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setSelected(new Set());
  }, [open, build?.id]);

  if (!build) return null;

  const current = new Set(testBuildGroups(build));
  const ios = build.platform === "ios";
  // storeops refuses a Play track release without the signed bundle on this machine.
  const missingArtifact = !ios && !build.has_artifact;
  const reviewPicked = (groups ?? []).some((g) => selected.has(g.id) && needsBetaReview(g));

  const toggle = (id: string, on: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  const submit = async () => {
    setBusy(true);
    try {
      const updated = await api.openStoreTestBuild(repositoryId, build.id, [...selected]);
      toast.success(t("operations.storeTest.opened"));
      onOpened(updated);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <ActionSurface
      inline={inline}
      open={open}
      onOpenChange={(next) => !busy && onOpenChange(next)}
      title={t("operations.storeTest.openDialogTitle", { build: build.build_number })}
      description={t(ios ? "operations.storeTest.openDialogDescriptionIos" : "operations.storeTest.openDialogDescriptionAndroid")}
      footer={
        <>
          <Button variant="outline" size={inline ? "sm" : "default"} onClick={() => onOpenChange(false)} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button
            size={inline ? "sm" : "default"}
            onClick={() => void submit()}
            disabled={busy || selected.size === 0 || missingArtifact}
          >
            {busy ? t("common.saving") : t("operations.storeTest.open")}
          </Button>
        </>
      }
    >
      {missingArtifact && (
        <Notice variant="warning" title={t("operations.storeTest.noArtifactTitle")}>
          {t("operations.storeTest.noArtifact")}
        </Notice>
      )}
      {groupsError && groups === null && (
        <Notice variant="warning" title={t("operations.storeTest.groupsFailed")}>
          {groupsError}
        </Notice>
      )}
      {groups === null && !groupsError && <Skeleton className="h-24 w-full" />}
      {groups !== null && groups.length === 0 && (
        <p className="text-sm text-muted-foreground">{t("operations.storeTest.noGroups")}</p>
      )}
      {groups !== null && groups.length > 0 && (
        <fieldset className="rounded-lg border border-border">
          <legend className="sr-only">{t("operations.storeTest.groupsLegend")}</legend>
          <div className="divide-y divide-border">
            {groups.map((group) => {
              const locked = current.has(group.id) || Boolean(group.all_builds);
              const checked = locked || selected.has(group.id);
              const inputId = `open-to-${build.id}-${group.id}`;
              return (
                <label
                  key={group.id}
                  htmlFor={inputId}
                  className={cn(
                    "flex items-start gap-3 px-3 py-2",
                    locked ? "cursor-default opacity-70" : "cursor-pointer hover:bg-muted/50",
                  )}
                >
                  <Checkbox
                    id={inputId}
                    className="mt-0.5"
                    checked={checked}
                    disabled={locked || busy || missingArtifact}
                    onCheckedChange={(value) => toggle(group.id, value === true)}
                  />
                  <span className="min-w-0 flex-1 space-y-1">
                    <span className="flex flex-wrap items-center gap-1.5">
                      <span className="truncate text-sm font-medium">{group.name}</span>
                      <Badge variant="outline" className="py-0 font-normal">
                        {t(testGroupKindLabelKey(group.kind))}
                      </Badge>
                      {needsBetaReview(group) && (
                        <Badge variant="warning" className="py-0">
                          {t("operations.storeTest.betaReview")}
                        </Badge>
                      )}
                    </span>
                    <span className="block text-xs text-muted-foreground">
                      {[
                        group.all_builds
                          ? t("operations.storeTest.allBuilds")
                          : current.has(group.id)
                            ? t("operations.storeTest.alreadyOpen")
                            : null,
                        group.tester_count >= 0 ? t("operations.storeTest.testerCount", { count: group.tester_count }) : null,
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </span>
                  </span>
                </label>
              );
            })}
          </div>
        </fieldset>
      )}
      {reviewPicked && (
        <Notice variant="info" title={t("operations.storeTest.betaReviewTitle")}>
          {t("operations.storeTest.betaReviewNotice")}
        </Notice>
      )}
    </ActionSurface>
  );
}
