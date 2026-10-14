import { Download, Users } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ToolchainItemRow } from "@/components/toolchain/ToolchainItemRow";
import { ToolchainManualAction } from "@/components/toolchain/ToolchainOpenPageButton";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainId, ToolchainJobState } from "@/lib/desktop-bridge";
import { toolchainReason, type ToolchainRecommendation } from "@/lib/toolchain";
import { idsInFlight, jobStepFor } from "@/lib/toolchainJob";

interface ToolchainRecommendedCardProps {
  /** The team's missing tools at the recommended level; null while the report or the roster loads. */
  recommendations: readonly ToolchainRecommendation[] | null;
  names: Readonly<Record<string, string>>;
  state: ToolchainJobState | null;
  installing: boolean;
  onInstall: (ids: ToolchainId[]) => void;
  onOpenPage: (id: ToolchainId) => void;
}

/** "Recommended for your team": what the enabled agents lack, installable in one click. */
export function ToolchainRecommendedCard({
  recommendations,
  names,
  state,
  installing,
  onInstall,
  onOpenPage,
}: ToolchainRecommendedCardProps) {
  const { t } = useI18n();
  const inFlight = idsInFlight(state);
  const installable = (recommendations ?? [])
    .filter((entry) => entry.preselected && !inFlight.has(entry.id))
    .map((entry) => entry.id);
  const anyManual = (recommendations ?? []).some((entry) => !entry.installable);

  return (
    <Card className="w-full space-y-4 p-6" data-testid="toolchain-recommended-card">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <Label className="flex items-center gap-2">
            <Users className="h-4 w-4" />
            {t("toolchain.settings.recommendedTitle")}
          </Label>
          <p className="text-sm text-muted-foreground">{t("toolchain.settings.recommendedDescription")}</p>
        </div>
        {recommendations && recommendations.length > 0 && (
          <Button disabled={installing || installable.length === 0} onClick={() => onInstall(installable)}>
            <Download className="h-4 w-4" />
            {t("toolchain.actions.installRecommended")}
          </Button>
        )}
      </div>

      {recommendations === null ? (
        <Skeleton className="h-24 w-full" />
      ) : recommendations.length === 0 ? (
        <p className="text-sm">{t("toolchain.settings.recommendedNone")}</p>
      ) : (
        <>
          <div className="divide-y divide-border rounded-lg border border-border">
            {recommendations.map((entry) => (
              <ToolchainItemRow
                key={entry.id}
                item={entry.item}
                reason={toolchainReason(t, entry.reasons, names)}
                step={jobStepFor(state, entry.id)}
                hint={entry.blockedBy[0] ? t("toolchain.needsManual", { label: entry.blockedBy[0].label }) : undefined}
                showStatus
                action={entry.installable ? undefined : <ToolchainManualAction entry={entry} onOpenPage={onOpenPage} />}
              />
            ))}
          </div>
          {anyManual && <p className="text-xs text-muted-foreground">{t("toolchain.settings.recommendedManual")}</p>}
        </>
      )}
    </Card>
  );
}
