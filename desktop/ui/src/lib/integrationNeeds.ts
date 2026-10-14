// The integrations the first run offers, and which of them the enabled agents
// make worth setting up now. GitHub is not here: it has its own numbered step.
//
// The core agents (CORE_AGENT_SLUGS) are always on, so what they need is
// recommended whatever the roster says — and even when it could not be read.

import { CORE_AGENT_SLUGS } from "@/lib/teamTemplates";

export type SetupIntegrationId = "asc" | "google-play" | "cloud" | "jira" | "issue-sync";

export interface SetupIntegration {
  id: SetupIntegrationId;
  /** i18n keys, so the table stays language-neutral; the names are the Settings cards' own. */
  nameKey: string;
  descriptionKey: string;
  /** Catalog slugs whose work this integration serves; empty for an integration nobody needs. */
  forAgents: readonly string[];
}

export const SETUP_INTEGRATIONS: readonly SetupIntegration[] = [
  {
    id: "asc",
    nameKey: "settingsPages.integrations.asc.title",
    descriptionKey: "setup.integrations.items.asc.description",
    forAgents: ["mobile-developer"],
  },
  {
    id: "google-play",
    nameKey: "settingsPages.integrations.play.title",
    descriptionKey: "setup.integrations.items.googlePlay.description",
    forAgents: ["mobile-developer"],
  },
  {
    id: "cloud",
    nameKey: "setup.integrations.items.cloud.name",
    descriptionKey: "setup.integrations.items.cloud.description",
    forAgents: ["release-engineer"],
  },
  {
    id: "jira",
    nameKey: "issues.jira.title",
    descriptionKey: "setup.integrations.items.jira.description",
    forAgents: [],
  },
  {
    id: "issue-sync",
    nameKey: "issues.sync.title",
    descriptionKey: "setup.integrations.items.issueSync.description",
    forAgents: [],
  },
];

export interface IntegrationChoice {
  integration: SetupIntegration;
  recommended: boolean;
}

/** Every offered integration, in display order, marked recommended when an enabled (or core) agent uses it. */
export function integrationChoices(enabledSlugs: Iterable<string>): IntegrationChoice[] {
  const team = new Set<string>([...CORE_AGENT_SLUGS, ...enabledSlugs]);
  return SETUP_INTEGRATIONS.map((integration) => ({
    integration,
    recommended: integration.forAgents.some((slug) => team.has(slug)),
  }));
}

/** Checked on first sight: the recommended ones. */
export function preselectedIntegrations(choices: readonly IntegrationChoice[]): SetupIntegrationId[] {
  return choices.filter((choice) => choice.recommended).map((choice) => choice.integration.id);
}

/** The picked ids in display order, whatever order they were picked in. */
export function orderedSelection(selected: Iterable<SetupIntegrationId>): SetupIntegrationId[] {
  const chosen = new Set(selected);
  return SETUP_INTEGRATIONS.filter((integration) => chosen.has(integration.id)).map((integration) => integration.id);
}
