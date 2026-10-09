import type { KeySetRequest } from "@ipc/channels.js";
import {
  BUILT_IN_PROVIDER_TYPES,
  CUSTOM_PROVIDER_TYPE,
  type ProviderKeySummary,
  type ProviderKeyType,
} from "@ipc/types.js";

export const PROVIDER_TYPES: readonly ProviderKeyType[] = [...BUILT_IN_PROVIDER_TYPES, CUSTOM_PROVIDER_TYPE];

const LABELS: Record<ProviderKeyType, string> = {
  anthropic: "Anthropic",
  openai: "OpenAI",
  gemini: "Google Gemini",
  groq: "Groq",
  local: "Local server (LM Studio, Ollama…)",
  openai_compatible: "Custom endpoint (OpenAI-compatible)",
};

export function typeLabel(type: string): string {
  return (LABELS as Record<string, string>)[type] ?? type;
}

/** What the form holds. The key field is cleared the moment it is sent. */
export interface KeyForm {
  /** Set when changing a stored provider; absent for a new one. */
  id?: string;
  type: ProviderKeyType;
  baseUrl: string;
  models: string;
  apiKey: string;
}

export function emptyForm(type: ProviderKeyType = "anthropic"): KeyForm {
  return { type, baseUrl: "", models: "", apiKey: "" };
}

export function formFor(provider: ProviderKeySummary): KeyForm {
  const type = (PROVIDER_TYPES as readonly string[]).includes(provider.type)
    ? (provider.type as ProviderKeyType)
    : CUSTOM_PROVIDER_TYPE;
  return { id: provider.id, type, baseUrl: provider.base_url ?? "", models: (provider.models ?? []).join(", "), apiKey: "" };
}

/** Whether the type asks for an address: always for a custom endpoint, optionally for a local server. */
export function wantsAddress(type: ProviderKeyType): boolean {
  return type === CUSTOM_PROVIDER_TYPE || type === "local";
}

export function toRequest(form: KeyForm): KeySetRequest {
  const models = form.models
    .split(",")
    .map((m) => m.trim())
    .filter((m) => m !== "");
  const baseUrl = form.baseUrl.trim();
  const apiKey = form.apiKey.trim();
  return {
    type: form.type,
    ...(form.id !== undefined ? { id: form.id } : {}),
    ...(baseUrl !== "" ? { base_url: baseUrl } : {}),
    ...(models.length > 0 ? { models } : {}),
    ...(apiKey !== "" ? { api_key: apiKey } : {}),
  };
}
