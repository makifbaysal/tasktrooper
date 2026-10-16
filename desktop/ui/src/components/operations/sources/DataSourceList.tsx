import { Database, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import type { CloudAccount, DataSource, DataSourceKindInfo } from "@/api";
import { kindLabel, sourceAddress } from "@/components/operations/sources/dataSourceSchema";
import { CapabilityChips, DataSourceStatusBadge } from "@/components/operations/sources/DataSourceStatusBadge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate, formatRelativeTime } from "@/lib/utils";

interface DataSourceListProps {
  sources: DataSource[];
  kinds: DataSourceKindInfo[];
  /** Ids whose check is running; several can run at once. */
  verifying: ReadonlySet<string>;
  onAdd?: () => void;
  onOpen: (source: DataSource) => void;
  onVerify: (source: DataSource) => void;
  onEdit: (source: DataSource) => void;
  onDelete: (source: DataSource) => void;
  /** Labels the account a cloud kind reads through. */
  cloudAccounts?: CloudAccount[];
}

const NO_ACCOUNTS: CloudAccount[] = [];

export function DataSourceList({
  sources,
  kinds,
  cloudAccounts = NO_ACCOUNTS,
  verifying,
  onAdd,
  onOpen,
  onVerify,
  onEdit,
  onDelete,
}: DataSourceListProps) {
  const { t } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.sources.${key}`, params);

  if (sources.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Database}
          title={s("empty.title")}
          description={s("empty.description")}
          action={
            onAdd && (
              <Button onClick={onAdd}>
                <Plus aria-hidden />
                {s("add")}
              </Button>
            )
          }
        />
      </Card>
    );
  }

  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[760px] text-body" aria-label={s("listLabel")}>
          <thead>
            <tr className="border-b border-border text-left text-caption text-muted-foreground">
              <th scope="col" className="px-4 py-2.5 font-medium">{s("columns.name")}</th>
              <th scope="col" className="px-4 py-2.5 font-medium">{s("columns.kind")}</th>
              <th scope="col" className="px-4 py-2.5 font-medium">{s("columns.capabilities")}</th>
              <th scope="col" className="px-4 py-2.5 font-medium">{s("columns.status")}</th>
              <th scope="col" className="px-4 py-2.5 text-right font-medium">
                <span className="sr-only">{s("columns.actions")}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {sources.map((source) => (
              <DataSourceRow
                key={source.id}
                source={source}
                kind={kinds.find((k) => k.kind === source.kind)}
                cloudAccounts={cloudAccounts}
                verifying={verifying.has(source.id)}
                onOpen={() => onOpen(source)}
                onVerify={() => onVerify(source)}
                onEdit={() => onEdit(source)}
                onDelete={() => onDelete(source)}
              />
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  );
}

interface DataSourceRowProps {
  source: DataSource;
  kind?: DataSourceKindInfo;
  cloudAccounts: CloudAccount[];
  verifying: boolean;
  onOpen: () => void;
  onVerify: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

function DataSourceRow({ source, kind, cloudAccounts, verifying, onOpen, onVerify, onEdit, onDelete }: DataSourceRowProps) {
  const { t, lang } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.sources.${key}`, params);
  const address = sourceAddress(source, kind, cloudAccounts);
  const version = source.info?.version;

  return (
    <tr
      className="cursor-pointer border-b border-border/50 align-top last:border-0 hover:bg-muted/40"
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("a, button")) return;
        onOpen();
      }}
    >
      <td className="max-w-80 px-4 py-3">
        <Button
          variant="link"
          className="h-auto p-0 text-body font-medium text-foreground"
          onClick={onOpen}
          aria-label={s("openLabel", { name: source.name })}
        >
          {source.name}
        </Button>
        {address && <p className="truncate font-mono text-caption text-muted-foreground" title={address}>{address}</p>}
      </td>
      <td className="whitespace-nowrap px-4 py-3">
        {kind ? kindLabel(t, kind) : s("unknownKind", { kind: source.kind })}
      </td>
      <td className="px-4 py-3">
        <CapabilityChips capabilities={source.capabilities ?? []} />
      </td>
      <td className="px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <DataSourceStatusBadge status={source.status} />
          {source.status === "ok" && version && (
            <span className="text-caption text-muted-foreground">{s("version", { version })}</span>
          )}
        </div>
        {source.status === "error" && source.status_detail && (
          <p className="mt-1 line-clamp-2 max-w-72 break-words text-caption text-destructive" title={source.status_detail}>
            {source.status_detail}
          </p>
        )}
        {source.verified_at && (
          <p className="mt-0.5 text-caption text-muted-foreground" title={formatDate(source.verified_at)}>
            {s("verifiedAt", { time: formatRelativeTime(source.verified_at, lang) })}
          </p>
        )}
      </td>
      <td className="whitespace-nowrap px-4 py-3 text-right">
        <div className="flex items-center justify-end gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={onVerify}
            disabled={verifying}
            aria-label={verifying ? s("verifyingLabel", { name: source.name }) : s("verifyLabel", { name: source.name })}
          >
            <RefreshCw className={cn("h-3 w-3", verifying && "animate-spin")} aria-hidden />
            {verifying ? s("verifying") : s("verify")}
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8"
            onClick={onEdit}
            aria-label={s("editLabel", { name: source.name })}
            title={s("edit")}
          >
            <Pencil aria-hidden />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 text-destructive hover:text-destructive"
            onClick={onDelete}
            aria-label={s("deleteLabel", { name: source.name })}
            title={s("delete")}
          >
            <Trash2 aria-hidden />
          </Button>
        </div>
      </td>
    </tr>
  );
}
