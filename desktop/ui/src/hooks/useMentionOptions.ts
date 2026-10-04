import { useEffect, useState } from "react";
import { api } from "@/api";
import type { MentionOption } from "@/components/chat/MentionMenu";

/** The @-mention roster: enabled agents, projects, repositories. Partial failures just shrink the list. */
export function useMentionOptions(): MentionOption[] {
  const [options, setOptions] = useState<MentionOption[]>([]);

  useEffect(() => {
    let cancelled = false;
    Promise.allSettled([api.listAgents(), api.listInitiativeProjects(), api.listRepositories()]).then(
      ([agentsRes, projectsRes, reposRes]) => {
        if (cancelled) return;
        const next: MentionOption[] = [];
        if (agentsRes.status === "fulfilled") {
          for (const a of agentsRes.value.agents ?? []) {
            if (a.enabled) next.push({ id: a.id, name: a.name, kind: "agent", description: a.description });
          }
        }
        if (projectsRes.status === "fulfilled") {
          for (const p of projectsRes.value.projects ?? []) {
            next.push({ id: p.id, name: p.name, kind: "project", description: p.description });
          }
        }
        if (reposRes.status === "fulfilled") {
          for (const r of reposRes.value.repositories ?? []) {
            next.push({ id: r.id, name: r.name, kind: "repository", description: r.description });
          }
        }
        setOptions(next);
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  return options;
}
