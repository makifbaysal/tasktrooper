import type { McpServerSetRequest } from "@ipc/channels.js";
import { isReservedMcpName } from "@ipc/mcp-names.js";
import type { McpServerSummary } from "@ipc/types.js";

export type McpTransport = "stdio" | "http";

export interface McpSecretRow {
  name: string;
  /** Empty on a stored server keeps what is stored under `name`. */
  value: string;
}

/** What the MCP server form holds. A value field is cleared the moment it is sent. */
export interface McpForm {
  /** Set when changing a stored server; absent for a new one. */
  editing?: string;
  name: string;
  transport: McpTransport;
  command: string;
  /** One argument per line. */
  args: string;
  url: string;
  /** Environment variables (stdio) or headers (http). */
  secrets: McpSecretRow[];
}

export function emptyMcpForm(): McpForm {
  return { name: "", transport: "stdio", command: "", args: "", url: "", secrets: [] };
}

export function mcpFormFor(server: McpServerSummary): McpForm {
  return {
    editing: server.name,
    name: server.name,
    transport: server.transport,
    command: server.command ?? "",
    args: (server.args ?? []).join("\n"),
    url: server.url ?? "",
    secrets: server.secretNames.map((name) => ({ name, value: "" })),
  };
}

export function mcpFormProblem(form: McpForm): string | null {
  if (isReservedMcpName(form.name)) return "TaskTrooper's own MCP server is always on for every agent. Pick another name.";
  return null;
}

export function secretLabel(transport: McpTransport): string {
  return transport === "http" ? "Headers" : "Environment variables";
}

export function toMcpRequest(form: McpForm): McpServerSetRequest {
  const secrets: Record<string, string> = {};
  for (const row of form.secrets) {
    const name = row.name.trim();
    if (name !== "") secrets[name] = row.value;
  }
  const hasSecrets = Object.keys(secrets).length > 0;
  if (form.transport === "http") {
    return { name: form.name.trim(), url: form.url.trim(), ...(hasSecrets ? { headers: secrets } : {}) };
  }
  const args = form.args
    .split("\n")
    .map((a) => a.trim())
    .filter((a) => a !== "");
  return {
    name: form.name.trim(),
    command: form.command.trim(),
    ...(args.length > 0 ? { args } : {}),
    ...(hasSecrets ? { env: secrets } : {}),
  };
}
