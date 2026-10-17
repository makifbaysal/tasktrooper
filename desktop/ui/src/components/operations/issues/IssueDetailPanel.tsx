import { Ban, CircleCheck, PencilLine, Stethoscope } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type Incident, type IncidentRemedyInput } from "@/api";
import { incidentRollbackRelease } from "@/components/operations/changes/rollbackSuggestion";
import { RollbackSuggestionBanner } from "@/components/operations/changes/RollbackSuggestionBanner";
import { DetailFacts, DetailPanel, DetailSection, type DetailFact } from "@/components/operations/console/DetailPanel";
import { entityPath } from "@/components/operations/console/opsEntities";
import { isOpenIncident } from "@/components/operations/console/opsStatus";
import { IssueSeverity, IssueStatusBadge } from "@/components/operations/issues/IssueSeverity";
import { issueDurationMs } from "@/components/operations/issues/issueList";
import { issueEnvironmentLabel, type IssueOwner } from "@/components/operations/issues/IssueTable";
import { RemedyDialog } from "@/components/operations/issues/RemedyDialog";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { useOpsDirectory } from "@/components/operations/shell/useOpsEntities";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { useOpsConsole } from "@/hooks/useOpsConsole";
import { useIncidentDetail } from "@/hooks/useOpsIssues";
import { formatDate, formatDuration, formatRelativeTime } from "@/lib/utils";

const SOURCES = ["webhook", "probe", "deploy", "manual", "alert_rule"];
const REMEDY_KINDS = ["rollback", "code_fix", "config", "dependency", "capacity", "unknown"];
const REMEDY_AUTHORS = ["auto_triage", "agent", "human"];
const EVENT_KINDS = ["detected", "recurred", "triaged", "proposed", "task_created", "notified", "resolved", "ignored", "note"];

type Translate = (key: string, params?: Record<string, string | number>) => string;

function known(t: Translate, list: string[], prefix: string, value: string | undefined): string {
  if (!value) return "—";
  return list.includes(value) ? t(`${prefix}.${value}`) : value;
}

// An alert rule writes its id and component into the issue it opens
// (alerting/evaluate.go#incidentInput).
function alertRuleOf(i: Incident): { ruleId: string; componentId: string; name?: string } | undefined {
  if (i.source !== "alert_rule") return undefined;
  const ruleId = i.payload?.rule_id;
  const componentId = i.payload?.component_id;
  if (typeof ruleId !== "string" || typeof componentId !== "string") return undefined;
  const name = i.payload?.rule_name;
  return { ruleId, componentId, name: typeof name === "string" ? name : undefined };
}

interface IssueDetailPanelProps {
  incidentId: string | null;
  listed?: Incident;
  ownerOf: (incident: Incident) => IssueOwner;
  onOpenChange: (open: boolean) => void;
  /** After any action, so the list behind the panel reads again. */
  onChanged: () => void;
  /** Once the opened issue is known (listed or read). */
  onLoaded?: (incident: Incident) => void;
}

/** One issue in full: what fired, how often, the remedy on it and who wrote
 * it, the raw alert and its timeline, with Triage / Resolve / Ignore and a
 * hand-written remedy while it is live. */
