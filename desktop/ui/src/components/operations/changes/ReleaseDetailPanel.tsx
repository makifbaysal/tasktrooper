import { CircleCheck, ExternalLink, Rocket, Undo2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import { api, type Release } from "@/api";
import {
  changeDurationMs,
  changeStartedAt,
  changeTrigger,
  releaseActions,
  type ChangeOwner,
} from "@/components/operations/changes/changeFeed";
import { ReleaseChecksSection } from "@/components/operations/changes/ReleaseChecksSection";
import { ReleaseImpactSection } from "@/components/operations/changes/ReleaseImpactSection";
import { ReleaseStatusPill } from "@/components/operations/changes/ReleaseStatusPill";
import { RollbackSuggestionBanner } from "@/components/operations/changes/RollbackSuggestionBanner";
import { DetailFacts, DetailPanel, DetailSection, type DetailFact } from "@/components/operations/console/DetailPanel";
import { entityPath, releaseLabel } from "@/components/operations/console/opsEntities";
import { useOpsHref } from "@/components/operations/shell/OpsContext";
import { storeTestPlatformLabel } from "@/components/operations/storeTestBuilds";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { useReleaseDetail } from "@/hooks/useOpsChanges";
import { releaseEnvironment } from "@/lib/deployFeed";
import { githubCommitUrl } from "@/lib/githubUrl";
import { formatDate, formatDuration, formatRelativeTime } from "@/lib/utils";

export type ReleaseAction = "rollback" | "deploy" | "finish";

interface ReleaseDetailPanelProps {
  releaseId: string | null;
  listed?: Release;
  ownerOf: (release: Release) => ChangeOwner;
  onOpenChange: (open: boolean) => void;
  /** The server's answer to an action, for the list behind the panel. */
  onChanged: (release: Release) => void;
}

/** One change in full: its timeline, the deploy watch, what verification
 * recorded, the rollback, and the actions its state allows. The buttons are
 * a hint; the server's refusal is what governs, and it is shown as said. */
export function ReleaseDetailPanel({ releaseId, listed, ownerOf, onOpenChange, onChanged }: ReleaseDetailPanelProps) {
  const { t } = useI18n();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.changes.detail.${key}`, params);
  const { release, loading, error, apply } = useReleaseDetail(releaseId, listed);
  const owner = release ? ownerOf(release) : undefined;

  let title: ReactNode = d("loading");
  if (release) title = <span className="font-mono">{releaseLabel(release)}</span>;
  else if (!loading && error) title = d("notFound");

  return (
    <DetailPanel
      open={releaseId !== null}
      onOpenChange={onOpenChange}
      title={title}
      description={release && owner ? <ReleaseSubtitle release={release} owner={owner} /> : undefined}
      badges={
        release ? (
          <>
            <ReleaseStatusPill status={release.status} />
            <Badge variant="outline" className="px-2 py-0 text-micro">{t(`release.modes.${release.mode}`)}</Badge>
            {release.executor && (
              <Badge variant="outline" className="px-2 py-0 text-micro">{t(`release.executors.${release.executor}`)}</Badge>
            )}
          </>
        ) : undefined
      }
      actions={
        release && owner ? (
          <ReleaseActions
            key={release.id}
            release={release}
            repositoryName={owner.repositoryName}
            onDone={(updated) => {
              apply(updated);
              onChanged(updated);
            }}
          />
        ) : undefined
      }
    >
      {!release && loading && (
        <div className="space-y-3" aria-busy>
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      )}
      {!release && !loading && error && (
        <Notice variant="error" title={d("notFound")}>
          <p className="break-words text-caption">{error}</p>
        </Notice>
      )}
      {release && owner && (
        <ReleaseBody
          release={release}
          owner={owner}
          onChanged={(updated) => {
            apply(updated);
            onChanged(updated);
          }}
        />
      )}
    </DetailPanel>
  );
}

function ReleaseSubtitle({ release, owner }: { release: Release; owner: ChangeOwner }) {
  const { t } = useI18n();
  const env = releaseEnvironment(release);
  return (
    <>
      {owner.name}
      <span aria-hidden> · </span>
      {env ? t(`cloud.environments.${env}`) : release.deploy?.env}
    </>
  );
}

interface ReleaseActionsProps {
  release: Release;
  repositoryName?: string;
  onDone: (release: Release) => void;
}

function ReleaseActions({ release, repositoryName, onDone }: ReleaseActionsProps) {
  const { t } = useI18n();
  const a = (key: string, params?: Record<string, string | number>) => t(`operations.changes.actions.${key}`, params);
  const [pending, setPending] = useState<ReleaseAction | null>(null);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | null>(null);
  const allowed = releaseActions(release, Date.now());
  const version = releaseLabel(release);
  const ready = !!repositoryName;

  const open = (action: ReleaseAction) => {
    setNote("");
    setRefusal(null);
    setPending(action);
  };

  // Every release write carries what the person typed, whatever the
  // environment: a staging rollback reverts merges on the default branch,
  // which can redeploy production. A refusal keeps the dialog and its note.
  const run = async (typed: string): Promise<boolean> => {
    if (!pending) return true;
    const action = pending;
    setBusy(true);
    try {
      const updated =
        action === "deploy"
          ? await api.deployRelease(release.id, typed)
          : action === "finish"
            ? await api.finishRelease(release.id, typed, note.trim())
            : await api.rollbackRelease(release.id, typed, note.trim());
      setRefusal(null);
      onDone(updated);
      toast.success(a(`done.${action}`));
      return true;
    } catch (err) {
      setRefusal(err instanceof Error ? err.message : t("common.actionFailed"));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const nothing = !allowed.deploy && !allowed.finish && !allowed.rollback;
  const description =
    pending === "rollback"
      ? a("rollbackDescription")
      : pending === "deploy"
        ? a("deployDescription")
        : release.status === "failed"
          ? a("finishFailedDescription")
          : a("finishDescription");

  return (
    <div className="w-full space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        {allowed.rollback && (
          <Button size="sm" variant="outline" disabled={!ready} onClick={() => open("rollback")} className="border-destructive/40 text-destructive hover:text-destructive">
            <Undo2 aria-hidden />
            {a("rollback")}
          </Button>
        )}
        {allowed.deploy && (
          <Button size="sm" disabled={!ready} onClick={() => open("deploy")}>
            <Rocket aria-hidden />
            {a("deploy")}
          </Button>
        )}
        {allowed.finish && (
          <Button size="sm" variant="outline" disabled={!ready} onClick={() => open("finish")}>
            <CircleCheck aria-hidden />
            {a("finish")}
          </Button>
        )}
        {nothing && (
          <p className="text-caption text-muted-foreground">{allowed.rollbackExpired ? a("rollbackExpired") : a("none")}</p>
        )}
      </div>
      {refusal && pending === null && (
        <Notice variant="error" title={a("refused")}>
          <p className="break-words text-caption">{refusal}</p>
        </Notice>
      )}
      <ConfirmDialog
        open={pending !== null}
        onOpenChange={(next) => {
          if (!next) setPending(null);
        }}
        title={pending ? a(`${pending}Title`, { version }) : ""}
        description={description}
        confirmLabel={pending ? a(pending) : undefined}
        variant={pending === "rollback" ? "destructive" : "default"}
        loading={busy}
        confirmPhrase={repositoryName}
        confirmDisabled={!ready || (pending === "rollback" && note.trim() === "")}
        error={refusal}
        errorTitle={a("refused")}
        onConfirm={run}
      >
        {pending && pending !== "deploy" && (
          <div className="space-y-2">
            <Label htmlFor="release-action-note">{a(`noteLabel.${pending}`)}</Label>
            <Textarea
              id="release-action-note"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder={a(`notePlaceholder.${pending}`)}
              rows={3}
            />
          </div>
        )}
      </ConfirmDialog>
    </div>
  );
}

interface TimelineEntry {
  key: string;
  label: string;
  at: string;
  future?: boolean;
}

function timelineOf(r: Release, d: (key: string) => string, now: number): TimelineEntry[] {
  const entries: TimelineEntry[] = [{ key: "created", label: d("timeline.created"), at: r.created_at }];
  if (r.cut_at) entries.push({ key: "cut", label: d("timeline.cut"), at: r.cut_at });
  if (r.deploy_started_at) entries.push({ key: "deployStarted", label: d("timeline.deployStarted"), at: r.deploy_started_at });
  if (r.deployed_at) entries.push({ key: "deployed", label: d("timeline.deployed"), at: r.deployed_at });
  if (r.verify_until) {
    entries.push({ key: "verifyUntil", label: d("timeline.verifyUntil"), at: r.verify_until, future: Date.parse(r.verify_until) > now });
  }
  if (r.rollback?.started_at) entries.push({ key: "rollback", label: d("timeline.rollbackStarted"), at: r.rollback.started_at });
  if (r.finished_at) entries.push({ key: "finished", label: d("timeline.finished"), at: r.finished_at });
  return entries.sort((x, y) => Date.parse(x.at) - Date.parse(y.at));
}

function ReleaseBody({ release: r, owner, onChanged }: { release: Release; owner: ChangeOwner; onChanged: (release: Release) => void }) {
  const { t, lang } = useI18n();
  const href = useOpsHref();
  const d = (key: string, params?: Record<string, string | number>) => t(`operations.changes.detail.${key}`, params);
  const now = Date.now();
  const env = releaseEnvironment(r);
  const sha = r.commit_sha;
  const commitUrl = githubCommitUrl(owner.repository?.remoteUrl, sha);
  const duration = changeDurationMs(r);
  const tasks = r.tasks ?? [];
  const started = changeStartedAt(r);
  const when = (iso?: string) =>
    iso ? (
      <span title={formatDate(iso)}>
        <span className="font-mono tabular-nums">{formatDate(iso)}</span>
        <span className="text-muted-foreground"> · {formatRelativeTime(iso, lang)}</span>
      </span>
    ) : (
      <span className="text-muted-foreground">—</span>
    );

  const facts: DetailFact[] = [
    {
      label: d("entity"),
      value: owner.entity ? (
        <Link to={href(entityPath(owner.entity.id))} className="font-medium text-info hover:underline">
          {owner.name}
        </Link>
      ) : (
        owner.name || "—"
      ),
    },
    { label: d("repository"), value: owner.repositoryName ?? "—" },
    {
      label: d("environment"),
      value: (
        <span>
          {env ? t(`cloud.environments.${env}`) : d("unknownEnvironment")}
          {r.deploy?.env && <span className="ml-1 font-mono text-muted-foreground">({r.deploy.env})</span>}
        </span>
      ),
    },
    {
      label: d("commit"),
      value: sha ? (
        commitUrl ? (
          <a href={commitUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 font-mono text-info hover:underline">
            {sha.slice(0, 12)}
            <ExternalLink className="h-3 w-3" aria-hidden />
          </a>
        ) : (
          <span className="font-mono">{sha.slice(0, 12)}</span>
        )
      ) : (
        "—"
      ),
    },
    ...(r.tag ? [{ label: d("tag"), value: <span className="font-mono">{r.tag}</span> }] : []),
    {
      label: d("trigger"),
      value: (
        <span>
          {t(`operations.changes.trigger.${changeTrigger(r)}`)}
          {r.executor && <span className="text-muted-foreground"> · {t(`release.executors.${r.executor}`)}</span>}
        </span>
      ),
    },
    { label: d("started"), value: when(started) },
    { label: d("finished"), value: when(r.finished_at) },
    {
      label: d("duration"),
      value: duration !== undefined ? <span className="font-mono tabular-nums">{formatDuration(duration, lang)}</span> : "—",
    },
    {
      label: d("tasks"),
      value:
        tasks.length === 0 ? (
          <span className="text-muted-foreground">{d("noTasks")}</span>
        ) : (
          <ul className="space-y-0.5">
            {tasks.map((task) => (
              <li key={task.id} className="min-w-0 truncate">
                <Link to={`/board?task=${encodeURIComponent(task.id)}`} className="font-mono text-info hover:underline">
                  {task.key ?? task.id.slice(0, 8)}
                </Link>
                {task.title && <span className="ml-1.5 text-muted-foreground">{task.title}</span>}
              </li>
            ))}
          </ul>
        ),
    },
  ];

  const timeline = timelineOf(r, d, now);
  const watch = r.deploy;
  const rollback = r.rollback;

  return (
    <>
      <RollbackSuggestionBanner release={r} repositoryName={owner.repositoryName} onDone={onChanged} />
      {!r.rollback_proposal && r.rollback_dismissal?.dismissed_at && (
        <p className="text-caption text-muted-foreground" title={formatDate(r.rollback_dismissal.dismissed_at)}>
          {t("operations.changes.rollbackSuggestion.dismissedAt", { time: formatRelativeTime(r.rollback_dismissal.dismissed_at, lang) })}
        </p>
      )}

      {r.status === "failed" && r.failure_reason && (
        <Notice variant="error" title={d("failure")}>
          <p className="break-words text-caption">{r.failure_reason}</p>
        </Notice>
      )}

      <DetailSection title={d("facts")}>
        <DetailFacts items={facts} />
      </DetailSection>

      <ReleaseImpactSection release={r} role={owner.entity?.role} entityId={owner.entity?.id ?? r.component_id} />

      <DetailSection title={d("timeline.title")}>
        <ol className="relative space-y-2 border-l border-border pl-4">
          {timeline.map((entry) => (
            <li key={entry.key} className="relative text-caption">
              <span
                aria-hidden
                className={
                  entry.future
                    ? "absolute top-1.5 -left-[21px] h-2 w-2 rounded-full border border-muted-foreground bg-background"
                    : "absolute top-1.5 -left-[21px] h-2 w-2 rounded-full bg-muted-foreground"
                }
              />
              <span className="font-medium">{entry.label}</span>
              <span className="ml-2 font-mono text-muted-foreground tabular-nums" title={formatDate(entry.at)}>
                {formatDate(entry.at)}
              </span>
            </li>
          ))}
          <li className="relative text-caption">
            <span aria-hidden className="absolute top-1.5 -left-[21px] h-2 w-2 rounded-full bg-info" />
            <span className="text-muted-foreground">{d("timeline.now")}</span> <ReleaseStatusPill status={r.status} />
          </li>
        </ol>
      </DetailSection>

      <DetailSection title={d("watch.title")}>
        {watch ? (
          <DetailFacts
            items={[
              { label: d("watch.state"), value: d(`watch.states.${watch.state}`) },
              ...(watch.signal ? [{ label: d("watch.signal"), value: <span className="font-mono">{watch.signal}</span> }] : []),
              ...(watch.detail ? [{ label: d("watch.detail"), value: watch.detail }] : []),
              ...(watch.failed_job
                ? [
                    {
                      label: d("watch.failedJob"),
                      value: watch.failed_job.url ? (
                        <a href={watch.failed_job.url} target="_blank" rel="noreferrer" className="text-info hover:underline">
                          {watch.failed_job.name}
                        </a>
                      ) : (
                        watch.failed_job.name
                      ),
                    },
                  ]
                : []),
              ...(watch.run_url
                ? [
                    {
                      label: d("watch.run"),
                      value: (
                        <a href={watch.run_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-info hover:underline">
                          {d("watch.openRun")}
                          <ExternalLink className="h-3 w-3" aria-hidden />
                        </a>
                      ),
                    },
                  ]
                : []),
              { label: d("watch.autoRollback"), value: watch.auto_rollback ? d("watch.on") : d("watch.off") },
              ...(watch.health_window_until ? [{ label: d("watch.healthWindow"), value: when(watch.health_window_until) }] : []),
              { label: d("watch.checked"), value: when(watch.checked_at) },
            ]}
          />
        ) : (
          <p className="text-caption text-muted-foreground">{d("watch.none")}</p>
        )}
      </DetailSection>

      <ReleaseChecksSection checks={r.checks} status={r.status} />

      {rollback && (
        <DetailSection title={d("rollback.title")}>
          <DetailFacts
            items={[
              { label: d("rollback.reason"), value: d(`rollback.reasons.${rollback.reason}`) },
              ...(rollback.actor ? [{ label: d("rollback.actor"), value: actorLabel(d, rollback.actor) }] : []),
              ...(rollback.mechanism ? [{ label: d("rollback.mechanism"), value: d(`rollback.mechanisms.${rollback.mechanism}`) }] : []),
              { label: d("rollback.started"), value: when(rollback.started_at) },
              ...(rollback.revert_sha
                ? [
                    {
                      label: d("rollback.revert"),
                      value: (() => {
                        const url = githubCommitUrl(owner.repository?.remoteUrl, rollback.revert_sha);
                        const text = rollback.revert_sha.slice(0, 12);
                        return url ? (
                          <a href={url} target="_blank" rel="noreferrer" className="font-mono text-info hover:underline">
                            {text}
                          </a>
                        ) : (
                          <span className="font-mono">{text}</span>
                        );
                      })(),
                    },
                  ]
                : []),
              ...(rollback.restored_ref ? [{ label: d("rollback.restored"), value: <span className="font-mono">{rollback.restored_ref}</span> }] : []),
              ...(rollback.run_url
                ? [
                    {
                      label: d("rollback.run"),
                      value: (
                        <a href={rollback.run_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-info hover:underline">
                          {d("watch.openRun")}
                          <ExternalLink className="h-3 w-3" aria-hidden />
                        </a>
                      ),
                    },
                  ]
                : []),
              ...(rollback.note ? [{ label: d("rollback.note"), value: rollback.note }] : []),
              ...(rollback.detail ? [{ label: d("rollback.detail"), value: rollback.detail }] : []),
            ]}
          />
          {(rollback.manual_steps ?? []).length > 0 && (
            <Notice variant="warning" title={d("rollback.manualSteps")}>
              <ul className="list-disc space-y-0.5 pl-5 text-caption">
                {rollback.manual_steps!.map((step) => (
                  <li key={step}>{step}</li>
                ))}
              </ul>
            </Notice>
          )}
        </DetailSection>
      )}

      {r.verdict && (
        <DetailSection title={d("verdict")}>
          <p className="text-caption whitespace-pre-wrap">{r.verdict}</p>
        </DetailSection>
      )}

      {r.local_run && (
        <DetailSection title={d("localRun.title")}>
          <DetailFacts
            items={[
              { label: d("localRun.command"), value: <span className="font-mono break-all">{r.local_run.argv.join(" ")}</span> },
              ...(r.local_run.exit_code !== undefined
                ? [{ label: d("localRun.exitCode"), value: <span className="font-mono">{r.local_run.exit_code}</span> }]
                : []),
              ...(r.local_run.error ? [{ label: d("localRun.error"), value: r.local_run.error }] : []),
            ]}
          />
          {r.local_run.tail && (
            <pre className="max-h-56 overflow-auto rounded-md border border-border bg-surface-sunken p-2 font-mono text-micro whitespace-pre-wrap">
              {r.local_run.tail}
            </pre>
          )}
        </DetailSection>
      )}

      {(r.store_builds ?? []).length > 0 && (
        <DetailSection title={d("storeBuilds.title")}>
          <ul className="divide-y divide-border rounded-md border border-border text-caption">
            {r.store_builds!.map((b) => (
              <li key={b.platform} className="flex flex-wrap items-center gap-x-4 gap-y-0.5 px-3 py-2">
                <span className="font-medium">{storeTestPlatformLabel(t, b.platform)}</span>
                <span className="font-mono text-muted-foreground">
                  {d("storeBuilds.baseline")}: {b.baseline_build ?? "—"}
                </span>
                <span className="font-mono">
                  {d("storeBuilds.build")}: {b.build ?? "—"}
                </span>
                {b.error && <span className="w-full text-destructive">{b.error}</span>}
              </li>
            ))}
          </ul>
        </DetailSection>
      )}

      {r.notes && (
        <DetailSection title={d("notes")}>
          <MarkdownContent content={r.notes} className="text-caption" />
        </DetailSection>
      )}
    </>
  );
}

const ACTORS = ["agent", "human", "system"];

function actorLabel(d: (key: string) => string, actor: string): string {
  return ACTORS.includes(actor) ? d(`rollback.actors.${actor}`) : actor;
}
