import { ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import type { ToolchainId, ToolchainItem } from "@/lib/desktop-bridge";
import type { ToolchainRecommendation } from "@/lib/toolchain";

interface ToolchainOpenPageButtonProps {
  item: ToolchainItem;
  /** The item is another row's dependency, so the button names it. */
  dependency?: boolean;
  onOpenPage: (id: ToolchainId) => void;
}

/** Opens a manual tool's own page — the item itself, or the manual tool another item waits on. */
export function ToolchainOpenPageButton({ item, dependency = false, onOpenPage }: ToolchainOpenPageButtonProps) {
  const { t } = useI18n();
  return (
    <Button variant="outline" size="sm" onClick={() => onOpenPage(item.id)}>
      <ExternalLink className="h-4 w-4" />
      {dependency ? t("toolchain.actions.openPageFor", { label: item.label }) : t("toolchain.actions.openPage")}
    </Button>
  );
}

/**
 * What a recommendation the shell cannot install offers instead: its own page
 * when it is manual, or the page of the manual tool it waits on. Null for an
 * installable one.
 */
export function ToolchainManualAction({
  entry,
  onOpenPage,
}: {
  entry: ToolchainRecommendation;
  onOpenPage: (id: ToolchainId) => void;
}) {
  if (entry.item.installKind === "manual") return <ToolchainOpenPageButton item={entry.item} onOpenPage={onOpenPage} />;
  const blocker = entry.blockedBy[0];
  if (blocker) return <ToolchainOpenPageButton item={blocker} dependency onOpenPage={onOpenPage} />;
  return null;
}
