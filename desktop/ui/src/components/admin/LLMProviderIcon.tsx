import { Server, Terminal } from "lucide-react";
import type { LLMProviderType } from "@/api";
import { BrandIcon, type Brand } from "@/components/ui/brand-icon";
import { cn } from "@/lib/utils";

const PROVIDER_BRANDS: Partial<Record<LLMProviderType, Brand>> = {
  claude_code: "claude",
  cursor_agent: "cursor",
  anthropic: "anthropic",
  gemini: "gemini",
  openai: "openai",
};

// Matched on the host, not a stored preset id: an endpoint keeps no record of
// the preset it was added from.
const ENDPOINT_BRANDS: [RegExp, Brand][] = [
  [/ollama|:11434\b/i, "ollama"],
  [/openrouter\.ai/i, "openrouter"],
  [/api\.openai\.com/i, "openai"],
  [/generativelanguage\.googleapis\.com/i, "gemini"],
  [/anthropic\.com/i, "anthropic"],
];

export function LLMProviderIcon({ type, className }: { type: LLMProviderType; className?: string }) {
  const brand = PROVIDER_BRANDS[type];
  if (brand) return <BrandIcon brand={brand} className={cn("h-5 w-5", className)} />;
  return <Terminal className={cn("h-5 w-5", className)} aria-hidden />;
}

export function LLMEndpointIcon({ baseURL, className }: { baseURL: string; className?: string }) {
  const brand = ENDPOINT_BRANDS.find(([pattern]) => pattern.test(baseURL))?.[1];
  if (brand) return <BrandIcon brand={brand} className={cn("h-5 w-5", className)} />;
  return <Server className={cn("h-5 w-5", className)} aria-hidden />;
}
