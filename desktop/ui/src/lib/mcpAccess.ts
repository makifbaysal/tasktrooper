import type { Agent, MCPServerView } from "@/api";
import { desktopRunner } from "@/lib/desktop-bridge";

function serverPatternMatches(pattern: string, serverId: string): boolean {
  if (!pattern.includes("*")) return pattern === serverId;
  const escaped = pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*");
  return new RegExp(`^${escaped}$`).test(serverId);
}

/** Agents whose tool policy names this server, the same match the backend makes (exact id or `*` pattern). */
export function agentsListingServer(agents: Pick<Agent, "id" | "name" | "tool_policy">[], serverId: string): string[] {
  return agents
    .filter((agent) => (agent.tool_policy?.allow_mcp_servers ?? []).some((p) => serverPatternMatches(p, serverId)))
    .map((agent) => agent.name)
    .sort((a, b) => a.localeCompare(b));
}

export function needsOAuthSignIn(server: Pick<MCPServerView, "transport" | "auth">): boolean {
  return server.transport === "http" && (server.auth === "oauth_needed" || server.auth === "oauth_expired");
}

/**
 * Opens an authorization server's sign-in page in the person's own browser.
 * In the desktop shell that is the bridge's `openExternal` (https or loopback
 * http only), never a window inside the app; in a browser it is a new tab.
 */
export async function openAuthorizationPage(url: string): Promise<boolean> {
  const runner = desktopRunner();
  if (runner) return runner.openExternal(url);
  window.open(url, "_blank", "noopener,noreferrer");
  return true;
}
