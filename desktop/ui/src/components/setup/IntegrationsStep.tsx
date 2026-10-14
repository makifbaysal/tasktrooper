import { Cloud, Inbox } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { AppStoreConnectCard } from "@/components/admin/AppStoreConnectCard";
import { CloudAccountsCard } from "@/components/admin/CloudAccountsCard";
import { GooglePlayCard } from "@/components/admin/GooglePlayCard";
import { IssueSyncCard } from "@/components/admin/IssueSyncCard";
import { JiraCard } from "@/components/admin/JiraCard";
import { IntegrationChoiceTile } from "@/components/setup/IntegrationChoiceTile";
import { BrandIcon } from "@/components/ui/brand-icon";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useEnabledAgentSlugs } from "@/hooks/useEnabledAgentSlugs";
import { useI18n } from "@/hooks/useI18n";
import { useStoreCredentials } from "@/hooks/useStoreCredentials";
import { writeFirstRunIntegrationsDone } from "@/lib/firstRun";
import {
  integrationChoices,
  orderedSelection,
  preselectedIntegrations,
  type SetupIntegrationId,
} from "@/lib/integrationNeeds";

const ICONS: Record<SetupIntegrationId, ReactNode> = {
  asc: <BrandIcon brand="apple" className="h-5 w-5" />,
  "google-play": <BrandIcon brand="googlePlay" className="h-5 w-5" />,
  cloud: <Cloud className="h-5 w-5" aria-hidden />,
  jira: <BrandIcon brand="jira" className="h-5 w-5" />,
  "issue-sync": <Inbox className="h-5 w-5" aria-hidden />,
};

/**
 * The first-run step after the toolchain: which integrations to set up, then
 * their own Settings → Integrations cards, inline, for whatever the person has
 * at hand. Nothing here is required, and either stage can be skipped. GitHub
 * is left to its own numbered step.
 */
export function IntegrationsStep({ onDone }: { onDone: () => void }) {
  const { t } = useI18n();
  const team = useEnabledAgentSlugs();
  const [stage, setStage] = useState<"choose" | "setup">("choose");

  const finish = useCallback(() => {
    writeFirstRunIntegrationsDone();
    onDone();
  }, [onDone]);

  const choices = useMemo(() => integrationChoices(team.slugs), [team.slugs]);

  // Null until the person changes something, so a roster that arrives late
  // still gets its recommendations checked.
  const [picked, setPicked] = useState<ReadonlySet<SetupIntegrationId> | null>(null);
  const selected = useMemo(
    () => orderedSelection(picked ?? preselectedIntegrations(choices)),
    [picked, choices],
  );

  const toggle = (id: SetupIntegrationId, on: boolean) => {
    const next = new Set(selected);
    if (on) next.add(id);
    else next.delete(id);
    setPicked(next);
  };

  // The button that moved between stages is gone; the new stage's heading
  // takes focus so keyboard and screen-reader users land on it.
  const headingRef = useRef<HTMLHeadingElement>(null);
  const stageChanged = useRef(false);
  useEffect(() => {
    if (stageChanged.current) headingRef.current?.focus();
    stageChanged.current = true;
  }, [stage]);

  const skip = (
    <Button variant="ghost" onClick={finish}>
      {t("setup.integrations.skip")}
    </Button>
  );

  if (stage === "setup") {
    return (
      <div className="space-y-4">
        <div className="space-y-1">
          <h2 ref={headingRef} tabIndex={-1} className="text-base font-semibold outline-none">
            {t("setup.integrations.setUpTitle")}
          </h2>
          <p className="text-sm text-muted-foreground">{t("setup.integrations.setUpHint")}</p>
        </div>

        <SelectedIntegrationCards ids={selected} />

        <div className="flex flex-wrap gap-2">
          <Button onClick={finish}>{t("setup.integrations.continue")}</Button>
          <Button variant="outline" onClick={() => setStage("choose")}>
            {t("setup.integrations.back")}
          </Button>
          {skip}
        </div>
      </div>
    );
  }

  const teamKnown = (team.loaded && !team.preparing) || Boolean(team.error);

  return (
    <div className="space-y-4">
      <div className="space-y-1">
        <h2 ref={headingRef} tabIndex={-1} className="text-base font-semibold outline-none">
          {t("setup.integrations.chooseTitle")}
        </h2>
        <p className="text-sm text-muted-foreground">{t("setup.integrations.chooseHint")}</p>
      </div>

      {team.error && (
        <Notice variant="error" title={t("setup.integrations.teamLoadFailed")}>
          <p>{team.error}</p>
          <Button variant="outline" size="sm" className="mt-2" onClick={() => void team.reload()}>
            {t("common.refresh")}
          </Button>
        </Notice>
      )}

      {teamKnown ? (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {choices.map(({ integration, recommended }) => (
            <IntegrationChoiceTile
              key={integration.id}
              id={integration.id}
              icon={ICONS[integration.id]}
              name={t(integration.nameKey)}
              description={t(integration.descriptionKey)}
              recommended={recommended}
              checked={selected.includes(integration.id)}
              onCheckedChange={(on) => toggle(integration.id, on)}
            />
          ))}
        </div>
      ) : (
        <Skeleton className="h-40 rounded-xl" aria-busy="true" />
      )}

      <div className="flex flex-wrap gap-2">
        <Button disabled={!teamKnown || selected.length === 0} onClick={() => setStage("setup")}>
          {t("setup.integrations.setUpSelected")}
        </Button>
        {skip}
      </div>
    </div>
  );
}

/** The picked integrations' own Settings cards, in display order. */
function SelectedIntegrationCards({ ids }: { ids: readonly SetupIntegrationId[] }) {
  const asc = ids.includes("asc");
  const play = ids.includes("google-play");
  return (
    <div className="space-y-4">
      {(asc || play) && <StoreIntegrationCards asc={asc} play={play} />}
      {ids.includes("cloud") && <CloudAccountsCard />}
      {ids.includes("jira") && <JiraCard />}
      {ids.includes("issue-sync") && <IssueSyncCard />}
    </div>
  );
}

function StoreIntegrationCards({ asc, play }: { asc: boolean; play: boolean }) {
  const { t } = useI18n();
  const { loading, error, reload, credentialFor } = useStoreCredentials();
  const refresh = () => void reload();

  if (loading) {
    return (
      <>
        {asc && <Skeleton className="h-20 w-full rounded-xl" />}
        {play && <Skeleton className="h-20 w-full rounded-xl" />}
      </>
    );
  }

  return (
    <>
      {error && (
        <Notice variant="error" title={t("settingsPages.integrations.loadFailed")}>
          <p>{error}</p>
          <Button variant="outline" size="sm" className="mt-2" onClick={refresh}>
            {t("common.refresh")}
          </Button>
        </Notice>
      )}
      {asc && <AppStoreConnectCard credential={credentialFor("asc")} onChanged={refresh} />}
      {play && <GooglePlayCard credential={credentialFor("google_play")} onChanged={refresh} />}
    </>
  );
}
