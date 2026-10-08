import { describe, expect, it } from "vitest";
import { FEEDBACK_REPO_URL, MAX_FEEDBACK_URL_LENGTH, feedbackIssueUrl } from "@/lib/feedback";

function params(url: string): URLSearchParams {
  return new URL(url).searchParams;
}

describe("feedbackIssueUrl", () => {
  it("opens a bug on the repository's bug template, with the title and body pre-filled", () => {
    const url = feedbackIssueUrl({
      kind: "bug",
      title: "  Header buttons do nothing  ",
      description: "Clicked the bell in full screen.",
      environment: { version: "0.2.22", platform: "darwin" },
    });

    expect(url.startsWith(`${FEEDBACK_REPO_URL}/issues/new?`)).toBe(true);
    const q = params(url);
    expect(q.get("template")).toBe("bug_report.md");
    expect(q.get("labels")).toBe("bug");
    expect(q.get("title")).toBe("Header buttons do nothing");
    const body = q.get("body") ?? "";
    expect(body).toContain("## What happened\n\nClicked the bell in full screen.");
    expect(body).toContain("- OS: macOS");
    expect(body).toContain("- TaskTrooper version: 0.2.22");
  });

  it("uses the feature template for a feature request", () => {
    const q = params(feedbackIssueUrl({ kind: "feature", title: "Dark tray icon", description: "Please." }));
    expect(q.get("template")).toBe("feature_request.md");
    expect(q.get("labels")).toBe("enhancement");
    expect(q.get("body")).toContain("## Problem\n\nPlease.");
  });

  it("leaves the environment out when the user did not attach it", () => {
    const body = params(feedbackIssueUrl({ kind: "bug", title: "x", description: "y" })).get("body") ?? "";
    expect(body).not.toContain("## Environment");
  });

  it("cuts a description that would push the URL past what GitHub accepts", () => {
    const url = feedbackIssueUrl({ kind: "bug", title: "Long", description: "ğ".repeat(20_000) });
    expect(url.length).toBeLessThanOrEqual(MAX_FEEDBACK_URL_LENGTH);
    expect(params(url).get("body")).toContain("…");
  });
});