export function IssueDetailPanel({ incidentId, listed, ownerOf, onOpenChange, onChanged, onLoaded }: IssueDetailPanelProps) {
  const { t } = useI18n();
  const d = (key: string) => t(`operations.issues.detail.${key}`);
  const { incident, loading, error, apply, reload } = useIncidentDetail(incidentId, listed);

  const loadedRef = useRef(onLoaded);
  loadedRef.current = onLoaded;
  useEffect(() => {
    if (incident) loadedRef.current?.(incident);
  }, [incident]);

  let title: ReactNode = d("loading");
  if (incident) title = incident.title;
  else if (!loading && error) title = d("notFound");

  return (
    <DetailPanel
      open={incidentId !== null}
      onOpenChange={onOpenChange}
      title={title}
      description={incident ? <span className="font-mono">{incident.fingerprint}</span> : undefined}
      badges={
        incident ? (
          <>
            <Badge variant="outline" className="px-2 py-0">
              <IssueSeverity severity={incident.severity} className="text-micro" />
            </Badge>
            <IssueStatusBadge status={incident.status} />
            <Badge variant="outline" className="px-2 py-0 text-micro" title={incident.env}>
              {issueEnvironmentLabel(t, incident.env)}
            </Badge>
          </>
        ) : undefined
      }
      actions={
        incident ? (
          <IssueActions
            key={incident.id}
            incident={incident}
            onDone={(updated) => {
              apply(updated);
              void reload();
              onChanged();
            }}
          />
        ) : undefined
      }
    >
      {!incident && loading && (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      )}
      {!incident && !loading && error && (
        <Notice variant="error" title={d("notFound")}>
          <p className="break-words text-caption">{error}</p>
        </Notice>
      )}
      {incident && <IssueBody incident={incident} owner={ownerOf(incident)} onChanged={onChanged} />}
    </DetailPanel>
  );
}

type IssueAction = "triage" | "resolve" | "ignore" | "remedy";

