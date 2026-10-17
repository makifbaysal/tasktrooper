import { Pencil, RefreshCw } from "lucide-react";
import { useState } from "react";
import type { CloudAccount, DataSource, DataSourceKindInfo } from "@/api";
import { inventoryQuery, kindLabel, sourceAddress } from "@/components/operations/sources/dataSourceSchema";
import { CapabilityChips, DataSourceStatusBadge } from "@/components/operations/sources/DataSourceStatusBadge";
import { DiscoveredMetrics } from "@/components/operations/sources/DiscoveredMetrics";
import { QueryPlayground } from "@/components/operations/sources/QueryPlayground";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Notice } from "@/components/ui/notice";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

interface DataSourceDetailDrawerProps {
  /** Looked up by the caller from its list on every render, so a verify or edit shows at once. */
  source: DataSource | null;
  kind?: DataSourceKindInfo;
  verifying: boolean;
  onOpenChange: (open: boolean) => void;
  onVerify: (source: DataSource) => void;
  onEdit: (source: DataSource) => void;
  cloudAccounts?: CloudAccount[];
}

const NO_ACCOUNTS: CloudAccount[] = [];

export function DataSourceDetailDrawer({
  source,
  kind,
  verifying,
  onOpenChange,
  onVerify,
  onEdit,
  cloudAccounts = NO_ACCOUNTS,
}: DataSourceDetailDrawerProps) {
  return (
    <Dialog open={source !== null} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[85vh] max-w-3xl flex-col gap-0 overflow-hidden p-0">
        {source && (
          <DrawerBody
            key={source.id}
            source={source}
            kind={kind}
            cloudAccounts={cloudAccounts}
            verifying={verifying}
            onVerify={() => onVerify(source)}
            onEdit={() => onEdit(source)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

interface DrawerBodyProps {
  source: DataSource;
  kind?: DataSourceKindInfo;
  cloudAccounts: CloudAccount[];
  verifying: boolean;
  onVerify: () => void;
  onEdit: () => void;
}

function DrawerBody({ source, kind, cloudAccounts, verifying, onVerify, onEdit }: DrawerBodyProps) {
  const { t, lang } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.sources.${key}`, params);
  const [query, setQuery] = useState("");
  const address = sourceAddress(source, kind, cloudAccounts);
  const metrics = (source.capabilities ?? []).includes("metrics");
  const kindName = kind ? kindLabel(t, kind) : s("unknownKind", { kind: source.kind });
  const version = source.info?.version;
  const infoDetail = source.info?.detail;

  return (
    <>
      <DialogHeader className="border-b border-border px-6 py-4">
        <div className="flex flex-wrap items-center gap-2 pr-6">
          <DialogTitle className="text-left">{source.name}</DialogTitle>
          <DataSourceStatusBadge status={source.status} />
        </div>
        <DialogDescription className="text-left">
          {kindName}
          {address && (
            <>
              <span aria-hidden> · </span>
              <span className="font-mono">{address}</span>
            </>
          )}
        </DialogDescription>
      </DialogHeader>

      <ScrollArea className="min-h-0 flex-1">
        <div className="space-y-6 px-6 py-5">
          <section className="space-y-3" aria-label={s("detail.status")}>
            {source.status === "error" && (
              <Notice variant="error" title={s("detail.errorTitle")}>
                {source.status_detail && <p className="break-words font-mono text-caption">{source.status_detail}</p>}
              </Notice>
            )}
            <dl className="grid gap-x-6 gap-y-2 text-body sm:grid-cols-[auto_1fr]">
              <dt className="text-caption text-muted-foreground">{s("detail.provides")}</dt>
              <dd>
                <CapabilityChips capabilities={source.capabilities ?? []} />
              </dd>
              {version && (
                <>
                  <dt className="text-caption text-muted-foreground">{s("detail.version")}</dt>
                  <dd className="font-mono text-caption">{version}</dd>
                </>
              )}
              {infoDetail && (
                <>
                  <dt className="text-caption text-muted-foreground">{s("detail.info")}</dt>
                  <dd className="break-words text-caption">{infoDetail}</dd>
                </>
              )}
              <dt className="text-caption text-muted-foreground">{s("detail.lastChecked")}</dt>
              <dd className="text-caption">
                {source.verified_at ? (
                  <span title={formatDate(source.verified_at)}>{formatRelativeTime(source.verified_at, lang)}</span>
                ) : (
                  s("neverVerified")
                )}
              </dd>
            </dl>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={onVerify} disabled={verifying}>
                <RefreshCw className={cn("h-3 w-3", verifying && "animate-spin")} aria-hidden />
                {verifying ? s("verifying") : s("verify")}
              </Button>
              <Button variant="outline" size="sm" onClick={onEdit}>
                <Pencil className="h-3 w-3" aria-hidden />
                {s("edit")}
              </Button>
            </div>
          </section>

          {metrics && (
            <>
              <Separator />
              <QueryPlayground sourceId={source.id} kind={source.kind} query={query} onQueryChange={setQuery} />
            </>
          )}

          <Separator />
          <DiscoveredMetrics
            sourceId={source.id}
            onPickMetric={
              metrics
                ? (name) => {
                    const picked = inventoryQuery(source.kind, name);
                    if (picked !== null) setQuery(picked);
                  }
                : undefined
            }
            canPick={(name) => inventoryQuery(source.kind, name) !== null}
          />
        </div>
      </ScrollArea>
    </>
  );
}
