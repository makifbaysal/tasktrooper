import { Plus, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import type { AlertToken } from "@/api";
import { isAlertEnv } from "@/components/operations/alerts/alertConnections";
import type { OpsRepository } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { useI18n } from "@/hooks/useI18n";
import { formatDate, formatRelativeTime } from "@/lib/utils";

export interface AlertTokenGroup {
  repository: OpsRepository;
  tokens: AlertToken[];
  error?: string;
}

interface AlertTokenTableProps {
  groups: AlertTokenGroup[];
  onAdd: (repositoryId: string) => void;
  onDelete: (repository: OpsRepository, token: AlertToken) => void;
}

const COLUMNS = 5;

/** Every repository's alert connections: one `tbody` per repository, its
 * name and an add button heading its rows. */
export function AlertTokenTable({ groups, onAdd, onDelete }: AlertTokenTableProps) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertConnections.${key}`, params);

  return (
    <Card className="overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[680px] text-body" aria-label={a("listLabel")}>
          <thead>
            <tr className="border-b border-border text-left text-caption text-muted-foreground">
              <th scope="col" className="px-4 py-2.5 font-medium">{a("columns.label")}</th>
              <th scope="col" className="px-4 py-2.5 font-medium">{a("columns.env")}</th>
              <th scope="col" className="px-4 py-2.5 font-medium">{a("columns.created")}</th>
              <th scope="col" className="px-4 py-2.5 font-medium">{a("columns.lastUsed")}</th>
              <th scope="col" className="px-4 py-2.5 text-right font-medium">
                <span className="sr-only">{a("columns.actions")}</span>
              </th>
            </tr>
          </thead>
          {groups.map((group) => (
            <RepositoryRows key={group.repository.id} group={group} onAdd={onAdd} onDelete={onDelete} />
          ))}
        </table>
      </div>
    </Card>
  );
}

interface RepositoryRowsProps {
  group: AlertTokenGroup;
  onAdd: (repositoryId: string) => void;
  onDelete: (repository: OpsRepository, token: AlertToken) => void;
}

function RepositoryRows({ group, onAdd, onDelete }: RepositoryRowsProps) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertConnections.${key}`, params);
  const { repository, tokens, error } = group;

  return (
    <tbody aria-label={repository.name} className="border-b border-border last:border-0">
      <tr className="bg-muted/30">
        <th scope="rowgroup" colSpan={COLUMNS - 1} className="px-4 py-1.5 text-left text-caption font-medium">
          <Link to={`/repositories/${repository.id}`} className="text-foreground underline-offset-2 hover:underline">
            {repository.name}
          </Link>
        </th>
        <td className="px-4 py-1 text-right">
          <Button
            variant="ghost"
            size="sm"
            className="h-7"
            onClick={() => onAdd(repository.id)}
            aria-label={a("addFor", { repository: repository.name })}
          >
            <Plus className="h-3.5 w-3.5" aria-hidden />
            {a("addShort")}
          </Button>
        </td>
      </tr>
      {error && (
        <tr>
          <td colSpan={COLUMNS} className="px-4 py-2.5 text-caption text-destructive">
            {a("loadFailed", { error })}
          </td>
        </tr>
      )}
      {!error && tokens.length === 0 && (
        <tr>
          <td colSpan={COLUMNS} className="px-4 py-2.5 text-caption text-muted-foreground">
            {a("noConnections")}
          </td>
        </tr>
      )}
      {tokens.map((token) => (
        <TokenRow key={token.id} token={token} onDelete={() => onDelete(repository, token)} />
      ))}
    </tbody>
  );
}

function TokenRow({ token, onDelete }: { token: AlertToken; onDelete: () => void }) {
  const { t, lang } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.alertConnections.${key}`, params);

  return (
    <tr className="border-t border-border/50 hover:bg-muted/40">
      <td className="max-w-80 truncate px-4 py-2 font-medium" title={token.label}>
        {token.label}
      </td>
      <td className="whitespace-nowrap px-4 py-2">
        {isAlertEnv(token.default_env) && <>{a(`envs.${token.default_env}`)} </>}
        <span className="font-mono text-caption text-muted-foreground">{token.default_env}</span>
      </td>
      <td className="whitespace-nowrap px-4 py-2 text-muted-foreground" title={formatDate(token.created_at)}>
        {formatRelativeTime(token.created_at, lang)}
      </td>
      <td className="whitespace-nowrap px-4 py-2">
        {token.last_used_at ? (
          <span className="text-muted-foreground" title={formatDate(token.last_used_at)}>
            {formatRelativeTime(token.last_used_at, lang)}
          </span>
        ) : (
          <span className="text-muted-foreground" title={a("neverTitle")}>
            {a("never")}
          </span>
        )}
      </td>
      <td className="whitespace-nowrap px-4 py-2 text-right">
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 text-destructive hover:text-destructive"
          onClick={onDelete}
          aria-label={a("deleteLabel", { label: token.label })}
          title={a("delete")}
        >
          <Trash2 aria-hidden />
        </Button>
      </td>
    </tr>
  );
}
