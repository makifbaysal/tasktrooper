import { Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, type LocalCommand } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface Row {
  dir: string;
  argv: string;
}

interface LocalCommandsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  checkId: string;
  initial: LocalCommand[];
  onSaved: () => void;
}

/** Edits one check's local-command override: a list of {dir, argv} rows,
 * argv typed as one space-separated string and split on save. */
export function LocalCommandsDialog({ open, onOpenChange, checkId, initial, onSaved }: LocalCommandsDialogProps) {
  const { t } = useI18n();
  const [rows, setRows] = useState<Row[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    setRows(initial.length > 0 ? initial.map((c) => ({ dir: c.dir, argv: c.argv.join(" ") })) : [{ dir: "", argv: "" }]);
    // initial is a fresh array from the caller on every open; re-running per
    // reference would reset the draft on every parent re-render instead.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, checkId]);

  const save = async () => {
    setSaving(true);
    try {
      const commands: LocalCommand[] = rows
        .filter((r) => r.argv.trim())
        .map((r) => ({ dir: r.dir.trim(), argv: r.argv.trim().split(/\s+/) }));
      await api.updateCheck(checkId, { local_commands: commands });
      onSaved();
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("repositoryPage.localCommands.title")}
      description={t("repositoryPage.localCommands.description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={save} disabled={saving}>
            {t("repositoryPage.localCommands.save")}
          </Button>
        </>
      }
    >
      <div className="space-y-3">
        {rows.map((row, i) => (
          <div key={i} className="flex items-end gap-2">
            <div className="w-32 space-y-1">
              <Label className="text-xs text-muted-foreground">{t("repositoryPage.localCommands.dir")}</Label>
              <Input
                value={row.dir}
                placeholder={t("repositoryPage.localCommands.dirPlaceholder")}
                onChange={(e) => setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, dir: e.target.value } : r)))}
              />
            </div>
            <div className="flex-1 space-y-1">
              <Label className="text-xs text-muted-foreground">{t("repositoryPage.localCommands.argv")}</Label>
              <Input
                value={row.argv}
                className="font-mono"
                onChange={(e) => setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, argv: e.target.value } : r)))}
              />
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="shrink-0"
              onClick={() => setRows((prev) => prev.filter((_, idx) => idx !== i))}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
        <Button type="button" variant="outline" size="sm" onClick={() => setRows((prev) => [...prev, { dir: "", argv: "" }])}>
          <Plus className="mr-1.5 h-3.5 w-3.5" />
          {t("repositoryPage.localCommands.addRow")}
        </Button>
      </div>
    </FormDialog>
  );
}
