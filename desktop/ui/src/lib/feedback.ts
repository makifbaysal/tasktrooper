export const FEEDBACK_REPO_URL = "https://github.com/makifbaysal/tasktrooper";

export type FeedbackKind = "bug" | "feature";

export interface FeedbackDraft {
  kind: FeedbackKind;
  title: string;
  description: string;
  /** Present when the user agreed to attach it. */
  environment?: { version: string; platform: string };
}

// The repository turns blank issues off, so the issue opens on its own
// template; `template` also carries that template's labels, which the
// `labels` parameter only does for people with triage rights.
const TEMPLATES: Record<FeedbackKind, { file: string; label: string; heading: string }> = {
  bug: { file: "bug_report.md", label: "bug", heading: "What happened" },
  feature: { file: "feature_request.md", label: "enhancement", heading: "Problem" },
};

/** GitHub refuses longer request lines; a long description is cut to fit. */
export const MAX_FEEDBACK_URL_LENGTH = 7500;

const PLATFORM_NAMES: Record<string, string> = { darwin: "macOS", win32: "Windows", linux: "Linux" };

function issueBody(draft: FeedbackDraft, description: string): string {
  const { heading } = TEMPLATES[draft.kind];
  const lines = [`## ${heading}`, "", description.trim() || "_No description._", ""];
  if (draft.environment) {
    const platform = PLATFORM_NAMES[draft.environment.platform] ?? draft.environment.platform;
    lines.push("## Environment", "", `- OS: ${platform}`, `- TaskTrooper version: ${draft.environment.version}`, "");
  }
  lines.push("_Sent from TaskTrooper → Send feedback._");
  return lines.join("\n");
}

function issueUrl(draft: FeedbackDraft, description: string): string {
  const template = TEMPLATES[draft.kind];
  const params = new URLSearchParams({
    template: template.file,
    labels: template.label,
    title: draft.title.trim(),
    body: issueBody(draft, description),
  });
  return `${FEEDBACK_REPO_URL}/issues/new?${params.toString()}`;
}

/** A pre-filled "new issue" page; the user reviews and submits it on GitHub. */
export function feedbackIssueUrl(draft: FeedbackDraft): string {
  let description = draft.description;
  let url = issueUrl(draft, description);
  while (url.length > MAX_FEEDBACK_URL_LENGTH && description.length > 0) {
    description = description.slice(0, Math.floor(description.length * 0.8));
    url = issueUrl(draft, `${description}…`);
  }
  return url;
}
