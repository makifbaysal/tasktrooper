import { Notice } from "@/components/ui/notice";
import { portChangesOnRestart, serverHost } from "@/components/operations/alerts/alertConnections";
import { useI18n } from "@/hooks/useI18n";

/** Says plainly that a loopback server is out of an external alerter's reach. */
export function LocalOnlyNotice({ base, className }: { base: string; className?: string }) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertConnections.localOnly.${key}`, params);
  return (
    <Notice variant="warning" title={a("title")} className={className}>
      <p>{a("body", { host: serverHost(base) ?? base })}</p>
      {portChangesOnRestart() && <p className="mt-1">{a("port")}</p>}
    </Notice>
  );
}
