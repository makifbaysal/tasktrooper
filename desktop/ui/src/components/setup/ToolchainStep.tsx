import { useCallback, useEffect, useLayoutEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { ToolchainItemRow } from "@/components/toolchain/ToolchainItemRow";
import { ToolchainManualAction } from "@/components/toolchain/ToolchainOpenPageButton";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useEnabledAgentSlugs } from "@/hooks/useEnabledAgentSlugs";
import { useI18n } from "@/hooks/useI18n";
import { useToolchain } from "@/hooks/useToolchain";
import type { ToolchainId } from "@/lib/desktop-bridge";
import { writeFirstRunToolchainDone } from "@/lib/firstRun";
import {
  installedFor,
  planExtras,
  preselectedIds,
  recommendFor,
  toolchainReason,
  type ToolchainRecommendation,
} from "@/lib/toolchain";
import { isDeclined } from "@/lib/toolchainJob";

/**
 * The first-run step after the team: the developer tools the enabled agents
 * need and this computer lacks, recommended ones checked. Installing does not
 * wait — the job runs in the shell's main process and the header chip follows
 * it — so the sequence moves on as soon as the shell has queued it.
 *
 * Without the installer (a browser, a shell older than bridge 5) there is
 * nothing to offer, and the step marks itself done unseen.
 */
