import { Loader2, MessagesSquare, Plus, Users } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api, type Audience, type AudienceStudy } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { AudienceFormDialog } from "@/components/audience/AudienceFormDialog";
import { AudienceRuntimeNotice } from "@/components/audience/AudienceRuntimeNotice";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { useAudienceRuntime } from "@/hooks/useAudienceRuntime";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import { audienceStatusTone, studyIsActive, studyStatusTone } from "@/lib/audienceHub";

const POLL_MS = 4000;

export function AudienceCard({ audience }: { audience: Audience }) {
  const { t } = useI18n();
  return (
    <Link
      to={`/audience/${audience.id}`}
      className="block rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <Card className="flex h-full flex-col gap-3 p-5 transition-colors hover:bg-accent/30">
        <div className="flex items-start justify-between gap-3">
          <h3 className="min-w-0 truncate text-title font-semibold tracking-tight">{audience.name}</h3>
          <Badge variant={audienceStatusTone(audience.status)} className="shrink-0">
            {audience.status === "building" && <Loader2 className="mr-1 h-3 w-3 animate-spin" aria-hidden />}
            {t(`audienceHub.status.${audience.status}`)}
          </Badge>
        </div>
        <p className="line-clamp-3 text-body text-muted-foreground">{audience.brief.product}</p>
        {audience.brief.segments && audience.brief.segments.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {audience.brief.segments.slice(0, 4).map((s) => (
              <Badge key={s.name} variant="outline">
                {s.name}
              </Badge>
            ))}
          </div>
        )}
        <p className="mt-auto flex items-center gap-4 pt-1 text-caption text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <Users className="h-3.5 w-3.5" aria-hidden />
            {t("audienceHub.list.people", { count: audience.persona_count })}
          </span>
          <span className="flex items-center gap-1.5">
            <MessagesSquare className="h-3.5 w-3.5" aria-hidden />
            {t("audienceHub.list.studies", { count: audience.study_count })}
          </span>
        </p>
      </Card>
    </Link>
  );
}

export function StudyRow({ study }: { study: AudienceStudy }) {
  const { t } = useI18n();
  const active = studyIsActive(study.status);
  const answered = study.persona_done + study.persona_failed;
  return (
    <li>
      <Link
        to={`/audience/studies/${study.id}`}
        className="flex flex-col gap-2 rounded-lg px-3 py-3 transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:flex-row sm:items-center sm:gap-4"
      >
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium">{study.title}</p>
          <p className="truncate text-caption text-muted-foreground">
            {study.audience_name ? `${study.audience_name} · ` : ""}
            {new Date(study.created_at).toLocaleDateString()}
          </p>
        </div>
        {active && (
          <div className="w-full sm:w-40">
            <Progress value={study.persona_total ? (answered / study.persona_total) * 100 : 0} label={t("audienceHub.study.progress", { done: answered, total: study.persona_total })} />
            <p className="mt-1 text-caption text-muted-foreground">{t("audienceHub.study.progress", { done: answered, total: study.persona_total })}</p>
          </div>
        )}
        <Badge variant={studyStatusTone(study.status)} className="shrink-0 self-start sm:self-center">
          {t(`audienceHub.studyStatus.${study.status}`)}
        </Badge>
      </Link>
    </li>
  );
}

export function AudiencePage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const runtime = useAudienceRuntime();
  const [audiences, setAudiences] = useState<Audience[] | null>(null);
  const [studies, setStudies] = useState<AudienceStudy[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);

  const load = useCallback(async () => {
    try {
      const [a, s] = await Promise.all([api.audiences(), api.audienceStudies()]);
      setAudiences(a.audiences ?? []);
      setStudies(s.studies ?? []);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("audienceHub.list.loadFailed"));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (error) toast.error(error);
  }, [error]);

  const busy = useMemo(
    () => (audiences ?? []).some((a) => a.status === "building") || studies.some((s) => studyIsActive(s.status)),
    [audiences, studies],
  );
  usePolling(load, POLL_MS, busy, { leading: false });

  return (
    <div className="mx-auto w-full max-w-6xl space-y-8">
      <PageHeader
        title={t("audienceHub.list.title")}
        description={t("audienceHub.list.description")}
        action={
          <Button onClick={() => setFormOpen(true)}>
            <Plus />
            {t("audienceHub.list.newAudience")}
          </Button>
        }
      />

      {runtime && !runtime.ready && <AudienceRuntimeNotice />}

      {audiences === null ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-40 w-full" />
          ))}
        </div>
      ) : audiences.length === 0 ? (
        <Card>
          <EmptyState
            icon={Users}
            title={t("audienceHub.list.emptyTitle")}
            description={t("audienceHub.list.emptyBody")}
            action={
              <div className="flex flex-col items-center gap-2">
                <Button onClick={() => setFormOpen(true)}>
                  <Plus />
                  {t("audienceHub.list.newAudience")}
                </Button>
                <p className="max-w-sm text-caption text-muted-foreground">{t("audienceHub.list.askPm")}</p>
              </div>
            }
          />
        </Card>
      ) : (
        <section className="space-y-3" aria-labelledby="audiences-heading">
          <h2 id="audiences-heading" className="text-title font-semibold tracking-tight">
            {t("audienceHub.list.audiences")}
          </h2>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {audiences.map((a) => (
              <AudienceCard key={a.id} audience={a} />
            ))}
          </div>
          <p className="text-caption text-muted-foreground">{t("audienceHub.list.askPm")}</p>
        </section>
      )}

      {studies.length > 0 && (
        <section className="space-y-3" aria-labelledby="studies-heading">
          <h2 id="studies-heading" className="text-title font-semibold tracking-tight">
            {t("audienceHub.list.studiesHeading")}
          </h2>
          <Card className="p-2">
            <ul className="divide-y divide-border/60">
              {studies.map((s) => (
                <StudyRow key={s.id} study={s} />
              ))}
            </ul>
          </Card>
        </section>
      )}

      <AudienceFormDialog open={formOpen} onOpenChange={setFormOpen} onSaved={(a) => navigate(`/audience/${a.id}`)} />
    </div>
  );
}

export default AudiencePage;
