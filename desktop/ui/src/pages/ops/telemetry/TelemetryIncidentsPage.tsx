import { DetailFacts, DetailPanel, DetailSection } from "@/components/operations/console/DetailPanel";
import { ServicePicker } from "@/components/operations/telemetry/ServicePicker";
import { SeverityBadge, StateBadge } from "@/components/operations/telemetry/TelemetryBadges";
import { TelemetryQueryView } from "@/components/operations/telemetry/TelemetryQueryView";
import { TelemetryCell, TelemetryRow, TelemetryTable } from "@/components/operations/telemetry/TelemetryTable";
import { TelemetryToolbar } from "@/components/operations/telemetry/TelemetryToolbar";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { useTelemetryQuery } from "@/hooks/useTelemetryQuery";
import { useTelemetryScope } from "@/hooks/useTelemetryScope";
import { useUrlParam } from "@/hooks/useUrlParam";
import { intlLocale } from "@/lib/languages";
import { INCIDENT_STATES, type TelemetryIncident, type TelemetryIncidentEvent } from "@/lib/telemetryHub";

const ALL = "all";
const OPEN = "open";

function eventText(data: unknown): string {
  if (data === null || data === undefined) return "";
  if (typeof data === "string") return data;
  try {
    return JSON.stringify(data, null, 2);
  } catch {
    return "";
  }
}

function IncidentPanel({ incident, onClose }: { incident: TelemetryIncident; onClose: () => void }) {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.incidents.${key}`, params);
  const time = new Intl.DateTimeFormat(intlLocale(lang), { dateStyle: "medium", timeStyle: "medium" });
  const events = useTelemetryQuery<TelemetryIncidentEvent[]>(`incidents/${encodeURIComponent(incident.id)}/events`, undefined, { intervalMs: 0 });
  return (
    <DetailPanel
      open
      onOpenChange={(open) => !open && onClose()}
      title={incident.title}
      badges={
        <>
          <SeverityBadge severity={incident.severity} />
          <StateBadge state={incident.state} />
        </>
      }
    >
      <DetailFacts
        items={[
          { label: h("columns.service"), value: [incident.service, incident.endpoint].filter(Boolean).join(" ") || "-" },
          { label: h("columns.value"), value: incident.value || "-" },
          { label: h("columns.started"), value: time.format(incident.startedAt) },
          ...(incident.ackedAt ? [{ label: h("acknowledgedAt"), value: time.format(incident.ackedAt) }] : []),
          ...(incident.resolvedAt ? [{ label: h("resolvedAt"), value: time.format(incident.resolvedAt) }] : []),
        ]}
      />
      <DetailSection title={h("timeline")}>
        {events.data === undefined ? (
          events.error ? (
            <p className="text-caption text-destructive">{events.error}</p>
          ) : (
            <Skeleton className="h-24 w-full" />
          )
        ) : events.data.length === 0 ? (
          <p className="text-caption text-muted-foreground">{h("noEvents")}</p>
        ) : (
          <ol className="space-y-3">
            {[...events.data]
              .sort((a, b) => a.ts - b.ts)
              .map((ev) => {
                const text = eventText(ev.data);
                return (
                  <li key={ev.id} className="space-y-1">
                    <p className="text-caption">
                      <span className="font-medium">{ev.kind}</span>
                      <span className="ml-2 font-mono text-micro text-muted-foreground">{time.format(ev.ts)}</span>
                    </p>
                    {text && <pre className="overflow-x-auto rounded-md bg-muted/40 p-2 font-mono text-micro whitespace-pre-wrap break-all">{text}</pre>}
                  </li>
                );
              })}
          </ol>
        )}
      </DetailSection>
    </DetailPanel>
  );
}

/** `/operations/telemetry/incidents`: what the product's monitors opened, with
 * the timeline of each. Acknowledging and resolving stay in the product. */
export default function TelemetryIncidentsPage() {
  const { t, lang } = useI18n();
  const h = (key: string, params?: Record<string, string | number>) => t(`telemetryHub.incidents.${key}`, params);
  useTelemetryScope({ time: false });
  const [service, setService] = useUrlParam("service");
  const [state, setState] = useUrlParam("state");
  const [selectedId, setSelected] = useUrlParam("id");
  const time = new Intl.DateTimeFormat(intlLocale(lang), { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const read = useTelemetryQuery<TelemetryIncident[]>("incidents", { state: state === ALL ? undefined : state || OPEN });
  const rows = (read.data ?? []).filter((i) => (!service || i.service === service));
  const selected = rows.find((i) => i.id === selectedId);

  return (
    <div className="space-y-4">
      <TelemetryToolbar env={false}>
        <ServicePicker value={service} onChange={setService} label={h("service")} />
        <Select value={state || OPEN} onValueChange={(v) => setState(v === OPEN ? null : v)}>
          <SelectTrigger className="h-8 w-48" aria-label={h("stateFilter")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={OPEN}>{h("openOnly")}</SelectItem>
            <SelectItem value={ALL}>{h("allStates")}</SelectItem>
            {INCIDENT_STATES.filter((s) => s !== "open").map((s) => (
              <SelectItem key={s} value={s}>
                {t(`telemetryHub.states.${s}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </TelemetryToolbar>
      <TelemetryQueryView query={read} isEmpty={() => rows.length === 0} emptyTitle={h("empty")} emptyDescription={h("emptyHint")}>
        {() => (
          <TelemetryTable
            label={h("listLabel")}
            columns={[
              { key: "severity", label: h("columns.severity") },
              { key: "title", label: h("columns.incident") },
              { key: "service", label: h("columns.service") },
              { key: "state", label: h("columns.state") },
              { key: "started", label: h("columns.started") },
            ]}
          >
            {[...rows]
              .sort((a, b) => b.startedAt - a.startedAt)
              .map((i) => (
                <TelemetryRow key={i.id} onOpen={() => setSelected(i.id)} selected={selectedId === i.id}>
                  <TelemetryCell>
                    <SeverityBadge severity={i.severity} />
                  </TelemetryCell>
                  <TelemetryCell className="max-w-md">
                    <button type="button" className="block max-w-full truncate text-left font-medium hover:underline" onClick={() => setSelected(i.id)}>
                      {i.title}
                    </button>
                    {i.value && <span className="block truncate text-micro text-muted-foreground">{i.value}</span>}
                  </TelemetryCell>
                  <TelemetryCell>
                    {i.service}
                    {i.endpoint && <span className="block font-mono text-micro text-muted-foreground">{i.endpoint}</span>}
                  </TelemetryCell>
                  <TelemetryCell>
                    <StateBadge state={i.state} />
                  </TelemetryCell>
                  <TelemetryCell className="whitespace-nowrap text-muted-foreground">{time.format(i.startedAt)}</TelemetryCell>
                </TelemetryRow>
              ))}
          </TelemetryTable>
        )}
      </TelemetryQueryView>
      {selected && <IncidentPanel incident={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}