export function ToolchainStep({ onDone }: { onDone: () => void }) {
  const { t } = useI18n();
  const { available, report, reportError, refreshing, refresh, plan, install, openPage } = useToolchain();
  const team = useEnabledAgentSlugs();

  const finish = useCallback(() => {
    writeFirstRunToolchainDone();
    onDone();
  }, [onDone]);

  // Layout, not passive: the parent swaps screens before this one ever paints.
  useLayoutEffect(() => {
    if (!available) finish();
  }, [available, finish]);

  const recommendations = useMemo(() => (report ? recommendFor(team.slugs, report) : []), [report, team.slugs]);
  const alreadyThere = useMemo(() => (report ? installedFor(team.slugs, report) : []), [report, team.slugs]);

  // Null until the person changes something: until then the selection follows
  // the recommendation, so a roster that arrives late still gets its tools checked.
  const [picked, setPicked] = useState<ReadonlySet<ToolchainId> | null>(null);
  const selectedIds = useMemo(() => {
    const chosen = picked ?? new Set(preselectedIds(recommendations));
    return recommendations
      .filter((entry) => entry.installable && chosen.has(entry.id))
      .map((entry) => entry.id);
  }, [picked, recommendations]);

  const toggle = (id: ToolchainId, on: boolean) => {
    const next = new Set(selectedIds);
    if (on) next.add(id);
    else next.delete(id);
    setPicked(next);
  };

  const [extras, setExtras] = useState<string[]>([]);
  useEffect(() => {
    if (selectedIds.length === 0) {
      setExtras([]);
      return;
    }
    let live = true;
    plan(selectedIds).then(
      (planned) => {
        if (live) setExtras(planExtras(planned, selectedIds, report));
      },
      () => {
        if (live) setExtras([]);
      },
    );
    return () => {
      live = false;
    };
  }, [plan, selectedIds, report]);

  const [installing, setInstalling] = useState(false);
  const [declined, setDeclined] = useState(false);
  const [installError, setInstallError] = useState("");

  const installSelected = async () => {
    setInstalling(true);
    setDeclined(false);
    setInstallError("");
    try {
      await install(selectedIds);
      finish();
    } catch (e) {
      if (isDeclined(e)) setDeclined(true);
      else setInstallError(e instanceof Error ? e.message : String(e));
      setInstalling(false);
    }
  };

  const openItemPage = (id: ToolchainId) => {
    openPage(id).catch((e: unknown) => {
      toast.error(t("toolchain.errors.openPageFailed"), { description: e instanceof Error ? e.message : undefined });
    });
  };

  if (!available) return null;

  if (!report && reportError) {
    return (
      <div className="space-y-3">
        <Notice variant="error" title={t("toolchain.errors.reportFailed")}>
          <p>{reportError}</p>
        </Notice>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" disabled={refreshing} onClick={() => void refresh(true)}>
            {t("toolchain.actions.checkAgain")}
          </Button>
          <Button variant="ghost" onClick={finish}>
            {t("setup.toolchain.skip")}
          </Button>
        </div>
      </div>
    );
  }

  const teamKnown = (team.loaded && !team.preparing) || Boolean(team.error);
  if (!report || !teamKnown) {
    return (
      <div className="space-y-3" aria-busy="true">
        <p className="text-sm text-muted-foreground">
          {team.preparing ? t("setup.toolchain.preparing") : t("setup.toolchain.checking")}
        </p>
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }

  const recommended = recommendations.filter((entry) => entry.level === "recommended");
  const optional = recommendations.filter((entry) => entry.level === "optional");

  const anyInstallable = recommendations.some((entry) => entry.installable);

  const row = (entry: ToolchainRecommendation) => {
    const controlId = `toolchain-pick-${entry.id}`;
    const blocker = entry.blockedBy[0];
    return (
      <ToolchainItemRow
        key={entry.id}
        item={entry.item}
        reason={toolchainReason(t, entry.reasons, team.names)}
        hint={blocker ? t("toolchain.needsManual", { label: blocker.label }) : undefined}
        controlId={entry.installable ? controlId : undefined}
        leading={
          entry.installable ? (
            <Checkbox
              id={controlId}
              checked={selectedIds.includes(entry.id)}
              disabled={installing}
              onCheckedChange={(on) => toggle(entry.id, on === true)}
            />
          ) : undefined
        }
        action={entry.installable ? undefined : <ToolchainManualAction entry={entry} onOpenPage={openItemPage} />}
      />
    );
  };

  const installedList =
    alreadyThere.length > 0 ? (
      <Accordion type="single" collapsible>
        <AccordionItem value="installed">
          <AccordionTrigger>{t("setup.toolchain.installed", { count: alreadyThere.length })}</AccordionTrigger>
          <AccordionContent className="px-0 pb-0 text-foreground">
            <div className="divide-y divide-border border-t border-border">
              {alreadyThere.map((entry) => (
                <ToolchainItemRow
                  key={entry.id}
                  item={entry.item}
                  reason={toolchainReason(t, entry.reasons, team.names)}
                  showStatus
                />
              ))}
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    ) : null;

  if (recommendations.length === 0) {
    return (
      <div className="space-y-3">
        <Notice variant="info" title={t("setup.toolchain.nothingMissingTitle")}>
          {t("setup.toolchain.nothingMissingBody")}
        </Notice>
        {installedList}
        <Button onClick={finish}>{t("setup.toolchain.continue")}</Button>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {team.error && (
        <Notice variant="warning" title={t("setup.toolchain.teamLoadFailed")}>
          {team.error}
        </Notice>
      )}

      {recommended.length > 0 && (
        <Card className="divide-y divide-border">
          <p className="px-4 py-3 text-body font-medium">{t("setup.toolchain.recommended")}</p>
          {recommended.map(row)}
        </Card>
      )}

      {optional.length > 0 && (
        <Card className="divide-y divide-border">
          <p className="px-4 py-3 text-body font-medium text-muted-foreground">{t("setup.toolchain.optional")}</p>
          {optional.map(row)}
        </Card>
      )}

      {installedList}

      {extras.length > 0 && (
        <p className="text-sm text-muted-foreground">{t("setup.toolchain.installsFirst", { items: extras.join(", ") })}</p>
      )}

      {declined && (
        <Notice variant="info" title={t("setup.toolchain.declinedTitle")}>
          {t("setup.toolchain.declinedBody")}
        </Notice>
      )}

      {installError && (
        <Notice variant="error" title={t("toolchain.errors.installFailed")}>
          {installError}
        </Notice>
      )}

      {anyInstallable && (
        <Notice variant="info" title={t("setup.toolchain.backgroundTitle")}>
          {t("setup.toolchain.background")}
        </Notice>
      )}

      {anyInstallable ? (
        <div className="flex flex-wrap gap-2">
          <Button disabled={installing || selectedIds.length === 0} onClick={() => void installSelected()}>
            {t("setup.toolchain.install")}
          </Button>
          <Button variant="outline" disabled={installing} onClick={finish}>
            {t("setup.toolchain.skip")}
          </Button>
        </div>
      ) : (
        // Everything left is installed by hand: nothing to start here, so
        // moving on is not a skip.
        <Button onClick={finish}>{t("setup.toolchain.continue")}</Button>
      )}
    </div>
  );
}
