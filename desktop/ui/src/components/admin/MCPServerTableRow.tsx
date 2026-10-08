import { ChevronRight, KeyRound, LogOut, Pencil, Trash2 } from "lucide-react";
import { Fragment } from "react";
import type { MCPConfigField, MCPServerView } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { needsOAuthSignIn } from "@/lib/mcpAccess";
import { cn } from "@/lib/utils";

function formatToolName(fullName: string, serverId: string): string {
  const prefix = `mcp_${serverId}_`;
  if (fullName.startsWith(prefix)) return fullName.slice(prefix.length);
  return fullName;
}

function statusVariant(status: MCPServerView["status"]) {
  if (status === "connected") return "success" as const;
  if (status === "disabled") return "secondary" as const;
  if (status === "needs_config") return "warning" as const;
  return "destructive" as const;
}

function configFieldLabel(field: MCPConfigField): string {
  if (field.location === "args") return field.description || `args[${field.key}]`;
  return field.key;
}

interface MCPServerTableRowProps {
  server: MCPServerView;
  expanded: boolean;
  onToggleExpand: () => void;
  toggling: boolean;
  onToggleEnabled: (enabled: boolean) => void;
  onEdit: () => void;
  onDelete: () => void;
  /** Names of the agents whose tool policy names this server. */
  listedBy?: string[];
  onConnect?: () => void;
  onDisconnect?: () => void;
  /** Set while this server's sign-in is open in the browser; the URL is offered again in case the tab never opened. */
  signInUrl?: string | null;
  authBusy?: boolean;
}

export function MCPServerTableRow({
  server,
  expanded,
  onToggleExpand,
  toggling,
  onToggleEnabled,
  onEdit,
  onDelete,
  listedBy = [],
  onConnect,
  onDisconnect,
  signInUrl = null,
  authBusy = false,
}: MCPServerTableRowProps) {
  const { t } = useI18n();
  const tools = server.tools ?? [];
  const canExpand = tools.length > 0;
  const access = server.access ?? "all";

  const missing = server.missing_config ?? [];
  const signInNeeded = server.enabled && needsOAuthSignIn(server);

  const statusLabel = (status: MCPServerView["status"]) => {
    if (status === "connected") return t("frame.admin.mcpRow.connected");
    if (status === "disabled") return t("frame.admin.mcpRow.disabled");
    if (status === "needs_config") return t("frame.admin.mcpRow.needsConfig");
    return t("frame.admin.mcpRow.error");
  };

  return (
    <Fragment>
      <tr
        className={cn(
          "border-b border-border hover:bg-muted/30",
          expanded && "border-b-0 bg-muted/20",
        )}
      >
        <td className="px-4 py-3">
          <div className="font-medium">{server.id}</div>
        </td>
        <td className="px-4 py-3">
          <Badge variant="outline">{server.transport}</Badge>
        </td>
        <td className="px-4 py-3">
          <div className="flex max-w-[14rem] flex-col gap-1">
            <Badge variant={access === "all" ? "secondary" : "info"} className="w-fit">
              {access === "all" ? t("frame.admin.mcpRow.accessAll") : t("frame.admin.mcpRow.accessListed")}
            </Badge>
            {listedBy.length > 0 ? (
              <p className="text-xs leading-snug text-muted-foreground" title={listedBy.join(", ")}>
                {t("frame.admin.mcpRow.listedBy", { agents: listedBy.join(", ") })}
              </p>
            ) : (
              access === "listed" && (
                <p className="text-xs leading-snug text-muted-foreground">{t("frame.admin.mcpRow.listedByNone")}</p>
              )
            )}
          </div>
        </td>
        <td className="px-4 py-3">
          <div className="flex max-w-xs flex-col items-start gap-1">
            {signInNeeded ? (
              <Badge variant="warning">
                {server.auth === "oauth_expired"
                  ? t("frame.admin.mcpRow.signInExpired")
                  : t("frame.admin.mcpRow.signInNeeded")}
              </Badge>
            ) : (
              <Badge variant={statusVariant(server.status)}>{statusLabel(server.status)}</Badge>
            )}
            {server.status === "error" && !signInNeeded && (
              <p
                className="text-xs leading-snug text-destructive"
                title={server.last_error || t("frame.admin.mcpRow.unknownError")}
              >
                {server.last_error || t("frame.admin.mcpRow.connectFailed")}
              </p>
            )}
            {missing.length > 0 && (
              <p className="text-xs leading-snug text-muted-foreground">
                {t("frame.admin.mcpRow.missingConfig", {
                  fields: missing.map(configFieldLabel).join(", "),
                })}
              </p>
            )}
            {signInUrl ? (
              <div className="flex flex-col gap-0.5 text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-1.5">
                  <Spinner className="h-3 w-3" />
                  {t("frame.admin.mcpRow.waitingForSignIn")}
                </span>
                <a href={signInUrl} target="_blank" rel="noreferrer" className="text-info hover:underline">
                  {t("content.mcp.openSignInPage")}
                </a>
              </div>
            ) : signInNeeded && onConnect ? (
              <Button size="sm" variant="outline" className="h-7 gap-1.5" disabled={authBusy} onClick={onConnect}>
                <KeyRound className="h-3.5 w-3.5" />
                {server.auth === "oauth_expired" ? t("frame.admin.mcpRow.reconnect") : t("frame.admin.mcpRow.connect")}
              </Button>
            ) : server.auth === "oauth_connected" ? (
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <span>{t("frame.admin.mcpRow.signedIn")}</span>
                {onDisconnect && (
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 gap-1 px-1.5 text-xs"
                    disabled={authBusy}
                    onClick={onDisconnect}
                  >
                    <LogOut className="h-3 w-3" />
                    {t("frame.admin.mcpRow.disconnect")}
                  </Button>
                )}
              </div>
            ) : null}
          </div>
        </td>
        <td className="px-4 py-3">
          {canExpand ? (
            <button
              type="button"
              onClick={onToggleExpand}
              className="flex items-center gap-1.5 text-left text-muted-foreground hover:text-foreground"
            >
              <ChevronRight
                className={cn("h-3.5 w-3.5 shrink-0 transition-transform", expanded && "rotate-90")}
              />
              <span>{server.tool_count}</span>
            </button>
          ) : (
            <span className="text-muted-foreground">{server.tool_count}</span>
          )}
        </td>
        <td className="px-4 py-3">
          <Switch checked={server.enabled} disabled={toggling} onCheckedChange={onToggleEnabled} />
        </td>
        <td className="px-4 py-3">
          <div className="flex gap-1">
            <Button variant="ghost" size="icon" onClick={onEdit}>
              <Pencil className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="icon" onClick={onDelete}>
              <Trash2 className="h-4 w-4 text-destructive" />
            </Button>
          </div>
        </td>
      </tr>
      {expanded && (
        <tr className="border-b border-border bg-muted/10">
          <td colSpan={7} className="px-4 py-3">
            <div className="text-xs font-medium text-muted-foreground">
              {t("frame.admin.mcpRow.tools", { count: tools.length })}
            </div>
            <div className="mt-2 flex flex-wrap gap-2">
              {tools.map((tool) => (
                <Badge key={tool} variant="secondary" className="font-mono text-micro font-normal" title={tool}>
                  {formatToolName(tool, server.id)}
                </Badge>
              ))}
            </div>
          </td>
        </tr>
      )}
    </Fragment>
  );
}
