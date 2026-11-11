import { ArrowRight, Loader2, RotateCcw } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { api, type AudienceStudyDetail, type PersonaProfile, type StudyAnswer } from "@/api";
import { PersonaAvatar } from "@/components/audience/PersonaAvatar";
import { StudyActionBar } from "@/components/audience/StudyActionBar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useI18n } from "@/hooks/useI18n";
import { buildTranscript, interviewTotal } from "@/lib/studyWizard";
import { cn } from "@/lib/utils";

type PersonStatus = "progress" | "done" | "failed";

interface Person extends PersonaProfile {
  key: string;
  ordinal: number;
  name: string;
  segment?: string;
  status: PersonStatus;
  answer?: StudyAnswer;
}

function peopleOf(study: AudienceStudyDetail): Person[] {
  const answered: Person[] = study.answers.map((a) => ({
    ...a.persona,
    key: a.id,
    ordinal: a.persona.ordinal ?? 0,
    status: a.status === "ok" ? "done" : "failed",
    answer: a,
  }));
  const waiting: Person[] = (study.pending ?? []).map((p) => ({ ...p, key: p.persona_id, status: "progress" }));
  return [...answered, ...waiting].sort((a, b) => a.ordinal - b.ordinal || a.name.localeCompare(b.name));
}

function Typing() {
  return (
    <span className="inline-flex items-center gap-1" aria-hidden>
      {[0, 1, 2].map((i) => (
        <span key={i} className="h-1.5 w-1.5 animate-pulse rounded-full bg-muted-foreground/60" style={{ animationDelay: `${i * 160}ms` }} />
      ))}
    </span>
  );
}

