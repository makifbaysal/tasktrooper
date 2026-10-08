import type { LLMProviderDefinition, LLMProviderType } from "@/api";

/**
 * Providers that run as a local process on this host instead of being reached
 * over HTTP.
 *
 * They have no base URL and no API key, so the LLM settings page's connect /
 * test / activate / embedding calls have nothing to send and the server rejects
 * them outright. They still get a card on that page — the section that names
 * which local CLIs this product runs — but the card explains where the
 * connection actually happens (the runner) instead of offering a form.
 * Choosing one is done on an AGENT.
 */
export const HOST_EXECUTED_PROVIDERS: LLMProviderType[] = ["claude_code", "cursor_agent", "antigravity", "opencode"];

/**
 * Providers that are declared but have no executor behind them yet.
 *
 * Empty today — every provider the server declares (`claude_code`,
 * `cursor_agent`, `antigravity`, `opencode`) has a real executor and is
 * `available: true`. This stays here as the fallback for a server that
 * predates `definition.available`: such a server cannot send a provider it
 * doesn't know about, so this only matters for a NEWER, still-unbuilt
 * provider read by an OLDER version of this list — keep it in step with the
 * server's own unavailable set (`internal/domain/llm_provider.go`) whenever
 * that becomes non-empty again.
 */
export const UNAVAILABLE_PROVIDERS: LLMProviderType[] = [];

/**
 * True for a provider the runner host executes. The definition's own flag wins;
 * the constant is the fallback for a server that predates the flag.
 */
export function isHostExecutedProvider(
  provider: LLMProviderType | string,
  definition?: Pick<LLMProviderDefinition, "host_executed">,
): boolean {
  if (definition?.host_executed) return true;
  return HOST_EXECUTED_PROVIDERS.includes(provider as LLMProviderType);
}

/**
 * True for a provider that can actually run work today.
 *
 * A missing `available` means the server predates the flag, and every provider
 * such a server knows about is one it can run — so the default is available,
 * and the named fallback covers the one type that is not.
 *
 * This is what decides whether a provider is offered or shown as "coming soon".
 * It is never the only thing standing in the way: the server refuses an
 * unavailable provider on connect, on activate and on agent save, because this
 * bundle is not the only client and a disabled button is not a guarantee.
 */
export function isProviderAvailable(
  provider: LLMProviderType | string,
  definition?: Pick<LLMProviderDefinition, "available">,
): boolean {
  if (definition?.available !== undefined) return definition.available;
  return !UNAVAILABLE_PROVIDERS.includes(provider as LLMProviderType);
}

export const SUBAGENT_TYPES = [
  "explore",
  "shell",
  "generalPurpose",
  "backend-engineer",
  "frontend-engineer",
  "mobile-dev-engineer",
  "system-architect",
  "data-scientist",
  "game-developer",
  "security-reviewer",
] as const;

export type SubagentType = (typeof SUBAGENT_TYPES)[number];
