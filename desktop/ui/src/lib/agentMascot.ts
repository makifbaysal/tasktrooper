// Each catalog agent is the mascot in its own colour. A custom agent (no
// catalog slug, a name that is not one) has none and keeps its initials.
export const AGENT_COLORS = {
  "product-manager": "#F59E0B",
  "system-architect": "#14B8A6",
  "backend-developer": "#65A30D",
  "frontend-developer": "#0EA5E9",
  "mobile-developer": "#8B5CF6",
  "qa-agent": "#EA580C",
  "release-engineer": "#DC2626",
  "security-agent": "#94A3B8",
  "ui-designer": "#EC4899",
  "data-scientist": "#2563EB",
  "game-developer": "#FACC15",
} as const;

export type AgentMascotSlug = keyof typeof AGENT_COLORS;

const avatars = import.meta.glob<string>("@/assets/mascot/avatars/*.webp", {
  eager: true,
  query: "?url",
  import: "default",
});

function avatarUrl(file: string): string | undefined {
  return avatars[`/src/assets/mascot/avatars/${file}.webp`];
}

export function agentMascotSlug(slug?: string | null, name?: string | null): AgentMascotSlug | null {
  for (const candidate of [slug, name?.trim().toLowerCase()]) {
    if (candidate && candidate in AGENT_COLORS) return candidate as AgentMascotSlug;
  }
  return null;
}

export function agentAvatarUrl(slug: AgentMascotSlug): string | undefined {
  return avatarUrl(slug);
}

export function trooperAvatarUrl(): string | undefined {
  return avatarUrl("trooper");
}
