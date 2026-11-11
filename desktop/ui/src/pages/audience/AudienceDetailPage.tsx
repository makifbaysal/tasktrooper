import { Loader2, MessageCircle, Pencil, Play, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ApiError, api, type AudienceDetail, type AudiencePersona, type AudienceStudy, AUDIENCE_NO_RUNTIME } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { AudienceFormDialog } from "@/components/audience/AudienceFormDialog";
import { AudienceRuntimeNotice } from "@/components/audience/AudienceRuntimeNotice";
import { PersonaAvatar } from "@/components/audience/PersonaAvatar";
import { PersonaChatDialog } from "@/components/audience/PersonaChatDialog";
import { StudyPlanDialog } from "@/components/audience/StudyPlanDialog";
import { PageBackLink } from "@/components/layout/PageBackLink";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { audienceStatusTone } from "@/lib/audienceHub";
import { StudyRow } from "./AudiencePage";

const POLL_MS = 3000;

function List({ items }: { items?: string[] }) {
  if (!items || items.length === 0) return null;
  return (
    <ul className="list-disc space-y-0.5 pl-4">
      {items.map((i) => (
        <li key={i}>{i}</li>
      ))}
    </ul>
  );
}

function PersonaCard({ persona, onTalk }: { persona: AudiencePersona; onTalk: () => void }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const p = (key: string) => t(`audienceHub.persona.${key}`);
  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-start gap-3">
        <PersonaAvatar name={persona.name} />
        <div className="min-w-0">
          <p className="truncate font-medium">{persona.name}</p>
          <p className="truncate text-caption text-muted-foreground">
            {[persona.age ? String(persona.age) : "", persona.role, persona.location].filter(Boolean).join(" · ")}
          </p>
        </div>
        {persona.segment && (
          <Badge variant="outline" className="ml-auto shrink-0">
            {persona.segment}
          </Badge>
        )}
      </div>
      {persona.quote && <p className="text-body italic text-muted-foreground">“{persona.quote}”</p>}
      {open && (
        <dl className="space-y-2 text-caption">
          {persona.bio && (
            <div>
              <dt className="font-medium">{p("bio")}</dt>
              <dd className="text-muted-foreground">{persona.bio}</dd>
            </div>
          )}
          {persona.goals && persona.goals.length > 0 && (
            <div>
              <dt className="font-medium">{p("goals")}</dt>
              <dd className="text-muted-foreground">
                <List items={persona.goals} />
              </dd>
            </div>
          )}
          {persona.pains && persona.pains.length > 0 && (
            <div>
              <dt className="font-medium">{p("pains")}</dt>
              <dd className="text-muted-foreground">
                <List items={persona.pains} />
              </dd>
            </div>
          )}
          {persona.current_solution && (
            <div>
              <dt className="font-medium">{p("usesToday")}</dt>
              <dd className="text-muted-foreground">{persona.current_solution}</dd>
            </div>
          )}
          {(persona.budget || persona.tech_savviness) && (
            <div>
              <dt className="font-medium">{p("traits")}</dt>
              <dd className="text-muted-foreground">
                {[persona.budget ? `${p("budget")}: ${persona.budget}` : "", persona.tech_savviness ? `${p("tech")}: ${persona.tech_savviness}` : ""]
                  .filter(Boolean)
                  .join(" · ")}
              </dd>
            </div>
          )}
          {persona.personality && (
            <div>
              <dt className="font-medium">{p("personality")}</dt>
              <dd className="text-muted-foreground">{persona.personality}</dd>
            </div>
          )}
        </dl>
      )}
      <div className="mt-auto flex items-center justify-between gap-2">
        <Button variant="ghost" size="sm" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
          {open ? p("hideProfile") : p("showProfile")}
        </Button>
        <Button variant="outline" size="sm" onClick={onTalk}>
          <MessageCircle />
          {p("talk")}
        </Button>
      </div>
    </Card>
  );
}

