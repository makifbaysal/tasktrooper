import type { ToolPolicy } from "@/api";
import { tStatic } from "@/hooks/useI18n";

export function isBuiltinTool(name: string): boolean {
  return !name.startsWith("mcp_");
}

export function selectedBuiltinTools(policy: ToolPolicy, known?: string[]): string[] {
  const allowed = policy.allow_tools ?? [];
  if (!known || known.length === 0) {
    return allowed;
  }
  const set = new Set(known);
  return allowed.filter((name) => set.has(name));
}

export const TASKTROOPER_MCP_ID = "tasktrooper";

export function isTaskTrooperMCP(id: string): boolean {
  return id.trim().toLowerCase() === TASKTROOPER_MCP_ID;
}

export function selectedMCPServers(policy: ToolPolicy): string[] {
  return (policy.allow_mcp_servers ?? []).filter((id) => !isTaskTrooperMCP(id));
}

export function withAllowedBuiltinTools(policy: ToolPolicy, allowTools: string[]): ToolPolicy {
  const next = { ...policy };
  if (allowTools.length > 0) {
    next.allow_tools = allowTools;
  } else {
    delete next.allow_tools;
  }
  return next;
}

export function withAllowedMCPServers(policy: ToolPolicy, serverIds: string[]): ToolPolicy {
  const next = { ...policy };
  const ids = serverIds.filter((id) => !isTaskTrooperMCP(id));
  if (ids.length > 0) {
    next.allow_mcp_servers = ids;
  } else {
    delete next.allow_mcp_servers;
  }
  return next;
}

// Reactive label for a built-in tool, resolved against lib.toolPolicy.builtin.*.
export function builtinToolLabel(name: string): string {
  const key = `lib.toolPolicy.builtin.${name}`;
  const label = tStatic(key);
  return label === key ? name : label;
}
