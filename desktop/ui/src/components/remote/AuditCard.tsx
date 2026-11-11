import { RefreshCw, ScrollText } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { api, type RemoteAuditEntry } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";

export function AuditCard({ refreshKey }: { refreshKey: number }) {
  const { t } = useI18n();
  const [entries, setEntries] = useState<RemoteAuditEntry[] | null>(null);
  const [failed, setFailed] = useState(false);

  const load = useCallback(async () => {
    try {
      setEntries((await api.remoteAccessAudit(100)).entries);
      setFailed(false);
    } catch {
      setFailed(true);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load, refreshKey]);

  return (
    <Card className="w-full space-y-4 p-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 text-heading font-semibold">
            <ScrollText className="h-4 w-4" />
            {t("remoteAccess.audit.title")}
          </h2>
          <p className="text-caption text-muted-foreground">{t("remoteAccess.audit.description")}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load()}>
          <RefreshCw className="mr-2 h-4 w-4" />
          {t("remoteAccess.audit.refresh")}
        </Button>
      </div>
      {failed && <p className="text-sm text-destructive">{t("remoteAccess.audit.loadFailed")}</p>}
      {entries && entries.length === 0 && !failed && <p className="text-sm text-muted-foreground">{t("remoteAccess.audit.empty")}</p>}
      {entries && entries.length > 0 && (
        <div className="max-h-80 overflow-auto rounded-lg border">
          <table className="w-full text-left text-xs">
            <thead className="sticky top-0 bg-muted">
              <tr>
                <th className="px-3 py-2 font-medium">{t("remoteAccess.audit.when")}</th>
                <th className="px-3 py-2 font-medium">{t("remoteAccess.audit.phone")}</th>
                <th className="px-3 py-2 font-medium">{t("remoteAccess.audit.what")}</th>
                <th className="px-3 py-2 font-medium">{t("remoteAccess.audit.result")}</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => (
                <tr key={entry.id} className="border-t" data-testid="audit-row">
                  <td className="whitespace-nowrap px-3 py-2">{new Date(entry.at).toLocaleString()}</td>
                  <td className="px-3 py-2">{entry.device_name || t("remoteAccess.audit.unknownPhone")}</td>
                  <td className="px-3 py-2">
                    {entry.kind === "request" && entry.method && entry.route ? (
                      <code>
                        {entry.method} {entry.route}
                      </code>
                    ) : (
                      t(`remoteAccess.audit.kinds.${entry.kind}`)
                    )}
                  </td>
                  <td className="px-3 py-2">{entry.status ?? ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}
