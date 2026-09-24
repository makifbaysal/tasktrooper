import { Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, RESOURCE_KINDS, type ResourceKind, type WorkspaceResource } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { ResourceKindIcon } from "@/components/projects/model/ResourceKindIcon";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

type KindFilter = "all" | ResourceKind;

interface ResourcePickerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  submitLabel: string;
  excludeResourceId?: string;
  defaultKind?: ResourceKind;
  repositoryId: string;
  projectIds: string[];
  onPick: (resource: WorkspaceResource) => Promise<void>;
}

function userLabel(user: WorkspaceResource["users"][number]): string {
  return user.component_path && user.component_path !== "." ? `${user.repository_name}/${user.component_path}` : user.repository_name;
}

function resourceSection(resource: WorkspaceResource, repositoryId: string, projectIds: string[]): "repo" | "project" | "other" {
  if (resource.users.some((u) => u.repository_id === repositoryId)) return "repo";
  if (resource.users.some((u) => u.project_ids.some((id) => projectIds.includes(id)))) return "project";
  return "other";
}

/**
 * Every workspace resource, filterable and grouped by how close it is to the
 * caller's repository/project — the picker behind both "merge with an
 * existing resource" and "link to an existing resource".
 */
export function ResourcePickerDialog({
  open,
  onOpenChange,
  title,
  description,
  submitLabel,
  excludeResourceId,
  defaultKind,
  repositoryId,
  projectIds,
  onPick,
}: ResourcePickerDialogProps) {
  const { t } = useI18n();
  const [resources, setResources] = useState<WorkspaceResource[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [kind, setKind] = useState<KindFilter>(defaultKind ?? "all");
  const [query, setQuery] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setResources(null);
    setLoadError(null);
    setKind(defaultKind ?? "all");
    setQuery("");
    setSelectedId(null);
    api
      .listWorkspaceResources()
      .then((res) => setResources(res.resources))
      .catch((e) => setLoadError(e instanceof Error ? e.message : t("repositoryPage.resourcePicker.loadFailed")));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, defaultKind]);

  const filtered = useMemo(() => {
    if (!resources) return [];
    let list = resources.filter((r) => r.resource.id !== excludeResourceId);
    if (kind !== "all") list = list.filter((r) => r.resource.kind === kind);
    const q = query.trim().toLowerCase();
    if (q) {
      list = list.filter((r) => {
        const haystack = [r.resource.name, r.resource.vendor, ...r.users.map((u) => u.repository_name)].join(" ").toLowerCase();
        return haystack.includes(q);
      });
    }
    return list;
  }, [resources, kind, query, excludeResourceId]);

  const sections = useMemo(() => {
    const repo: WorkspaceResource[] = [];
    const project: WorkspaceResource[] = [];
    const other: WorkspaceResource[] = [];
    for (const r of filtered) {
      const section = resourceSection(r, repositoryId, projectIds);
      (section === "repo" ? repo : section === "project" ? project : other).push(r);
    }
    return { repo, project, other };
  }, [filtered, repositoryId, projectIds]);

  const selected = filtered.find((r) => r.resource.id === selectedId) ?? null;

  const submit = async () => {
    if (!selected) return;
    setSubmitting(true);
    try {
      await onPick(selected);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={!selected || submitting}>
            {submitLabel}
          </Button>
        </>
      }
    >
      <div className="flex gap-2">
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("repositoryPage.resourcePicker.searchPlaceholder")} className="pl-8" />
        </div>
        <Select value={kind} onValueChange={(v) => setKind(v as KindFilter)}>
          <SelectTrigger className="w-44 shrink-0">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("repositoryPage.resourcePicker.allKinds")}</SelectItem>
            {RESOURCE_KINDS.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`projectModel.resourceKinds.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {loadError && <Notice variant="error" title={loadError} />}

      {!loadError && resources === null ? (
        <div className="space-y-2">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
        </div>
      ) : !loadError && filtered.length === 0 ? (
        <EmptyState icon={Search} variant="search" title={t("repositoryPage.resourcePicker.empty")} />
      ) : !loadError ? (
        <div role="radiogroup" aria-label={title} className="max-h-80 space-y-4 overflow-y-auto">
          <ResourceSection heading={t("repositoryPage.resourcePicker.inThisRepository")} items={sections.repo} selectedId={selectedId} onSelect={setSelectedId} />
          <ResourceSection heading={t("repositoryPage.resourcePicker.inThisProject")} items={sections.project} selectedId={selectedId} onSelect={setSelectedId} />
          <ResourceSection heading={t("repositoryPage.resourcePicker.inOtherProjects")} items={sections.other} selectedId={selectedId} onSelect={setSelectedId} />
        </div>
      ) : null}
    </FormDialog>
  );
}

function ResourceSection({
  heading,
  items,
  selectedId,
  onSelect,
}: {
  heading: string;
  items: WorkspaceResource[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  if (items.length === 0) return null;
  return (
    <div className="space-y-1.5">
      <p className="text-xs font-medium text-muted-foreground">{heading}</p>
      <div className="space-y-1.5">
        {items.map((r) => (
          <ResourceOption key={r.resource.id} resource={r} selected={r.resource.id === selectedId} onSelect={() => onSelect(r.resource.id)} />
        ))}
      </div>
    </div>
  );
}

function ResourceOption({ resource, selected, onSelect }: { resource: WorkspaceResource; selected: boolean; onSelect: () => void }) {
  const usersSummary = resource.users.map(userLabel).join(", ");
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex w-full flex-col gap-1 rounded-md border px-3 py-2 text-left transition-colors hover:bg-muted/40",
        selected ? "border-primary bg-primary/5" : "border-border",
      )}
    >
      <span className="flex items-center gap-2">
        <ResourceKindIcon kind={resource.resource.kind} className="shrink-0" />
        <span className="min-w-0 flex-1 truncate font-medium">{resource.resource.name}</span>
        {resource.resource.vendor && <span className="shrink-0 text-caption text-muted-foreground">{resource.resource.vendor}</span>}
      </span>
      {usersSummary && <span className="truncate pl-6 text-caption text-muted-foreground">{usersSummary}</span>}
      {resource.projects.length > 0 && (
        <span className="flex flex-wrap gap-1 pl-6">
          {resource.projects.map((p) => (
            <Badge key={p.id} variant="outline">
              {p.name}
            </Badge>
          ))}
        </span>
      )}
    </button>
  );
}
