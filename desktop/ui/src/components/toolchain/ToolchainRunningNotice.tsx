import { ToolchainProgress } from "@/components/toolchain/ToolchainProgress";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainJobState } from "@/lib/desktop-bridge";

/** A compact "still installing" callout, for screens whose checks the running job may yet turn green. */
export function ToolchainRunningNotice({ state }: { state: ToolchainJobState | null }) {
  const { t } = useI18n();
  if (!state || state.phase !== "running") return null;
  return (
    <Notice variant="info" title={t("toolchain.running.title")}>
      <p>{t("toolchain.running.body")}</p>
      <ToolchainProgress state={state} className="mt-2" />
    </Notice>
  );
}
