import { describe, expect, it } from "vitest";
import {
  SETUP_INTEGRATIONS,
  integrationChoices,
  orderedSelection,
  preselectedIntegrations,
} from "@/lib/integrationNeeds";

const recommended = (slugs: string[]) =>
  integrationChoices(slugs)
    .filter((choice) => choice.recommended)
    .map((choice) => choice.integration.id);

describe("integrationChoices", () => {
  it("offers every integration but GitHub, in display order", () => {
    expect(integrationChoices([]).map((choice) => choice.integration.id)).toEqual([
      "asc",
      "google-play",
      "cloud",
      "jira",
      "issue-sync",
    ]);
    expect(SETUP_INTEGRATIONS.some((integration) => (integration.id as string) === "github")).toBe(false);
  });

  it("recommends cloud accounts for the release engineer, who is always on", () => {
    expect(recommended([])).toEqual(["cloud"]);
    expect(recommended(["frontend-developer", "backend-developer"])).toEqual(["cloud"]);
  });

  it("recommends both stores once a mobile developer is on the team", () => {
    expect(recommended(["mobile-developer"])).toEqual(["asc", "google-play", "cloud"]);
  });

  it("never recommends Jira or issue import — they depend on how the team works, not who is on it", () => {
    const everyone = ["mobile-developer", "game-developer", "data-scientist", "qa-agent", "ui-designer"];
    expect(recommended(everyone)).not.toContain("jira");
    expect(recommended(everyone)).not.toContain("issue-sync");
  });
});

describe("selection helpers", () => {
  it("preselects exactly the recommended ones", () => {
    expect(preselectedIntegrations(integrationChoices(["mobile-developer"]))).toEqual(["asc", "google-play", "cloud"]);
  });

  it("puts a selection back in display order", () => {
    expect(orderedSelection(["issue-sync", "asc", "cloud"])).toEqual(["asc", "cloud", "issue-sync"]);
  });
});
