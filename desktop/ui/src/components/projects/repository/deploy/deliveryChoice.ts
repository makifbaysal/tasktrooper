import type { ComponentDelivery, DeliveryExecutor, DeliveryMode } from "@/api";

/** The first question: does a merge deploy, does a person decide, or does nothing deploy. */
export type DeliveryWhen = "merge" | "manual" | "never";

/** The second question: what actually carries the deploy out. */
export type DeliveryHow = "vercel" | "actionsPush" | "actionsDispatch" | "tag" | "local" | "store";

export const HOW_FOR_WHEN: Record<DeliveryWhen, DeliveryHow[]> = {
  merge: ["vercel", "actionsPush", "actionsDispatch"],
  manual: ["tag", "local", "store"],
  never: [],
};

const PROFILE_FOR_HOW: Record<DeliveryHow, { mode: DeliveryMode; executor: DeliveryExecutor }> = {
  vercel: { mode: "on_merge", executor: "vercel" },
  actionsPush: { mode: "on_merge", executor: "github_actions" },
  actionsDispatch: { mode: "dispatch", executor: "github_actions" },
  tag: { mode: "batch", executor: "github_actions" },
  local: { mode: "batch", executor: "local" },
  store: { mode: "batch", executor: "store" },
};

export function whenOf(d: ComponentDelivery): DeliveryWhen {
  if (d.mode === "none") return "never";
  return d.mode === "batch" ? "manual" : "merge";
}

export function howOf(d: ComponentDelivery): DeliveryHow | null {
  switch (d.mode) {
    case "none":
      return null;
    case "dispatch":
      return "actionsDispatch";
    case "on_merge":
      return d.executor === "vercel" ? "vercel" : "actionsPush";
    case "batch":
      if (d.executor === "local") return "local";
      if (d.executor === "store") return "store";
      return "tag";
  }
}

/** Applies an answer to the draft, dropping the fields the new answer has no use for. */
export function withHow(d: ComponentDelivery, how: DeliveryHow): ComponentDelivery {
  const { mode, executor } = PROFILE_FOR_HOW[how];
  const usesWorkflow = how === "actionsPush" || how === "actionsDispatch";
  return {
    ...d,
    mode,
    executor,
    workflow: usesWorkflow ? d.workflow : undefined,
    tag_pattern: how === "tag" ? d.tag_pattern : undefined,
    local_command: how === "local" ? d.local_command : undefined,
  };
}

export function withNever(d: ComponentDelivery): ComponentDelivery {
  return { ...d, mode: "none", executor: undefined, workflow: undefined, tag_pattern: undefined, local_command: undefined };
}

type Translate = (key: string, params?: Record<string, string | number>) => string;

/** One plain sentence saying what a merge of this component sets in motion. */
export function deliverySummary(d: ComponentDelivery, t: Translate): string {
  const how = howOf(d);
  if (!how) return t("release.wizard.summary.never");
  const workflow = (d.workflow ?? "").trim();
  switch (how) {
    case "vercel":
      return t("release.wizard.summary.vercel");
    case "actionsPush":
      return workflow ? t("release.wizard.summary.actionsPushNamed", { workflow }) : t("release.wizard.summary.actionsPush");
    case "actionsDispatch":
      return t("release.wizard.summary.actionsDispatch", { workflow });
    case "tag":
      return t("release.wizard.summary.tag", { tag: (d.tag_pattern ?? "").trim() || "v{version}" });
    case "local":
      return t("release.wizard.summary.local", { command: (d.local_command ?? "").trim() });
    case "store":
      return t("release.wizard.summary.store");
  }
}
