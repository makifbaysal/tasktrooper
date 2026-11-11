import type { Agent, BoardTask, TaskAgentRun, TaskReviews } from "@/api";

export interface TaskChatAgent {
  id: string;
  name: string;
  /** Has a pending or running run on the task right now. */
  working: boolean;
}

/**
 * The agents a human can talk to about a task: whoever is assigned, whoever has
 * run on it, and whoever sits on its latest review round. Working agents come
 * first so the one the person is waiting on is the first chip.
 */
export function taskChatAgents(
  task: Pick<BoardTask, "assignee_agent_id">,
  runs: TaskAgentRun[],
  reviews: TaskReviews | null,
  agents: Pick<Agent, "id" | "name" | "enabled">[],
): TaskChatAgent[] {
  const working = new Set(
    runs.filter((run) => run.status === "pending" || run.status === "running").map((run) => run.agent_id),
  );
  const ids: string[] = [];
  const add = (id?: string | null) => {
    if (id && !ids.includes(id)) ids.push(id);
  };
  add(task.assignee_agent_id);
  for (const run of runs) add(run.agent_id);
  const latest = reviews?.rounds[reviews.rounds.length - 1];
  for (const reviewer of latest?.reviewers ?? []) add(reviewer.agent_id);

  const out: TaskChatAgent[] = [];
  for (const id of ids) {
    const agent = agents.find((a) => a.id === id);
    if (!agent || agent.enabled === false) continue;
    out.push({ id, name: agent.name, working: working.has(id) });
  }
  return out.sort((a, b) => Number(b.working) - Number(a.working));
}
