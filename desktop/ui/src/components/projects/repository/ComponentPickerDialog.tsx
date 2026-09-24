import { Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type ComponentSummary, type RepositorySummary } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

interface Row {
  componentId: string;
  repositoryName: string;
  component: ComponentSummary;
}

interface ComponentPickerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPick: (componentId: string, label: string) => void;
}

/** Every component in every repository this workspace knows about (grouped
 * project-independent) — the target picker for "Point to a component…". */
export function ComponentPickerDialog({ open, onOpenChange, onPick }: ComponentPickerDialogProps) {
  const { t } = useI18n();
  const [rows, setRows] = useState<Row[] | null>(null);
  const [query, setQuery] = useState("");

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setRows(null);
    const toRows = (repos: RepositorySummary[]): Row[] =>
      repos.flatMap((repo) => repo.components.map((component) => ({ componentId: component.id, repositoryName: repo.name, component })));
    api
      .getProjectsOverview()
      .then((overview) => {
        const fromProjects = overview.projects.flatMap((p) => toRows(p.repositories));
        setRows([...fromProjects, ...toRows(overview.unassigned)]);
      })
      .catch((e) => {
        toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
        setRows([]);
      });
  }, [open, t]);

  const filtered = useMemo(() => {
    if (!rows) return [];
    const q = query.trim().toLowerCase();
    if (!q) return rows;
    return rows.filter((row) => `${row.repositoryName}/${row.component.path}`.toLowerCase().includes(q));
  }, [rows, query]);

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("repositoryPage.componentPicker.title")}
      description={t("repositoryPage.componentPicker.description")}
      footer={
        <Button variant="outline" onClick={() => onOpenChange(false)}>
          {t("common.cancel")}
        </Button>
      }
    >
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("repositoryPage.componentPicker.searchPlaceholder")}
          className="pl-8"
        />
      </div>
      <ScrollArea className="h-72 rounded-md border">
        {rows === null ? (
          <div className="space-y-2 p-3">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : filtered.length === 0 ? (
          <p className="p-4 text-center text-body text-muted-foreground">{t("repositoryPage.componentPicker.empty")}</p>
        ) : (
          <div className="divide-y divide-border/60">
            {filtered.map((row) => {
              const label = `${row.repositoryName}/${row.component.path === "." ? "" : row.component.path}`;
              return (
                <button
                  key={row.componentId}
                  type="button"
                  className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-body hover:bg-muted/40"
                  onClick={() => {
                    onPick(row.componentId, label);
                    onOpenChange(false);
                  }}
                >
                  <span className="truncate font-mono text-caption">{label}</span>
                  <RoleBadge role={row.component.role} className="shrink-0" />
                </button>
              );
            })}
          </div>
        )}
      </ScrollArea>
    </FormDialog>
  );
}