export function AudienceDetailPage() {
  const { audienceId = "" } = useParams();
  const { t } = useI18n();
  const navigate = useNavigate();
  const [audience, setAudience] = useState<AudienceDetail | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [noRuntime, setNoRuntime] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [planOpen, setPlanOpen] = useState(false);
  const [editStudy, setEditStudy] = useState<AudienceStudy | null>(null);
  const [buildOpen, setBuildOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [talkTo, setTalkTo] = useState<AudiencePersona | null>(null);
  const [runTarget, setRunTarget] = useState<AudienceStudy | null>(null);
  const d = (key: string, params?: Record<string, string | number>) => t(`audienceHub.detail.${key}`, params);

  const load = useCallback(async () => {
    try {
      setAudience(await api.audience(audienceId));
      setNotFound(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) setNotFound(true);
      else toast.error(e instanceof Error ? e.message : d("loadFailed"));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [audienceId]);

  useEffect(() => {
    setAudience(null);
    void load();
  }, [load]);

  usePolling(load, POLL_MS, audience?.status === "building", { leading: false });

  const fail = (e: unknown, fallback: string) => {
    if (e instanceof ApiError && e.type === AUDIENCE_NO_RUNTIME) setNoRuntime(true);
    toast.error(e instanceof Error ? e.message : fallback);
  };

  const build = async () => {
    try {
      await api.buildAudience(audienceId);
      toast.success(d("buildStarted"));
      await load();
    } catch (e) {
      fail(e, d("buildFailed"));
      return false;
    }
  };

  const cancelBuild = async () => {
    try {
      await api.cancelAudienceBuild(audienceId);
      await load();
    } catch (e) {
      fail(e, d("cancelFailed"));
    }
  };

  const remove = async () => {
    try {
      await api.deleteAudience(audienceId);
      toast.success(d("deleted"));
      navigate("/audience");
    } catch (e) {
      fail(e, d("deleteFailed"));
      return false;
    }
  };

  const runStudy = async () => {
    if (!runTarget) return;
    try {
      await api.startAudienceStudy(runTarget.id);
      toast.success(d("studyStarted"));
      navigate(`/audience/studies/${runTarget.id}`);
    } catch (e) {
      fail(e, d("studyFailed"));
      return false;
    }
  };

  if (notFound) {
    return (
      <div className="mx-auto w-full max-w-6xl">
        <PageBackLink to="/audience" label={d("back")} />
        <Card>
          <EmptyState icon={Trash2} title={d("notFoundTitle")} description={d("notFoundBody")} />
        </Card>
      </div>
    );
  }
  if (!audience) {
    return (
      <div className="mx-auto w-full max-w-6xl space-y-4">
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  const building = audience.status === "building";
  const hasPeople = audience.personas.length > 0;
  const brief = audience.brief;
  const draftStudies = audience.studies.filter((s) => s.status === "draft");

  return (
    <div className="mx-auto w-full max-w-6xl space-y-6">
      <PageBackLink to="/audience" label={d("back")} />
      <PageHeader
        title={audience.name}
        description={d("subtitle", { people: audience.persona_count, generation: audience.generation })}
        action={
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={audienceStatusTone(audience.status)}>{t(`audienceHub.status.${audience.status}`)}</Badge>
            <Button variant="outline" onClick={() => setEditOpen(true)} disabled={building}>
              <Pencil />
              {d("editBrief")}
            </Button>
            {building ? (
              <Button variant="outline" onClick={() => void cancelBuild()}>
                {d("cancelBuild")}
              </Button>
            ) : (
              <Button onClick={() => setBuildOpen(true)}>
                {hasPeople ? <RefreshCw /> : <Play />}
                {hasPeople ? d("rebuild") : d("build")}
              </Button>
            )}
            <Button variant="ghost" size="icon" onClick={() => setDeleteOpen(true)} aria-label={d("delete")} title={d("delete")}>
              <Trash2 />
            </Button>
          </div>
        }
      />

      {noRuntime && <AudienceRuntimeNotice />}
      {building && (
        <Notice variant="info" title={d("buildingTitle")} role="status">
          <span className="inline-flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
            {d("buildingBody")}
          </span>
        </Notice>
      )}
      {audience.status === "failed" && audience.error && (
        <Notice variant="error" title={d("buildFailedTitle")}>
          {audience.error}
        </Notice>
      )}
      {audience.status === "draft" && audience.error && (
        <Notice variant="info" title={d("buildCancelledTitle")} role="status">
          {audience.error}
        </Notice>
      )}

      <Card className="space-y-4 p-5">
        <h2 className="text-title font-semibold tracking-tight">{d("briefHeading")}</h2>
        <dl className="grid gap-x-8 gap-y-3 text-body md:grid-cols-2">
          <div className="md:col-span-2">
            <dt className="text-caption font-medium text-muted-foreground">{t("audienceHub.brief.product")}</dt>
            <dd className="mt-0.5 whitespace-pre-line">{brief.product}</dd>
          </div>
          {brief.segments && brief.segments.length > 0 && (
            <div className="md:col-span-2">
              <dt className="text-caption font-medium text-muted-foreground">{t("audienceHub.brief.segments")}</dt>
              <dd className="mt-1 flex flex-wrap gap-2">
                {brief.segments.map((s) => (
                  <Badge key={s.name} variant="outline" title={s.description}>
                    {s.name}
                    {s.count ? ` · ${s.count}` : ""}
                  </Badge>
                ))}
              </dd>
            </div>
          )}
          {(
            [
              ["locations", brief.locations],
              ["currentSolutions", brief.current_solutions],
              ["pricing", brief.pricing],
              ["topic", brief.topic],
              ["language", brief.language],
            ] as const
          )
            .filter(([, v]) => v)
            .map(([k, v]) => (
              <div key={k}>
                <dt className="text-caption font-medium text-muted-foreground">{t(`audienceHub.brief.${k}`)}</dt>
                <dd className="mt-0.5">{v}</dd>
              </div>
            ))}
        </dl>
      </Card>

      <section className="space-y-3" aria-labelledby="people-heading">
        <div className="flex items-end justify-between gap-3">
          <div>
            <h2 id="people-heading" className="text-title font-semibold tracking-tight">
              {d("peopleHeading")}
            </h2>
            <p className="text-caption text-muted-foreground">{d("syntheticNote")}</p>
          </div>
        </div>
        {hasPeople ? (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {audience.personas.map((p) => (
              <PersonaCard key={p.id} persona={p} onTalk={() => setTalkTo(p)} />
            ))}
          </div>
        ) : (
          !building && (
            <Card>
              <EmptyState
                icon={Play}
                title={d("noPeopleTitle")}
                description={d("noPeopleBody", { count: audience.persona_target, calls: audience.build_calls })}
                action={<Button onClick={() => setBuildOpen(true)}>{d("build")}</Button>}
              />
            </Card>
          )
        )}
      </section>

      <section className="space-y-3" aria-labelledby="studies-heading">
        <div className="flex items-end justify-between gap-3">
          <h2 id="studies-heading" className="text-title font-semibold tracking-tight">
            {d("studiesHeading")}
          </h2>
          <Button
            onClick={() => {
              setEditStudy(null);
              setPlanOpen(true);
            }}
            disabled={!hasPeople || building}
          >
            <Plus />
            {d("planStudy")}
          </Button>
        </div>
        {audience.studies.length === 0 ? (
          <Card>
            <p className="p-5 text-body text-muted-foreground">{hasPeople ? d("noStudies") : d("noStudiesNoPeople")}</p>
          </Card>
        ) : (
          <Card className="p-2">
            <ul className="divide-y divide-border/60">
              {audience.studies.map((s) => (
                <StudyRow key={s.id} study={s} />
              ))}
            </ul>
          </Card>
        )}
        {draftStudies.length > 0 && (
          <ul className="space-y-2">
            {draftStudies.map((s) => (
              <li key={s.id} className="flex flex-wrap items-center gap-3 rounded-lg border border-dashed border-border px-4 py-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{s.title}</p>
                  <p className="text-caption text-muted-foreground">{d("planned", { calls: s.calls_estimate, people: s.persona_total })}</p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setEditStudy(s);
                    setPlanOpen(true);
                  }}
                >
                  {d("edit")}
                </Button>
                <Button size="sm" onClick={() => setRunTarget(s)}>
                  <Play />
                  {d("runStudy")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <AudienceFormDialog open={editOpen} onOpenChange={setEditOpen} audience={audience} onSaved={() => void load()} />
      <StudyPlanDialog
        open={planOpen}
        onOpenChange={setPlanOpen}
        audienceId={audience.id}
        personaCount={audience.persona_count}
        study={editStudy}
        onSaved={() => void load()}
      />
      <PersonaChatDialog persona={talkTo} onOpenChange={(o) => !o && setTalkTo(null)} />
      <ConfirmDialog
        open={buildOpen}
        onOpenChange={setBuildOpen}
        title={hasPeople ? d("rebuildTitle") : d("buildTitle")}
        description={d(hasPeople ? "rebuildBody" : "buildBody", { count: audience.persona_target, calls: audience.build_calls })}
        confirmLabel={hasPeople ? d("rebuild") : d("build")}
        onConfirm={build}
      />
      <ConfirmDialog
        open={runTarget !== null}
        onOpenChange={(o) => !o && setRunTarget(null)}
        title={d("runTitle")}
        description={d("runBody", { calls: runTarget?.calls_estimate ?? 0, people: runTarget?.persona_total ?? 0 })}
        confirmLabel={d("runStudy")}
        onConfirm={runStudy}
      />
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={d("deleteTitle")}
        description={d("deleteBody", { name: audience.name })}
        confirmLabel={d("delete")}
        variant="destructive"
        onConfirm={remove}
      />
    </div>
  );
}

export default AudienceDetailPage;