function InterviewCard({ person, study }: { person: Person; study: AudienceStudyDetail }) {
  const { t } = useI18n();
  const variants = study.inputs.variants;
  const logRef = useRef<HTMLDivElement>(null);
  const turns = useMemo(() => (person.answer ? buildTranscript(person.answer, study.kind, variants, t) : []), [person.answer, study.kind, variants, t]);
  const total = interviewTotal(study.kind, study.script);
  const answered = person.status === "done" ? total : 0;

  useEffect(() => {
    const el = logRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [turns.length, person.status]);

  const tone = person.status === "done" ? "success" : person.status === "failed" ? "destructive" : "info";
  return (
    <Card className="flex flex-col gap-3 p-4" data-testid="interview-card">
      <div className="flex items-start gap-3">
        <PersonaAvatar name={person.name} />
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium">{person.name}</p>
          <p className="truncate text-caption text-muted-foreground">
            {[person.age ? String(person.age) : "", person.role, person.segment].filter(Boolean).join(" · ")}
          </p>
          {person.location && <p className="truncate text-caption text-muted-foreground">{person.location}</p>}
        </div>
        <Badge variant={tone} className="shrink-0">
          {person.status === "progress" && <Loader2 className="mr-1 h-3 w-3 animate-spin" aria-hidden />}
          {t(`audienceStudy.interviews.status.${person.status}`)}
        </Badge>
      </div>
      <div>
        <div className="mb-1 flex justify-between text-caption text-muted-foreground">
          <span>{t("audienceStudy.interviews.questions")}</span>
          <span>
            {answered}/{total}
          </span>
        </div>
        <Progress value={total ? (answered / total) * 100 : 0} className="h-1" tone={person.status === "failed" ? "destructive" : "default"} label={t("audienceStudy.interviews.questionsOf", { name: person.name, done: answered, total })} />
      </div>
      <div ref={logRef} role="log" aria-label={t("audienceStudy.interviews.transcript", { name: person.name })} className="h-56 space-y-2 overflow-y-auto rounded-lg bg-surface-sunken p-3 text-caption">
        {person.status === "progress" && (
          <>
            <p className="text-muted-foreground">{t("audienceStudy.interviews.preparing")}</p>
            <Typing />
          </>
        )}
        {person.status === "failed" && <p className="text-destructive">{person.answer?.error || t("audienceStudy.interviews.failedBody")}</p>}
        {turns.map((turn, i) => (
          <div key={i} className="space-y-1">
            <p className="max-w-[90%] rounded-lg bg-background px-2.5 py-1.5 text-muted-foreground">{turn.question}</p>
            <p className={cn("ml-auto max-w-[90%] whitespace-pre-line rounded-lg bg-primary/10 px-2.5 py-1.5 text-foreground")}>{turn.answer}</p>
          </div>
        ))}
      </div>
    </Card>
  );
}

interface StudyInterviewsStepProps {
  study: AudienceStudyDetail;
  onChanged: () => void;
  onError: (e: unknown, fallback: string) => void;
}

/**
 * The interviews: one card per person with the conversation as it fills in,
 * filters by segment, and the bar that moves the study on. The report is
 * written only when the stakeholder asks for it; that click is the yes to its
 * one model call.
 */
export function StudyInterviewsStep({ study, onChanged, onError }: StudyInterviewsStepProps) {
  const { t } = useI18n();
  const i = (key: string, params?: Record<string, string | number>) => t(`audienceStudy.interviews.${key}`, params);
  const [filter, setFilter] = useState("");
  const [busy, setBusy] = useState<"" | "report" | "retry">("");
  const people = useMemo(() => peopleOf(study), [study]);
  const groups = useMemo(() => [...new Set(people.map((p) => p.segment || p.role || "").filter(Boolean))], [people]);
  const shown = filter ? people.filter((p) => (p.segment || p.role) === filter) : people;

  const failed = people.filter((p) => p.status === "failed").length;
  const ok = people.filter((p) => p.status === "done").length;
  const total = Math.max(study.persona_total, people.length);
  const finished = ok + failed;

  const run = async (kind: "report" | "retry") => {
    setBusy(kind);
    try {
      if (kind === "report") await api.generateAudienceStudyReport(study.id);
      else await api.retryAudienceStudy(study.id);
      onChanged();
    } catch (e) {
      onError(e, kind === "report" ? i("reportFailed") : i("retryFailed"));
    } finally {
      setBusy("");
    }
  };

  const bar = (() => {
    switch (study.status) {
      case "running":
        return {
          status: i("running", { done: finished, total }),
          actions: (
            <Button disabled>
              {i("generate")}
              <ArrowRight />
            </Button>
          ),
        };
      case "synthesizing":
        return {
          status: i("writing"),
          actions: (
            <Button disabled>
              <Loader2 className="animate-spin" />
              {i("generate")}
            </Button>
          ),
        };
      case "interviewed":
        return {
          status: failed > 0 ? i("someFailed", { failed, ok, total }) : i("allDone", { total }),
          actions: (
            <>
              {failed > 0 && (
                <Button variant="outline" onClick={() => void run("retry")} disabled={busy !== ""}>
                  {busy === "retry" ? <Loader2 className="animate-spin" /> : <RotateCcw />}
                  {i("retry", { count: failed })}
                </Button>
              )}
              <Button onClick={() => void run("report")} disabled={ok === 0 || busy !== ""}>
                {busy === "report" && <Loader2 className="animate-spin" />}
                {failed > 0 ? i("generatePartial", { ok }) : i("generate")}
                <ArrowRight />
              </Button>
            </>
          ),
          note: i("reportCost"),
        };
      default:
        return null;
    }
  })();

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-title font-semibold tracking-tight">{i("title")}</h2>
          <p className="text-caption text-muted-foreground">{i("description")}</p>
        </div>
        <p className="text-body font-medium tabular-nums" aria-live="polite">
          {i("completed", { done: ok, total })}
        </p>
      </div>

      {groups.length > 1 && (
        <div className="flex flex-wrap gap-2" role="group" aria-label={i("filter")}>
          <Button size="sm" variant={filter === "" ? "default" : "outline"} aria-pressed={filter === ""} onClick={() => setFilter("")}>
            {i("allRoles")}
          </Button>
          {groups.map((g) => (
            <Button key={g} size="sm" variant={filter === g ? "default" : "outline"} aria-pressed={filter === g} onClick={() => setFilter(g)}>
              {g}
            </Button>
          ))}
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {shown.map((p) => (
          <InterviewCard key={p.key} person={p} study={study} />
        ))}
      </div>

      {bar && (
        <StudyActionBar
          status={
            <>
              {bar.status}
              {"note" in bar && bar.note && <span className="mt-0.5 block text-caption text-muted-foreground">{bar.note}</span>}
            </>
          }
          progress={<Progress value={total ? (finished / total) * 100 : 0} label={i("running", { done: finished, total })} />}
          actions={bar.actions}
        />
      )}
    </div>
  );
}
