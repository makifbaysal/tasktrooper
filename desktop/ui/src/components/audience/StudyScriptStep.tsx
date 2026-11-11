import { ArrowRight, Loader2, Plus, RefreshCw, Trash2, Upload, X } from "lucide-react";
import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { toast } from "sonner";
import { api, type AudienceStudyDetail, type StudyKind, type StudyScript } from "@/api";
import { StudyActionBar } from "@/components/audience/StudyActionBar";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cleanScript, MAX_SCRIPT_QUESTIONS, MAX_SCRIPT_SECTIONS, parseScriptText, scriptQuestionCount } from "@/lib/studyWizard";

const CORE_KEYS: Record<StudyKind, string[]> = {
  user_interviews: ["buy", "price", "missing", "objections", "alternatives"],
  landing_page_test: ["impression", "clarity", "act", "confused", "missing", "trust", "change"],
  message_testing: ["ratings", "best", "stronger"],
  ab_test: ["scorecard", "preferred"],
};

interface StudyScriptStepProps {
  study: AudienceStudyDetail;
  readOnly?: boolean;
  onChanged: () => void;
  onError: (e: unknown, fallback: string) => void;
}

/**
 * The interview script: what the person's agent wrote from the goal, editable,
 * or the stakeholder's own. The study cannot be executed until it exists;
 * pressing Execute is the stakeholder's yes to the model calls shown beside it.
 */
