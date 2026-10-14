import { Download, RefreshCw, Wrench } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { ToolchainItemRow } from "@/components/toolchain/ToolchainItemRow";
import { ToolchainOpenPageButton } from "@/components/toolchain/ToolchainOpenPageButton";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainId, ToolchainItem, ToolchainJobState, ToolchainReport } from "@/lib/desktop-bridge";
import { groupReport, manualBlockers } from "@/lib/toolchain";
import { idsInFlight, jobStepFor, stepSatisfied } from "@/lib/toolchainJob";
import { cn, formatRelativeTime } from "@/lib/utils";

interface ToolchainInventoryCardProps {
  report: ToolchainReport | null;
  reportError: string;
  state: ToolchainJobState | null;
  refreshing: boolean;
  installing: boolean;
  onRefresh: () => void;
  onInstall: (ids: ToolchainId[]) => void;
  onOpenPage: (id: ToolchainId) => void;
}

/** Every tool this OS can have, grouped, each with what was detected and how to get it. */
export function ToolchainInventoryCard({
  report,
  reportError,
  state,
  refreshing,
  installing,
  onRefresh,
  onInstall,
  onOpenPage,
}: ToolchainInventoryCardProps) {
  const { t, lang } = useI18n();
  const inFlight = idsInFlight(state);

  const blockersOf = (item: ToolchainItem) =>
    report && item.status !== "installed" && item.installKind !== "manual" ? manualBlockers(item, report) : [];

  const action = (item: ToolchainItem, blocked: boolean) => {
    if (item.status === "installed") return undefined;
    const step = jobStepFor(state, item.id);
    if (step && stepSatisfied(step)) return undefined;
    if (item.installKind === "manual") return <ToolchainOpenPageButton item={item} onOpenPage={onOpenPage} />;
    return (
      <Button
        variant="outline"
        size="sm"
        disabled={installing || blocked || inFlight.has(item.id)}
        onClick={() => onInstall([item.id])}
        aria-label={`${t("toolchain.actions.install")} ${item.label}`}
      >
        <Download className="h-4 w-4" />
        {t("toolchain.actions.install")}
      </Button>
    );
  };

  return (
    <Card className="w-full space-y-4 p-6" data-testid="toolchain-inventory-card">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <Label className="flex items-center gap-2">
            <Wrench className="h-4 w-4" />
            {t("toolchain.settings.allTitle")}
          </Label>
          <p className="text-sm text-muted-foreground">
            {t("toolchain.settings.allDescription")}
            {report && (
              <>
                {" "}
                {t("toolchain.settings.lastChecked", {
                  when: formatRelativeTime(new Date(report.generatedAt).toISOString(), lang),
                })}
              </>
            )}
          </p>
        </div>
        <Button variant="outline" disabled={refreshing} onClick={onRefresh}>
          <RefreshCw className={cn("h-4 w-4", refreshing && "animate-spin")} />
          {t("toolchain.actions.checkAgain")}
        </Button>
      </div>

      {reportError && (
        <Notice variant="error" title={t("toolchain.errors.reportFailed")}>
          {reportError}
        </Notice>
      )}

      {!report ? (
        !reportError && (
          <div className="space-y-2" aria-busy="true">
            <p className="text-sm text-muted-foreground">{t("toolchain.settings.checking")}</p>
            <Skeleton className="h-40 w-full" />
          </div>
        )
      ) : (
        groupReport(report).map((group) => (
          <section key={group.id} className="space-y-2" aria-labelledby={`toolchain-group-${group.id}`}>
            <h3
              id={`toolchain-group-${group.id}`}
              className="text-xs font-medium uppercase tracking-wide text-muted-foreground"
            >
              {t(`toolchain.settings.groups.${group.id}`)}
            </h3>
            <div className="divide-y divide-border rounded-lg border border-border">
              {group.items.map((item) => {
                const blocker = blockersOf(item)[0];
                return (
                  <ToolchainItemRow
                    key={item.id}
                    item={item}
                    step={jobStepFor(state, item.id)}
                    hint={blocker ? t("toolchain.needsManual", { label: blocker.label }) : undefined}
                    showStatus
                    showCommand
                    action={action(item, blocker !== undefined)}
                  />
                );
              })}
            </div>
          </section>
        ))
      )}
    </Card>
  );
}
