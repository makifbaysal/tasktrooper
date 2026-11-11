import { describe, expect, it } from "vitest";
import type { TaskAgentRun, TaskReviews } from "@/api";
import { taskChatAgents } from "@/lib/taskChatAgents";

const agents = [
  { id: "dev", name: "backend-developer", enabled: true },
  { id: "qa", name: "qa-agent", enabled: true },
  { id: "sec", name: "security-agent", enabled: true },
  { id: "off", name: "retired", enabled: false },
];

const run = (agent_id: string, status: string) => ({ agent_id, status }) as TaskAgentRun;

describe("taskChatAgents", () => {
  it("lists the assignee, run agents and reviewers once, working agents first", () => {
    const reviews = {
      column: "code_review",
      rounds: [{ round: 1, outcome: "open", reviewers: [{ agent_id: "sec", agent_name: "security-agent", verdict: "pending", required: true }] }],
    } as unknown as TaskReviews;
    const got = taskChatAgents(
      { assignee_agent_id: "dev" },
      [run("dev", "completed"), run("qa", "running"), run("off", "running")],
      reviews,
      agents,
    );
    expect(got).toEqual([
      { id: "qa", name: "qa-agent", working: true },
      { id: "dev", name: "backend-developer", working: false },
      { id: "sec", name: "security-agent", working: false },
    ]);
  });

  it("is empty when nobody is attached to the task", () => {
    expect(taskChatAgents({ assignee_agent_id: undefined }, [], null, agents)).toEqual([]);
  });
});
