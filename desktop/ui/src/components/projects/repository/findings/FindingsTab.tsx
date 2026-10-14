import { ChevronDown, ChevronRight, ClipboardList, ExternalLink, RotateCcw, ShieldCheck, ScanSearch } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  FINDING_CATEGORIES,
  type Component,
  type ComponentFinding,
  type FindingCategory,
  type FindingStatus,
  type RepositoryModel,
} from "@/api";
import { evidenceLabel } from "@/components/projects/model/EvidenceList";
import { ComponentRail } from "@/components/projects/repository/ComponentRail";
import {
  FindingTaskDialog,
  FindingVerdictDialog,
  type FindingVerdict,
} from "@/components/projects/repository/findings/FindingDialogs";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { EmptyState } from "@/components/ui/empty-state";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { FINDING_STATUSES, findingComponentKey, openFindings, severityVariant, sortFindings } from "@/lib/findings";
import { cn } from "@/lib/utils";

interface FindingsTabProps {
  model: RepositoryModel;
  repositoryId: string;
  selectedComponentId: string | null;
  onSelectComponent: (id: string | null) => void;
  onReload: () => void;
}

type CategoryFilter = FindingCategory | "all";

/**
 * Code-health findings per component: what the last scan found, the
 * person's verdict on each (accepted, false positive — both stay closed on
 * later scans) and the board task opened for it.
 */
