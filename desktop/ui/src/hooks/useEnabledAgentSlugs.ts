import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type Agent } from "@/api";
import { enabledTeam } from "@/lib/toolchain";

const EMPTY_RETRY_MS = 2000;
const EMPTY_RETRIES = 15;

export interface EnabledAgentSlugs {
  slugs: string[];
  /** Display name per catalog slug. */
  names: Record<string, string>;
  loaded: boolean;
  /** The roster is still empty while the catalog's first sync creates it. */
  preparing: boolean;
  error: string;
  reload: () => Promise<void>;
}

/**
 * The enabled agents' catalog slugs, read straight from `listAgents` — the
 * settings pages render outside the workspace outlet, so its roster is not
 * there to reuse.
 */
export function useEnabledAgentSlugs({ retryMs = EMPTY_RETRY_MS }: { retryMs?: number } = {}): EnabledAgentSlugs {
  const [agents, setAgents] = useState<Agent[] | null>(null);
  const [error, setError] = useState("");
  const [emptyRetries, setEmptyRetries] = useState(0);

  const load = useCallback(async () => {
    try {
      const { agents: list } = await api.listAgents();
      setAgents(list ?? []);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // Same as TeamAgentsEditor: a workspace created a moment ago gets its
  // catalog agents a few seconds later, so an empty roster is asked again.
  useEffect(() => {
    if (!agents || agents.length > 0 || emptyRetries >= EMPTY_RETRIES) return;
    const timer = setTimeout(() => {
      setEmptyRetries((n) => n + 1);
      void load();
    }, retryMs);
    return () => clearTimeout(timer);
  }, [agents, emptyRetries, load, retryMs]);

  const team = useMemo(() => (agents ? enabledTeam(agents) : []), [agents]);
  const slugs = useMemo(() => team.map((member) => member.slug), [team]);
  const names = useMemo(() => Object.fromEntries(team.map((member) => [member.slug, member.name])), [team]);

  return {
    slugs,
    names,
    loaded: agents !== null,
    preparing: agents !== null && agents.length === 0 && emptyRetries < EMPTY_RETRIES,
    error,
    reload: load,
  };
}
