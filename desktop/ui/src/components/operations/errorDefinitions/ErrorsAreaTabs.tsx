import { Bug, BookMarked } from "lucide-react";
import { ERROR_DEFINITIONS_PATH } from "@/components/operations/errorDefinitions/errorDefinitions";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { NavStrip } from "@/components/operations/shell/NavStrip";
import { useI18n } from "@/hooks/useI18n";

export const ERRORS_INBOX_PATH = "/operations/errors";

/** The Errors area's two views, the inbox and its definitions, as a row of
 * links above either: one nav entry in the shell, two pages under it. */
export function ErrorsAreaTabs({ active }: { active: "inbox" | "definitions" }) {
  const { t } = useI18n();
  const href = useOpsHref();
  const a = (key: string) => t(`operations.errorDefinitions.areaTabs.${key}`);
  return (
    <NavStrip
      label={a("label")}
      className="-mt-2"
      groups={[
        [
          { key: "inbox", to: href(ERRORS_INBOX_PATH), label: a("inbox"), icon: Bug, active: active === "inbox" },
          { key: "definitions", to: href(ERROR_DEFINITIONS_PATH), label: a("definitions"), icon: BookMarked, active: active === "definitions" },
        ],
      ]}
    />
  );
}
