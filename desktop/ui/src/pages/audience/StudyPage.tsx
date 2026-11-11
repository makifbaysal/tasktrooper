import { Loader2, Play, Trash2, XCircle } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ApiError, api, type AudienceStudyDetail, type StudyAnswer, AUDIENCE_NO_RUNTIME } from "@/api";
import { AudienceRuntimeNotice } from "@/components/audience/AudienceRuntimeNotice";
import { PersonaChatDialog } from "@/components/audience/PersonaChatDialog";
import { ReportView } from "@/components/audience/report/ReportView";
import { StudyAnswers } from "@/components/audience/StudyAnswers";
import { StudyFollowUps } from "@/components/audience/StudyFollowUps";
import { PageBackLink } from "@/components/layout/PageBackLink";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { formatMoney, studyIsActive, studyStatusTone } from "@/lib/audienceHub";

const POLL_MS = 3000;
// After a question to everyone the replies arrive one by one; poll for a while.
const FOLLOW_UP_WATCH_MS = 3 * 60 * 1000;

export function StudyPage() {
  const { studyId = "" } = useParams();
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const s = (key: string, params?: Record<string, string | number>) => t(`audienceHub.study.${key}`, params);
  const [study, setStudy] = useState<AudienceStudyDetail | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [noRuntime, setNoRuntime] = useState(false);
  const [talkTo, setTalkTo] = useState<StudyAnswer | null>(null);
  const [runOpen, setRunOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [watchUntil, setWatchUntil] = useState(0);

  const load = useCallback(async () => {
    try {
      setStudy(await api.audienceStudy(studyId));
      setNotFound(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) setNotFound(true);
      else toast.error(e instanceof Error ? e.message : s("loadFailed"));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [studyId]);

  useEffect(() => {
    setStudy(null);
    void load();
  }, [load]);

  const active = study ? studyIsActive(study.status) : false;
  const watching = watchUntil > Date.now();
  usePolling(load, POLL_MS, active || watching, { leading: false });

  const fail = (e: unknown, fallback: string) => {
    if (e instanceof ApiError && e.type === AUDIENCE_NO_RUNTIME) setNoRuntime(true);
    toast.error(e instanceof Error ? e.message : fallback);
  };

  const start = async () => {
    try {
      await api.startAudienceStudy(studyId);
      toast.success(s("started"));
      await load();
    } catch (e) {
      fail(e, s("startFailed"));
      return false;
    }
  };

  const cancel = async () => {
    try {
      await api.cancelAudienceStudy(studyId);
      await load();
    } catch (e) {
      fail(e, s("cancelFailed"));
      return false;
    }
  };

  const remove = async () => {
    try {
      await api.deleteAudienceStudy(studyId);
      toast.success(s("deleted"));
      navigate(study ? `/audience/${study.audience_id}` : "/audience");
    } catch (e) {
      fail(e, s("deleteFailed"));
      return false;
    }
  };

  if (notFound) {
    return (
      <div className="mx-auto w-full max-w-5xl">
        <PageBackLink to="/audience" label={s("back")} />
        <Card>
          <EmptyState icon={XCircle} title={s("notFoundTitle")} description={s("notFoundBody")} />
        </Card>
      </div>
    );
  }
  if (!study) {
    return (
      <div className="mx-auto w-full max-w-5xl space-y-4">
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const answered = study.persona_done + study.persona_failed;
  const pct = study.persona_total ? (answered / study.persona_total) * 100 : 0;
  const askablePeople = study.answers.filter((a) => a.status === "ok" && a.persona_id).length;

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <PageBackLink to={`/audience/${study.audience_id}`} label={study.audience_name ?? s("back")} />

      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          <h1 className="text-display font-semibold tracking-tight">{study.title}</h1>
          <p className="mt-1 flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
            <Badge variant={studyStatusTone(study.status)}>{t(`audienceHub.studyStatus.${study.status}`)}</Badge>
            <Link to={`/audience/${study.audience_id}`} className="underline-offset-4 hover:underline">
              {study.audience_name}
            </Link>
            · {new Date(study.created_at).toLocaleString(lang)}
          </p>
          {study.goal && <p className="mt-2 text-body text-muted-foreground">{study.goal}</p>}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {study.status === "draft" && (
            <Button onClick={() => setRunOpen(true)}>
              <Play />
              {s("run")}
            </Button>
          )}
          {active && (
            <Button variant="outline" onClick={() => setCancelOpen(true)}>
              {s("cancel")}
            </Button>
          )}
          {!active && (
            <Button variant="ghost" size="icon" onClick={() => setDeleteOpen(true)} aria-label={s("delete")} title={s("delete")}>
              <Trash2 />
            </Button>
          )}
        </div>
      </div>

      {noRuntime && <AudienceRuntimeNotice />}

      {study.status === "draft" && (
        <Card className="space-y-3 p-5">
          <h2 className="text-title font-semibold tracking-tight">{s("planHeading")}</h2>
          <p className="text-body text-muted-foreground">{s("planBody", { people: study.persona_total, calls: study.calls_estimate })}</p>
          {study.price_points.length > 0 && (
            <p className="text-body">
              <span className="font-medium">{s("prices")}: </span>
              {study.price_points.map((p) => formatMoney(p, study.currency, lang)).join(", ")}
            </p>
          )}
          {study.questions.length > 0 && (
            <div>
              <p className="font-medium">{s("questions")}</p>
              <ul className="list-disc pl-5 text-body text-muted-foreground">
                {study.questions.map((q) => (
                  <li key={q}>{q}</li>
                ))}
              </ul>
            </div>
          )}
        </Card>
      )}

      {active && (
        <Card className="space-y-3 p-5" role="status">
          <h2 className="flex items-center gap-2 text-title font-semibold tracking-tight">
            <Loader2 className="h-5 w-5 animate-spin" aria-hidden />
            {study.status === "synthesizing" ? s("synthesizing") : s("running")}
          </h2>
          <Progress value={study.status === "synthesizing" ? 100 : pct} label={s("progress", { done: answered, total: study.persona_total })} />
          <p className="text-caption text-muted-foreground">
            {s("progress", { done: answered, total: study.persona_total })}
            {study.persona_failed > 0 && ` · ${s("failedCount", { count: study.persona_failed })}`}
          </p>
          <p className="text-caption text-muted-foreground">{s("runningHelp")}</p>
        </Card>
      )}

      {(study.status === "failed" || study.status === "cancelled") && (
        <Notice variant={study.status === "failed" ? "error" : "warning"} title={study.status === "failed" ? s("failedTitle") : s("cancelledTitle")}>
          {study.error || (study.status === "cancelled" ? s("cancelledBody") : s("failedBody"))}
          {study.persona_done > 0 && <span className="mt-1 block">{s("keptAnswers", { count: study.persona_done })}</span>}
        </Notice>
      )}

      {study.report && <ReportView report={study.report} title={study.title} audience={study.audience_name} />}

      {study.answers.length > 0 && (
        <section className="space-y-3" aria-labelledby="answers-heading">
          <div>
            <h2 id="answers-heading" className="text-title font-semibold tracking-tight">
              {t("audienceHub.answers.heading")}
            </h2>
            <p className="text-caption text-muted-foreground">{t("audienceHub.answers.help")}</p>
          </div>
          <StudyAnswers answers={study.answers} currency={study.currency} onTalk={setTalkTo} />
        </section>
      )}

      {study.status === "done" && (
        <section className="space-y-3" aria-labelledby="followups-heading">
          <h2 id="followups-heading" className="text-title font-semibold tracking-tight">
            {t("audienceHub.followUps.heading")}
          </h2>
          <StudyFollowUps
            studyId={study.id}
            followUps={study.follow_ups}
            people={askablePeople}
            onAsked={() => {
              setWatchUntil(Date.now() + FOLLOW_UP_WATCH_MS);
              void load();
            }}
            onNoRuntime={() => setNoRuntime(true)}
          />
        </section>
      )}

      <PersonaChatDialog
        persona={talkTo?.persona_id ? { id: talkTo.persona_id, name: talkTo.persona.name, role: talkTo.persona.role, segment: talkTo.persona.segment } : null}
        studyId={study.id}
        onOpenChange={(o) => !o && setTalkTo(null)}
        onAnswered={() => void load()}
      />
      <ConfirmDialog
        open={runOpen}
        onOpenChange={setRunOpen}
        title={s("runTitle")}
        description={s("runBody", { calls: study.calls_estimate, people: study.persona_total })}
        confirmLabel={s("run")}
        onConfirm={start}
      />
      <ConfirmDialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        title={s("cancelTitle")}
        description={s("cancelBody")}
        confirmLabel={s("cancel")}
        variant="destructive"
        onConfirm={cancel}
      />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={s("deleteTitle")}
        description={s("deleteBody", { title: study.title })}
        confirmLabel={s("delete")}
        variant="destructive"
        onConfirm={remove}
      />
    </div>
  );
}

export default StudyPage;