export function StudyScriptStep({ study, readOnly = false, onChanged, onError }: StudyScriptStepProps) {
  const { t, lang } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`audienceStudy.script.${key}`, params);
  const [draft, setDraft] = useState<StudyScript>(study.script ?? { sections: [] });
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState<"" | "save" | "execute" | "again" | "upload">("");
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!dirty) setDraft(study.script ?? { sections: [] });
  }, [study.script, dirty]);

  const generating = study.script_state === "generating";
  const count = scriptQuestionCount(draft);
  const canEdit = !readOnly && !generating && busy === "";
  const canExecute = canEdit && count > 0 && count <= MAX_SCRIPT_QUESTIONS;

  const edit = (next: StudyScript) => {
    setDraft(next);
    setDirty(true);
  };

  const persist = async (script: StudyScript) => {
    const saved = await api.updateAudienceStudy(study.id, { script: cleanScript(script) });
    setDraft(saved.script ?? { sections: [] });
    setDirty(false);
    return saved;
  };

  const save = async () => {
    setBusy("save");
    try {
      await persist(draft);
      toast.success(s("saved"));
      onChanged();
    } catch (e) {
      onError(e, t("common.saveFailed"));
    } finally {
      setBusy("");
    }
  };

  const execute = async () => {
    setBusy("execute");
    try {
      let version = study.script_version;
      let approved = study.script_state === "approved" && study.script_approved_version === study.script_version;
      if (dirty) {
        version = (await persist(draft)).script_version;
        approved = false;
      }
      if (!approved) await api.approveAudienceStudyScript(study.id, version);
      await api.startAudienceStudy(study.id, { manualReport: true });
      onChanged();
    } catch (e) {
      onError(e, t("audienceHub.study.startFailed"));
      setBusy("");
    }
  };

  const writeAgain = async () => {
    setBusy("again");
    try {
      await api.generateAudienceStudyScript(study.id);
      setDirty(false);
      onChanged();
    } catch (e) {
      onError(e, s("againFailed"));
    } finally {
      setBusy("");
    }
  };

  const upload = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    setBusy("upload");
    try {
      const parsed = parseScriptText(await file.text());
      const total = scriptQuestionCount(parsed);
      if (total === 0) throw new Error(s("uploadEmpty"));
      if (total > MAX_SCRIPT_QUESTIONS || parsed.sections.length > MAX_SCRIPT_SECTIONS) {
        throw new Error(s("uploadTooLong", { max: MAX_SCRIPT_QUESTIONS, sections: MAX_SCRIPT_SECTIONS }));
      }
      await persist(parsed);
      toast.success(s("uploaded", { count: total }));
      onChanged();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : s("uploadFailed"));
    } finally {
      setBusy("");
    }
  };

  const setSection = (i: number, patch: Partial<StudyScript["sections"][number]>) =>
    edit({ sections: draft.sections.map((sec, j) => (j === i ? { ...sec, ...patch } : sec)) });

  const status = generating
    ? s("generating")
    : study.script_state === "failed"
      ? s("failedStatus")
      : readOnly
        ? s("readOnly", { count })
        : count === 0
          ? s("empty")
          : s("ready", { count, calls: study.calls_estimate });
  const isApproved = !dirty && study.script_state === "approved" && study.script_approved_version === study.script_version;
  const approval = isApproved
    ? s("approvedBy", { by: study.script_approved_by || "-", date: study.script_approved_at ? new Date(study.script_approved_at).toLocaleString(lang) : "" })
    : s("awaiting");

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h2 className="text-title font-semibold tracking-tight">{generating ? s("craftingTitle") : s("title")}</h2>
          <p className="mt-1 text-caption text-muted-foreground">{generating ? s("craftingBody") : s("description")}</p>
        </div>
        {!readOnly && (
          <div className="flex shrink-0 flex-wrap gap-2">
            <input ref={fileRef} type="file" accept=".txt,.md,.markdown,text/plain,text/markdown" className="sr-only" aria-label={s("upload")} data-testid="script-upload" onChange={(e) => void upload(e)} />
            <Button variant="outline" onClick={() => fileRef.current?.click()} disabled={!canEdit}>
              {busy === "upload" ? <Loader2 className="animate-spin" /> : <Upload />}
              {s("upload")}
            </Button>
            <Button variant="outline" onClick={() => void writeAgain()} disabled={!canEdit} title={s("againHelp")}>
              {busy === "again" ? <Loader2 className="animate-spin" /> : <RefreshCw />}
              {s("again")}
            </Button>
          </div>
        )}
      </div>

      {study.script_state === "failed" && study.error && (
        <Notice variant="error" title={s("failedTitle")}>
          {study.error}
        </Notice>
      )}

      <Card className="space-y-2 p-4">
        <h3 className="text-body font-semibold">{s("coreTitle")}</h3>
        <p className="text-caption text-muted-foreground">{s("coreBody")}</p>
        <ol className="list-decimal space-y-1 pl-5 text-body text-muted-foreground">
          {CORE_KEYS[study.kind].map((key) => (
            <li key={key}>{t(`audienceStudy.core.${study.kind}.${key}`)}</li>
          ))}
        </ol>
      </Card>

      {generating ? (
        <div className="space-y-3" aria-hidden>
          {[0, 1, 2].map((i) => (
            <Card key={i} className="space-y-2 p-4">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-9 w-full" />
            </Card>
          ))}
        </div>
      ) : (
        <div className="space-y-3">
          {draft.sections.map((sec, i) => (
            <Card key={i} className="space-y-3 p-4">
              <div className="flex items-center gap-2">
                <Input
                  aria-label={s("sectionTitle")}
                  value={sec.title}
                  placeholder={s("sectionTitle")}
                  disabled={!canEdit}
                  maxLength={100}
                  className="font-medium"
                  onChange={(e) => setSection(i, { title: e.target.value })}
                />
                {canEdit && (
                  <Button variant="ghost" size="icon" aria-label={s("removeSection")} onClick={() => edit({ sections: draft.sections.filter((_, j) => j !== i) })}>
                    <Trash2 />
                  </Button>
                )}
              </div>
              <ul className="space-y-2">
                {sec.questions.map((q, k) => (
                  <li key={k} className="flex items-center gap-2">
                    <Input
                      aria-label={s("question")}
                      value={q}
                      disabled={!canEdit}
                      maxLength={400}
                      onChange={(e) => setSection(i, { questions: sec.questions.map((x, m) => (m === k ? e.target.value : x)) })}
                    />
                    {canEdit && (
                      <Button variant="ghost" size="icon" aria-label={s("removeQuestion")} onClick={() => setSection(i, { questions: sec.questions.filter((_, m) => m !== k) })}>
                        <X />
                      </Button>
                    )}
                  </li>
                ))}
              </ul>
              {canEdit && count < MAX_SCRIPT_QUESTIONS && (
                <Button variant="outline" size="sm" onClick={() => setSection(i, { questions: [...sec.questions, ""] })}>
                  <Plus />
                  {s("addQuestion")}
                </Button>
              )}
            </Card>
          ))}
          {canEdit && draft.sections.length < MAX_SCRIPT_SECTIONS && count < MAX_SCRIPT_QUESTIONS && (
            <Button variant="outline" onClick={() => edit({ sections: [...draft.sections, { title: "", questions: [""] }] })}>
              <Plus />
              {s("addSection")}
            </Button>
          )}
          {!readOnly && <p className="text-caption text-muted-foreground">{s("limit", { count, max: MAX_SCRIPT_QUESTIONS })}</p>}
        </div>
      )}

      {!readOnly && (
        <StudyActionBar
          status={
            <>
              {status}
              {!generating && count > 0 && <span className="mt-0.5 block text-caption text-muted-foreground">{approval}</span>}
            </>
          }
          progress={generating ? <div className="h-2 w-full animate-pulse rounded-full bg-primary/30" aria-hidden /> : undefined}
          actions={
            <>
              {dirty && (
                <Button variant="outline" onClick={() => void save()} disabled={!canEdit}>
                  {busy === "save" && <Loader2 className="animate-spin" />}
                  {t("common.save")}
                </Button>
              )}
              <Button onClick={() => void execute()} disabled={!canExecute}>
                {busy === "execute" && <Loader2 className="animate-spin" />}
                {isApproved ? s("executeApproved") : s("execute")}
                <ArrowRight />
              </Button>
            </>
          }
        />
      )}
    </div>
  );
}