export function FindingsTab({ model, repositoryId, selectedComponentId, onSelectComponent, onReload }: FindingsTabProps) {
  const { t } = useI18n();
  const [status, setStatus] = useState<FindingStatus>("open");
  const [category, setCategory] = useState<CategoryFilter>("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [taskFindings, setTaskFindings] = useState<ComponentFinding[]>([]);
  const [verdict, setVerdict] = useState<{ finding: ComponentFinding; verdict: FindingVerdict } | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const findings = model.findings ?? [];
  const componentById = useMemo(() => new Map(model.components.map((c) => [c.id, c])), [model.components]);
  const scope = selectedComponentId && componentById.has(selectedComponentId) ? selectedComponentId : null;

  const inScope = findings.filter((f) => scope === null || f.component_id === scope);
  const counts = Object.fromEntries(
    FINDING_STATUSES.map((s) => [s, inScope.filter((f) => f.status === s).length]),
  ) as Record<FindingStatus, number>;
  const visible = sortFindings(inScope.filter((f) => f.status === status && (category === "all" || f.category === category)));
  const selectable = visible.filter((f) => f.status === "open" && !f.task_id);
  const selectedFindings = visible.filter((f) => selected.has(f.id));

  const groups = useMemo(() => {
    const byKey = new Map<string, ComponentFinding[]>();
    for (const f of visible) {
      const key = findingComponentKey(f);
      byKey.set(key, [...(byKey.get(key) ?? []), f]);
    }
    return [...byKey.entries()].sort(([a], [b]) => componentLabel(componentById.get(a)).localeCompare(componentLabel(componentById.get(b))));
  }, [visible, componentById]);

  const toggle = (id: string, on: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });

  const reopen = async (finding: ComponentFinding) => {
    setBusyId(finding.id);
    try {
      await api.updateFinding(finding.id, { status: "open" });
      onReload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("findings.updateFailed"));
    } finally {
      setBusyId(null);
    }
  };

  const defaultTaskTitle = (items: ComponentFinding[]): string => {
    if (items.length === 0) return "";
    const ids = new Set(items.map(findingComponentKey));
    const component =
      ids.size > 1 ? t("findings.task.severalComponents") : componentLabel(componentById.get(findingComponentKey(items[0])), t("findings.repositoryWide"));
    if (items.length === 1) return `${t(`findings.rules.${items[0].rule}.title`)} (${component})`;
    return t("findings.task.defaultTitleMany", { count: items.length, component });
  };

  const scanned = Boolean(model.latest_scan);

  return (
    <div className="flex gap-6">
      <ComponentRail
        components={model.components}
        selectedId={scope}
        onSelect={(id) => {
          setSelected(new Set());
          onSelectComponent(id);
        }}
        allowAll
        allLabel={t("findings.allComponents")}
        renderTrailing={(c) => {
          const open = openFindings(findings, c.id).length;
          return open > 0 ? t("findings.openCount", { count: open }) : null;
        }}
      />
      <div className="min-w-0 flex-1 space-y-4">
        <Card className="flex items-start gap-3 p-4">
          <ScanSearch className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
          <p className="text-caption text-muted-foreground">{t("findings.intro")}</p>
        </Card>

        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-1" role="tablist">
            {FINDING_STATUSES.map((s) => (
              <button
                key={s}
                type="button"
                role="tab"
                aria-selected={status === s}
                onClick={() => {
                  setStatus(s);
                  setSelected(new Set());
                }}
                className={cn(
                  "rounded-full px-3 py-1 text-caption font-medium transition-colors",
                  status === s ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground hover:bg-muted/70",
                )}
              >
                {t(`findings.status.${s}`)} · {counts[s]}
              </button>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <Select value={category} onValueChange={(v) => setCategory(v as CategoryFilter)}>
              <SelectTrigger className="h-8 w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t("findings.allCategories")}</SelectItem>
                {FINDING_CATEGORIES.map((c) => (
                  <SelectItem key={c} value={c}>
                    {t(`findings.category.${c}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {status === "open" && selectable.length > 0 && (
              <>
                {selectedFindings.length > 0 ? (
                  <>
                    <Button variant="ghost" size="sm" onClick={() => setSelected(new Set())}>
                      {t("findings.clearSelection")}
                    </Button>
                    <Button size="sm" onClick={() => setTaskFindings(selectedFindings)}>
                      <ClipboardList className="mr-1.5 h-3.5 w-3.5" />
                      {t("findings.createTaskSelected", { count: selectedFindings.length })}
                    </Button>
                  </>
                ) : (
                  <Button variant="ghost" size="sm" onClick={() => setSelected(new Set(selectable.map((f) => f.id)))}>
                    {t("findings.selectAll")}
                  </Button>
                )}
              </>
            )}
          </div>
        </div>

        {!scanned ? (
          <EmptyState icon={ScanSearch} title={t("findings.empty.noScanTitle")} description={t("findings.empty.noScanDescription")} />
        ) : visible.length === 0 ? (
          inScope.length === 0 || (status === "open" && category === "all") ? (
            <EmptyState icon={ShieldCheck} title={t("findings.empty.cleanTitle")} description={t("findings.empty.cleanDescription")} />
          ) : (
            <EmptyState icon={ScanSearch} variant="search" title={t("findings.empty.filtered")} />
          )
        ) : (
          groups.map(([key, items]) => (
            <section key={key || "repository"} className="space-y-2">
              {scope === null && (
                <h3 className="font-mono text-caption text-muted-foreground">
                  {componentLabel(componentById.get(key), t("findings.repositoryWide"))}
                </h3>
              )}
              {items.map((finding) => (
                <FindingCard
                  key={finding.id}
                  finding={finding}
                  selectable={selectable.includes(finding)}
                  selected={selected.has(finding.id)}
                  busy={busyId === finding.id}
                  onSelect={(on) => toggle(finding.id, on)}
                  onCreateTask={() => setTaskFindings([finding])}
                  onVerdict={(v) => setVerdict({ finding, verdict: v })}
                  onReopen={() => reopen(finding)}
                />
              ))}
            </section>
          ))
        )}
      </div>

      <FindingTaskDialog
        repositoryId={repositoryId}
        findings={taskFindings}
        defaultTitle={defaultTaskTitle(taskFindings)}
        open={taskFindings.length > 0}
        onOpenChange={(open) => !open && setTaskFindings([])}
        onCreated={() => {
          setSelected(new Set());
          onReload();
        }}
      />
      <FindingVerdictDialog
        finding={verdict?.finding ?? null}
        verdict={verdict?.verdict ?? "accepted"}
        onOpenChange={(open) => !open && setVerdict(null)}
        onSaved={() => onReload()}
      />
    </div>
  );
}

function componentLabel(component: Component | undefined, fallback = ""): string {
  if (!component) return fallback;
  return component.path === "." ? "/" : component.path;
}

interface FindingCardProps {
  finding: ComponentFinding;
  selectable: boolean;
  selected: boolean;
  busy: boolean;
  onSelect: (on: boolean) => void;
  onCreateTask: () => void;
  onVerdict: (verdict: FindingVerdict) => void;
  onReopen: () => void;
}

function FindingCard({ finding, selectable, selected, busy, onSelect, onCreateTask, onVerdict, onReopen }: FindingCardProps) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const evidence = finding.evidence ?? [];
  const more = finding.occurrences - evidence.length;
  const sizeRule = finding.rule === "oversized_source_file";

  return (
    <Card className={cn("space-y-2 p-4", finding.status !== "open" && "opacity-80")}>
      <div className="flex items-start gap-3">
        {selectable && (
          <Checkbox
            className="mt-1"
            checked={selected}
            onCheckedChange={(v) => onSelect(v === true)}
            aria-label={t(`findings.rules.${finding.rule}.title`)}
          />
        )}
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={severityVariant(finding.severity)}>{t(`findings.severity.${finding.severity}`)}</Badge>
            <span className="font-medium">{t(`findings.rules.${finding.rule}.title`)}</span>
            <Badge variant="outline">{t(`findings.category.${finding.category}`)}</Badge>
            <span className="text-micro text-muted-foreground">{t("findings.occurrences", { count: finding.occurrences })}</span>
          </div>
          <p className="text-caption text-muted-foreground">{t(`findings.rules.${finding.rule}.description`)}</p>
          {finding.status === "resolved" && <p className="text-caption text-success">{t("findings.resolvedHint")}</p>}
          {finding.status_reason && (
            <p className="text-caption text-muted-foreground">{t("findings.reason", { reason: finding.status_reason })}</p>
          )}
        </div>
      </div>

      <div className={cn("flex flex-wrap items-center justify-between gap-2", selectable && "pl-7")}>
        {evidence.length > 0 ? (
          <Button variant="ghost" size="sm" className="h-7 px-2 text-micro" onClick={() => setExpanded((v) => !v)}>
            {expanded ? <ChevronDown className="mr-1 h-3 w-3" /> : <ChevronRight className="mr-1 h-3 w-3" />}
            {t(expanded ? "findings.hideLocations" : "findings.showLocations")}
          </Button>
        ) : (
          <span />
        )}
        <div className="flex flex-wrap items-center gap-1">
          {finding.task ? (
            <Button asChild variant="outline" size="sm">
              <Link to={`/board?task=${encodeURIComponent(finding.task.id)}`}>
                <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
                {t("findings.openTask", { key: finding.task.key })}
              </Link>
            </Button>
          ) : (
            finding.status === "open" && (
              <Button size="sm" variant="outline" disabled={busy} onClick={onCreateTask}>
                <ClipboardList className="mr-1.5 h-3.5 w-3.5" />
                {t("findings.createTask")}
              </Button>
            )
          )}
          {finding.status === "open" && (
            <>
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => onVerdict("accepted")}>
                {t("findings.accept")}
              </Button>
              <Button variant="ghost" size="sm" disabled={busy} onClick={() => onVerdict("false_positive")}>
                {t("findings.falsePositive")}
              </Button>
            </>
          )}
          {(finding.status === "accepted" || finding.status === "false_positive") && (
            <Button variant="ghost" size="sm" disabled={busy} onClick={onReopen}>
              <RotateCcw className="mr-1.5 h-3.5 w-3.5" />
              {t("findings.reopen")}
            </Button>
          )}
        </div>
      </div>

      {expanded && evidence.length > 0 && (
        <ul className={cn("space-y-0.5 text-micro", selectable && "pl-7")}>
          {evidence.map((e, i) => (
            <li key={`${e.path}:${e.line ?? 0}:${i}`} className="flex min-w-0 items-start gap-2">
              <span className="shrink-0 font-mono text-foreground">{evidenceLabel(e)}</span>
              {e.note && (
                <span className="truncate font-mono text-muted-foreground">
                  {sizeRule ? t("findings.lines", { count: e.note }) : e.note}
                </span>
              )}
            </li>
          ))}
          {more > 0 && <li className="text-muted-foreground">{t("findings.moreLocations", { count: more })}</li>}
        </ul>
      )}
    </Card>
  );
}
