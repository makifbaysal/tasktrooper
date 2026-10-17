import { ExternalAnchor } from "@/components/operations/StoreTestBuildParts";
import { TABLE_WINDOWS_DOCS_URL, type TableWindow } from "@/components/operations/tables/componentTables";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";

const INTL_UNIT: Record<TableWindow["unit"], string> = { minutes: "minute", hours: "hour", days: "day" };

/** What span of time a recipe table covers: "Covers the last 7 days", or,
 * when that cannot be told, the source's own recent window with a link to
 * the docs that list each source's. */
export function TableWindowNote({ window }: { window?: TableWindow }) {
  const { t, lang } = useI18n();
  if (!window) {
    return (
      <span>
        {t("operations.telemetry.table.windowUnknown")}{" "}
        <ExternalAnchor href={TABLE_WINDOWS_DOCS_URL} className="text-info hover:underline">
          {t("operations.telemetry.table.windowDocs")}
        </ExternalAnchor>
      </span>
    );
  }
  const span = new Intl.NumberFormat(intlLocale(lang), { style: "unit", unit: INTL_UNIT[window.unit], unitDisplay: "long" }).format(window.amount);
  return <span>{t("operations.telemetry.table.window", { span })}</span>;
}
