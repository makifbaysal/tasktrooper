import { AlertTriangle, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type Incident, type IncidentSeverity, type Repository } from "@/api";
import { PageHeader } from "@/components/admin/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

const LIVE_STATUSES = "open,triaging,proposed,fixing";

function severityVariant(severity: IncidentSeverity): "destructive" | "warning" | "secondary" {
  if (severity === "critical") return "destructive";
  if (severity === "high") return "warning";
  return "secondary";
}

function formatTime(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "-" : d.toLocaleString();
}

// IncidentsPage — the production side of the board: what is broken, what the
// engine (or an agent) proposes to do about it, and the actions that close it.
export function IncidentsPage() {
  const { t } = useI18n();
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [repositories, setRepositories] = useState<Repository[]>([]);
  const [selected, setSelected] = useState<Incident | null>(null);
  const [statusFilter, setStatusFilter] = useState<"live" | "all">("live");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.listIncidents(statusFilter === "live" ? { status: LIVE_STATUSES } : undefined);
      setIncidents(res.incidents ?? []);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("projectAdmin.prodOps.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [statusFilter, t]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    void api
      .listRepositories()
      .then((res) => setRepositories(res.repositories ?? []))
      .catch(() => setRepositories([]));
  }, []);

  const repoName = useMemo(() => {
    const byID = new Map(repositories.map((r) => [r.id, r.name]));
    return (id: string) => byID.get(id) ?? id.slice(0, 8);
  }, [repositories]);

  const openDetail = useCallback(
    async (incident: Incident) => {
      try {
        setSelected(await api.getIncident(incident.id));
      } catch (err) {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      }
    },
    [t],
  );

  const act = useCallback(
    async (action: "triage" | "resolve" | "ignore", incident: Incident) => {
      setBusy(true);
      try {
        if (action === "triage") {
          setSelected(await api.getIncident((await api.triageIncident(incident.id)).id));
          toast.success(t("projectAdmin.prodOps.triaged"));
        } else if (action === "resolve") {
          await api.resolveIncident(incident.id);
          toast.success(t("projectAdmin.prodOps.resolved"));
          setSelected(null);
        } else {
          await api.ignoreIncident(incident.id);
          toast.success(t("projectAdmin.prodOps.ignored"));
          setSelected(null);
        }
        await load();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      } finally {
        setBusy(false);
      }
    },
    [load, t],
  );

  return (
    <div className="space-y-4 p-4 md:p-6">
      <PageHeader
        title={t("projectAdmin.prodOps.incidentsTitle")}
        description={t("projectAdmin.prodOps.incidentsSubtitle")}
        action={
          <div className="flex items-end gap-2">
            <div className="space-y-1">
              <Label htmlFor="incident-status">{t("projectAdmin.prodOps.statusFilter")}</Label>
              <Select value={statusFilter} onValueChange={(v) => setStatusFilter(v as "live" | "all")}>
                <SelectTrigger id="incident-status" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="live">{t("projectAdmin.prodOps.statusLive")}</SelectItem>
                  <SelectItem value="all">{t("projectAdmin.prodOps.statusAll")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Button variant="outline" onClick={() => void load()} disabled={loading}>
              <RefreshCw className={cn("mr-2 h-4 w-4", loading && "animate-spin")} />
              {t("common.refresh")}
            </Button>
          </div>
        }
      />

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
        <Card>
          <CardContent className="p-0">
            {loading ? (
              <div className="space-y-2 p-4">
                <Skeleton className="h-14 w-full" />
                <Skeleton className="h-14 w-full" />
                <Skeleton className="h-14 w-full" />
              </div>
            ) : incidents.length === 0 ? (
              <EmptyState
                icon={AlertTriangle}
                title={t("projectAdmin.prodOps.empty")}
                description={t("projectAdmin.prodOps.emptyDesc")}
              />
            ) : (
              <ul className="divide-y divide-border">
                {incidents.map((incident) => (
                  <li key={incident.id}>
                    <button
                      type="button"
                      onClick={() => void openDetail(incident)}
                      className={cn(
                        "flex w-full flex-col gap-1 px-4 py-3 text-left transition-colors hover:bg-muted/50",
                        selected?.id === incident.id && "bg-muted",
                      )}
                    >
                      <div className="flex flex-wrap items-center gap-2">
                        <Badge variant={severityVariant(incident.severity)}>{incident.severity}</Badge>
                        <Badge variant="outline">{incident.env}</Badge>
                        <Badge variant="secondary">{incident.status}</Badge>
                        {incident.occurrences > 1 && (
                          <span className="text-xs text-muted-foreground">×{incident.occurrences}</span>
                        )}
                      </div>
                      <span className="text-sm font-medium">{incident.title}</span>
                      <span className="text-xs text-muted-foreground">
                        {repoName(incident.repository_id)} · {formatTime(incident.last_seen_at)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        <Card>
          {selected === null ? (
            <CardContent className="p-6">
              <p className="text-sm text-muted-foreground">{t("projectAdmin.prodOps.selectHint")}</p>
            </CardContent>
          ) : (
            <>
              <CardHeader>
                <CardTitle className="text-base">{selected.title}</CardTitle>
                <CardDescription>
                  {repoName(selected.repository_id)} · {selected.env} · {selected.source}
                </CardDescription>
                <div className="flex flex-wrap gap-2 pt-2">
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => void act("triage", selected)}>
                    {t("projectAdmin.prodOps.triage")}
                  </Button>
                  <Button size="sm" disabled={busy} onClick={() => void act("resolve", selected)}>
                    {t("projectAdmin.prodOps.resolve")}
                  </Button>
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => void act("ignore", selected)}>
                    {t("projectAdmin.prodOps.ignore")}
                  </Button>
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                <dl className="grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                  <div>
                    <dt>{t("projectAdmin.prodOps.firstSeen")}</dt>
                    <dd className="text-foreground">{formatTime(selected.first_seen_at)}</dd>
                  </div>
                  <div>
                    <dt>{t("projectAdmin.prodOps.lastSeen")}</dt>
                    <dd className="text-foreground">{formatTime(selected.last_seen_at)}</dd>
                  </div>
                  <div>
                    <dt>{t("projectAdmin.prodOps.occurrences")}</dt>
                    <dd className="text-foreground">{selected.occurrences}</dd>
                  </div>
                  <div>
                    <dt>{t("projectAdmin.prodOps.confidence")}</dt>
                    <dd className="text-foreground">
                      {selected.remedy_kind ? `${selected.remedy_kind} · ${selected.confidence}%` : "-"}
                    </dd>
                  </div>
                </dl>

                <section className="space-y-1">
                  <h2 className="text-sm font-medium">{t("projectAdmin.prodOps.remedy")}</h2>
                  <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs">
                    {selected.remedy?.trim() || t("projectAdmin.prodOps.noRemedy")}
                  </pre>
                </section>

                {selected.detail && (
                  <section className="space-y-1">
                    <h2 className="text-sm font-medium">{t("projectAdmin.prodOps.payload")}</h2>
                    <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-xs">
                      {selected.detail}
                    </pre>
                  </section>
                )}

                {selected.events && selected.events.length > 0 && (
                  <section className="space-y-1">
                    <h2 className="text-sm font-medium">{t("projectAdmin.prodOps.timeline")}</h2>
                    <ul className="space-y-1 text-xs text-muted-foreground">
                      {selected.events.map((event) => (
                        <li key={event.id}>
                          <span className="text-foreground">{event.kind}</span> · {formatTime(event.created_at)}
                          {event.message ? ` — ${event.message}` : ""}
                        </li>
                      ))}
                    </ul>
                  </section>
                )}
              </CardContent>
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
