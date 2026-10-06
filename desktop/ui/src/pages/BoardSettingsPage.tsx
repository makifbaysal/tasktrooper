import { Loader2, Plus, RotateCcw, Save, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, type BoardColumn, type BoardTransition } from "@/api";
import { TransitionGraph, type TransitionMap } from "@/components/admin/workflow/TransitionGraph";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";

function slugify(name: string): string {
  const map: Record<string, string> = {
    ç: "c", ğ: "g", ı: "i", ö: "o", ş: "s", ü: "u",
    Ç: "c", Ğ: "g", İ: "i", Ö: "o", Ş: "s", Ü: "u",
  };
  return name
    .split("")
    .map((c) => map[c] ?? c)
    .join("")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

export function BoardSettingsPage() {
  const { t } = useI18n();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [columns, setColumns] = useState<BoardColumn[]>([]);
  // from-slug -> Set of allowed to-slugs
  const [transitions, setTransitions] = useState<TransitionMap>({});
  const [defaults, setDefaults] = useState<BoardTransition[]>([]);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [newName, setNewName] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const cfg = await api.getWorkspaceConfig();
      setColumns(cfg.columns ?? []);
      const t: Record<string, Set<string>> = {};
      for (const tr of cfg.transitions ?? []) {
        if (!t[tr.from]) t[tr.from] = new Set();
        t[tr.from].add(tr.to);
      }
      setTransitions(t);
      setDefaults(cfg.default_transitions ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.board.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);

  const workflowCols = useMemo(() => columns.filter((c) => !c.is_backlog), [columns]);

  const addColumn = () => {
    const name = newName.trim();
    if (!name) return;
    const slug = slugify(name);
    if (!slug) {
      toast.error(t("settingsPages.board.invalidName"));
      return;
    }
    if (columns.some((c) => c.slug === slug)) {
      toast.error(t("settingsPages.board.duplicateName"));
      return;
    }
    const maxPos = columns.reduce((m, c) => Math.max(m, c.position), 0);
    setColumns((cols) => [
      ...cols,
      { id: "", slug, label: name, position: maxPos + 1, is_backlog: false },
    ]);
    setNewName("");
    setDialogOpen(false);
  };

  const removeColumn = (slug: string) => {
    setColumns((cols) => cols.filter((c) => c.slug !== slug));
    setTransitions((prev) => {
      const next: Record<string, Set<string>> = {};
      for (const [from, tos] of Object.entries(prev)) {
        if (from === slug) continue;
        next[from] = new Set([...tos].filter((to) => to !== slug));
      }
      return next;
    });
  };

  // Recommended rules for the columns this board has; a column the defaults
  // don't know stays free, as it would with no rules at all. Saved only on Save.
  const resetToDefaults = () => {
    const present = new Set(columns.map((c) => c.slug));
    const next: TransitionMap = {};
    for (const { from, to } of defaults) {
      if (!present.has(from) || !present.has(to)) continue;
      (next[from] ??= new Set()).add(to);
    }
    setTransitions(next);
  };

  const save = async () => {
    setSaving(true);
    try {
      await api.updateBoardColumns(
        columns.map((c) => ({
          slug: c.slug,
          label: c.label,
          position: c.position,
          is_backlog: c.is_backlog,
        })),
      );
      const flat: BoardTransition[] = [];
      const valid = new Set(columns.map((c) => c.slug));
      for (const [from, tos] of Object.entries(transitions)) {
        if (!valid.has(from)) continue;
        for (const to of tos) {
          if (valid.has(to) && from !== to) flat.push({ from, to });
        }
      }
      await api.setBoardTransitions(flat);
      toast.success(t("settingsPages.board.savedToast"));
      load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("settingsPages.board.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-48 rounded-xl" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    );
  }

  return (
    <div className="space-y-6 pb-8">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="font-semibold">{t("settingsPages.board.title")}</h2>
          <p className="mt-0.5 text-sm text-muted-foreground">
            {t("settingsPages.board.subtitle")}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => setDialogOpen(true)} className="gap-2">
            <Plus className="h-4 w-4" />
            {t("settingsPages.board.addColumn")}
          </Button>
          <Button onClick={save} disabled={saving} className="gap-2">
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {t("common.save")}
          </Button>
        </div>
      </div>

      <Card className="p-6">
        <h3 className="text-sm font-semibold">{t("settingsPages.board.columnsTitle")}</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          {t("settingsPages.board.columnsSubtitle")}
        </p>
        <div className="mt-4 divide-y divide-border overflow-hidden rounded-lg border border-border">
          <div className="flex items-center gap-3 bg-muted/30 px-4 py-3">
            <span className="flex-1 text-sm font-medium text-muted-foreground">{t("settingsPages.board.backlogLabel")}</span>
            <code className="text-xs text-muted-foreground">backlog</code>
            <span className="text-micro uppercase tracking-wide text-muted-foreground">{t("settingsPages.board.fixedTag")}</span>
          </div>
          {workflowCols.map((col) => (
            <div key={col.slug} className="flex items-center gap-3 px-4 py-3">
              <span className="flex-1 text-sm font-medium">{col.label}</span>
              <code className="text-xs text-muted-foreground">{col.slug}</code>
              <button
                type="button"
                className="text-muted-foreground hover:text-destructive"
                onClick={() => removeColumn(col.slug)}
                title={t("settingsPages.board.deleteColumn")}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          ))}
          {workflowCols.length === 0 && (
            <div className="px-4 py-6 text-center text-sm text-muted-foreground">
              {t("settingsPages.board.noColumns")}
            </div>
          )}
        </div>
      </Card>

      <Card className="p-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold">{t("settingsPages.board.transitionsTitle")}</h3>
            <p className="mt-1 text-sm text-muted-foreground">{t("settingsPages.board.transitionsSubtitle")}</p>
          </div>
          {defaults.length > 0 && (
            <Button variant="outline" size="sm" onClick={resetToDefaults} className="gap-1.5">
              <RotateCcw className="h-3.5 w-3.5" />
              {t("settingsPages.board.resetDefaults")}
            </Button>
          )}
        </div>
        <div className="mt-4">
          <TransitionGraph columns={columns} transitions={transitions} onChange={setTransitions} />
        </div>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settingsPages.board.newColumnTitle")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="col-name">{t("settingsPages.board.columnNameLabel")}</Label>
            <Input
              id="col-name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  addColumn();
                }
              }}
              placeholder={t("settingsPages.board.columnNamePlaceholder")}
              autoFocus
            />
            {newName.trim() && (
              <p className="text-xs text-muted-foreground">
                {t("settingsPages.board.slugPrefix")} <code>{slugify(newName)}</code>
              </p>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={addColumn} disabled={!newName.trim()}>
              {t("settingsPages.board.add")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
