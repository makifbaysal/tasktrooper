export const RESERVED_MCP_NAME = "tasktrooper";

export function isReservedMcpName(name: string): boolean {
  return name.trim().toLowerCase() === RESERVED_MCP_NAME;
}
