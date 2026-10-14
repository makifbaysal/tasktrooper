import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ToolchainInventoryCard } from "@/components/toolchain/ToolchainInventoryCard";
import { ToolchainJobCard } from "@/components/toolchain/ToolchainJobCard";
import { ToolchainRecommendedCard } from "@/components/toolchain/ToolchainRecommendedCard";
import { Notice } from "@/components/ui/notice";
import { useEnabledAgentSlugs } from "@/hooks/useEnabledAgentSlugs";
import { useI18n } from "@/hooks/useI18n";
import { useToolchain } from "@/hooks/useToolchain";
import type { ToolchainId } from "@/lib/desktop-bridge";
import { recommendFor } from "@/lib/toolchain";
import { isDeclined } from "@/lib/toolchainJob";

/**
 * Settings → Development environment: the current or last install job, what
 * the enabled agents still lack, and every tool this OS can have. It acts on
 * this computer through the desktop shell, so a browser gets the sentence.
 */
export function EnvironmentSettingsPage() {
  const { t } = useI18n();
  const toolchain = useToolchain();
  const team = useEnabledAgentSlugs();
  const [installing, setInstalling] = useState(false);

  const recommendations = useMemo(() => {
    if (!toolchain.report || (!team.loaded && !team.error) || team.preparing) return null;
    return recommendFor(team.slugs, toolchain.report).filter((entry) => entry.level === "recommended");
  }, [toolchain.report, team.loaded, team.error, team.preparing, team.slugs]);

  if (!toolchain.available) {
    return (
      <Notice variant="info" title={t("toolchain.settings.desktopOnlyTitle")}>
        {t("toolchain.settings.desktopOnlyBody")}
      </Notice>
    );
  }

  const install = async (ids: ToolchainId[]) => {
    if (ids.length === 0) return;
    setInstalling(true);
    // While a job runs the shell appends to it (a failed or stopped id is
    // queued again), so the person hears where it went rather than nothing.
    const appending = toolchain.state?.phase === "running";
    try {
      await toolchain.install(ids);
      if (appending) toast.success(t("toolchain.queuedAfterCurrent"));
    } catch (e) {
      if (isDeclined(e)) toast.info(t("toolchain.errors.declined"));
      else toast.error(t("toolchain.errors.installFailed"), { description: e instanceof Error ? e.message : undefined });
    } finally {
      setInstalling(false);
    }
  };

  const openPage = (id: ToolchainId) => {
    toolchain.openPage(id).catch((e: unknown) => {
      toast.error(t("toolchain.errors.openPageFailed"), { description: e instanceof Error ? e.message : undefined });
    });
  };

  const { state } = toolchain;

  return (
    <div className="space-y-4">
      {state && state.phase !== "idle" && (
        <ToolchainJobCard state={state} onStop={toolchain.cancel} onDismiss={toolchain.dismiss} />
      )}
      <ToolchainRecommendedCard
        recommendations={recommendations}
        names={team.names}
        state={state}
        installing={installing}
        onInstall={(ids) => void install(ids)}
        onOpenPage={openPage}
      />
      <ToolchainInventoryCard
        report={toolchain.report}
        reportError={toolchain.reportError}
        state={state}
        refreshing={toolchain.refreshing}
        installing={installing}
        onRefresh={() => void toolchain.refresh(true)}
        onInstall={(ids) => void install(ids)}
        onOpenPage={openPage}
      />
    </div>
  );
}