function IssueActions({ incident, onDone }: { incident: Incident; onDone: (incident: Incident) => void }) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.issues.actions.${key}`, params);
  const [confirming, setConfirming] = useState<"resolve" | "ignore" | null>(null);
  const [retriaging, setRetriaging] = useState(false);
  const [remedyOpen, setRemedyOpen] = useState(false);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<IssueAction | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);

  if (!isOpenIncident(incident)) {
    return <p className="text-caption text-muted-foreground">{t("operations.issues.detail.terminal")}</p>;
  }

  const run = async (action: IssueAction, call: () => Promise<Incident>): Promise<boolean> => {
    setBusy(action);
    try {
      const updated = await call();
      setRefusal(null);
      onDone(updated);
      toast.success(a(`done.${action}`));
      return true;
    } catch (err) {
      const message = err instanceof Error ? err.message : t("common.actionFailed");
      setRefusal(message);
      toast.error(message);
      return false;
    } finally {
      setBusy(null);
    }
  };

  const open = (action: "resolve" | "ignore") => {
    setNote("");
    setConfirming(action);
  };

  // Triage always writes the rules engine's proposal over the current one; a
  // person's or an agent's is only replaced once asked.
  const writtenBy = incident.remedy ? incident.remedy_author : undefined;
  const replacesWritten = writtenBy === "human" || writtenBy === "agent";
  const triage = () => run("triage", () => api.triageIncident(incident.id));

  return (
    <div className="w-full space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={busy !== null}
          title={a("triageHint")}
          onClick={() => (replacesWritten ? setRetriaging(true) : void triage())}
        >
          <Stethoscope aria-hidden />
          {busy === "triage" ? a("triaging") : a("triage")}
        </Button>
        <Button size="sm" variant="outline" disabled={busy !== null} onClick={() => setRemedyOpen(true)}>
          <PencilLine aria-hidden />
          {a("writeRemedy")}
        </Button>
        <Button size="sm" disabled={busy !== null} onClick={() => open("resolve")}>
          <CircleCheck aria-hidden />
          {a("resolve")}
        </Button>
        <Button size="sm" variant="ghost" disabled={busy !== null} onClick={() => open("ignore")}>
          <Ban aria-hidden />
          {a("ignore")}
        </Button>
      </div>
      {refusal && (
        <Notice variant="error" title={a("refused")}>
          <p className="break-words text-caption">{refusal}</p>
        </Notice>
      )}
      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(next) => {
          if (!next) setConfirming(null);
        }}
        title={confirming ? a(`${confirming}Title`) : ""}
        description={confirming === "ignore" ? a("ignoreDescription") : a("resolveDescription")}
        confirmLabel={confirming ? a(confirming) : undefined}
        variant={confirming === "ignore" ? "destructive" : "default"}
        loading={busy === "resolve" || busy === "ignore"}
        onConfirm={async () => {
          if (!confirming) return;
          const action = confirming;
          const text = note.trim();
          await run(action, () => (action === "ignore" ? api.ignoreIncident(incident.id, text) : api.resolveIncident(incident.id, text)));
        }}
      >
        <div className="space-y-2">
          <Label htmlFor="issue-action-note">{a("noteLabel")}</Label>
          <Textarea
            id="issue-action-note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={a("notePlaceholder")}
            rows={3}
          />
        </div>
      </ConfirmDialog>
      <ConfirmDialog
        open={retriaging}
        onOpenChange={(next) => {
          if (!next) setRetriaging(false);
        }}
        title={a("retriageTitle")}
        description={writtenBy === "agent" ? a("retriageAgent") : a("retriageHuman")}
        confirmLabel={a("retriageConfirm")}
        variant="destructive"
        loading={busy === "triage"}
        onConfirm={async () => {
          await triage();
          setRetriaging(false);
        }}
      />
      {remedyOpen && (
        <RemedyDialog
          incident={incident}
          open={remedyOpen}
          busy={busy === "remedy"}
          onOpenChange={setRemedyOpen}
          onSave={async (remedy: IncidentRemedyInput) => {
            if (await run("remedy", () => api.proposeIncidentRemedy(incident.id, remedy))) setRemedyOpen(false);
          }}
        />
      )}
    </div>
  );
}

function IssueBody({ incident: i, owner, onChanged }: { incident: Incident; owner: IssueOwner; onChanged: () => void }) {
  const { t, lang } = useI18n();
  const href = useOpsHref();
  const { releases, reload: reloadConsole } = useOpsConsole();
  const { repositories } = useOpsDirectory();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.issues.detail.${key}`, params);
  const now = Date.now();
  const when = (iso?: string) =>
    iso ? (
      <span title={formatDate(iso)}>
        <span className="font-mono tabular-nums">{formatDate(iso)}</span>
        <span className="text-muted-foreground"> · {formatRelativeTime(iso, lang, now)}</span>
      </span>
    ) : (
      <span className="text-muted-foreground">—</span>
    );

  const alertRule = alertRuleOf(i);
  const facts: DetailFact[] = [
    {
      label: d("entity"),
      value:
        owner.entities.length > 0 ? (
          <span className="flex flex-wrap gap-x-2">
            {owner.entities.map((e) => (
              <Link key={e.id} to={href(entityPath(e.id, "issues"))} className="font-medium text-info hover:underline">
                {e.name}
              </Link>
            ))}
          </span>
        ) : (
          <span className="text-muted-foreground">{d("noEntity")}</span>
        ),
    },
    { label: d("repository"), value: owner.repositoryName ?? t("operations.issues.unknownRepository") },
    {
      label: d("environment"),
      value: (
        <span>
          {issueEnvironmentLabel(t, i.env)}
          <span className="ml-1 font-mono text-muted-foreground">({i.env || "—"})</span>
        </span>
      ),
    },
    { label: d("source"), value: known(t, SOURCES, "operations.issues.detail.sources", i.source) },
    ...(alertRule
      ? [
          {
            label: d("alertRule"),
            value: (
              <Link to={href(`${entityPath(alertRule.componentId, "alerts")}?rule=${encodeURIComponent(alertRule.ruleId)}`)} className="text-info hover:underline">
                {alertRule.name || d("openAlertRule")}
              </Link>
            ),
          },
        ]
      : []),
    { label: d("firstSeen"), value: when(i.first_seen_at) },
    { label: d("lastSeen"), value: when(i.last_seen_at) },
    ...(i.resolved_at ? [{ label: d("resolved"), value: when(i.resolved_at) }] : []),
    { label: d("duration"), value: <span className="font-mono tabular-nums">{formatDuration(issueDurationMs(i, now), lang)}</span> },
    { label: d("occurrences"), value: <span className="font-mono tabular-nums">{i.occurrences}</span> },
    {
      label: d("task"),
      value: i.task_id ? (
        <Link to={`/board?task=${encodeURIComponent(i.task_id)}`} className="text-info hover:underline">
          {d("openTask")}
        </Link>
      ) : (
        <span className="text-muted-foreground">{d("noTask")}</span>
      ),
    },
  ];

  const events = [...(i.events ?? [])].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
  const payload = i.payload && Object.keys(i.payload).length > 0 ? JSON.stringify(i.payload, null, 2) : null;
  const confidence = Math.max(0, Math.min(100, i.confidence ?? 0));
  const suspect = incidentRollbackRelease(i, releases);

  return (
    <>
      {suspect && (
        <RollbackSuggestionBanner
          release={suspect}
          repositoryName={repositories.find((repo) => repo.id === suspect.repository_id)?.name ?? (suspect.repository_id === i.repository_id ? owner.repositoryName : undefined)}
          releaseHref={href(`/operations/changes?release=${encodeURIComponent(suspect.id)}`)}
          onDone={() => {
            reloadConsole();
            onChanged();
          }}
        />
      )}

      <DetailSection title={d("facts")}>
        <DetailFacts items={facts} />
      </DetailSection>

      {i.detail && (
        <DetailSection title={d("detail")}>
          <p className="font-mono text-caption break-words whitespace-pre-wrap">{i.detail}</p>
        </DetailSection>
      )}

      <DetailSection title={d("remedy.title")}>
        {i.remedy ? (
          <div className="space-y-3">
            <DetailFacts
              items={[
                { label: d("remedy.kind"), value: known(t, REMEDY_KINDS, "operations.issues.detail.remedy.kinds", i.remedy_kind) },
                {
                  label: d("remedy.confidence"),
                  value: (
                    <span className="flex items-center gap-2">
                      <Progress value={confidence} className="h-1.5 w-24" label={d("remedy.confidence")} />
                      <span className="font-mono tabular-nums">{confidence}%</span>
                    </span>
                  ),
                },
                { label: d("remedy.author"), value: known(t, REMEDY_AUTHORS, "operations.issues.detail.remedy.authors", i.remedy_author) },
              ]}
            />
            <div className="rounded-md border border-border bg-surface-sunken/50 px-3 py-2">
              <MarkdownContent content={i.remedy} className="text-caption" />
            </div>
          </div>
        ) : (
          <p className="text-caption text-muted-foreground">{d("remedy.none")}</p>
        )}
      </DetailSection>

      <DetailSection title={d("timeline.title")}>
        {events.length === 0 ? (
          <p className="text-caption text-muted-foreground">{d("timeline.empty")}</p>
        ) : (
          <ol className="relative space-y-2 border-l border-border pl-4">
            {events.map((e) => (
              <li key={e.id} className="relative text-caption">
                <span aria-hidden className="absolute top-1.5 -left-[21px] h-2 w-2 rounded-full bg-muted-foreground" />
                <span className="font-medium">{known(t, EVENT_KINDS, "operations.issues.detail.timeline.kinds", e.kind)}</span>
                <span className="ml-2 font-mono text-muted-foreground tabular-nums" title={formatDate(e.created_at)}>
                  {formatDate(e.created_at)}
                </span>
                {e.message && <p className="break-words text-muted-foreground">{e.message}</p>}
              </li>
            ))}
          </ol>
        )}
      </DetailSection>

      <DetailSection title={d("payload.title")}>
        {payload ? (
          <Accordion type="single">
            <AccordionItem value="payload">
              <AccordionTrigger className="py-2 text-caption">{d("payload.show")}</AccordionTrigger>
              <AccordionContent>
                <pre className="max-h-80 overflow-auto rounded-md bg-surface-sunken p-2 font-mono text-micro text-foreground whitespace-pre-wrap">
                  {payload}
                </pre>
              </AccordionContent>
            </AccordionItem>
          </Accordion>
        ) : (
          <p className="text-caption text-muted-foreground">{d("payload.empty")}</p>
        )}
      </DetailSection>
    </>
  );
}
