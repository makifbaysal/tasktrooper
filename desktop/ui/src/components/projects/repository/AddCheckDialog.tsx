import { Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api, CHECK_GATES, CHECK_PURPOSES, type CheckGate, type CheckPurpose, type ComponentCheck, type LocalCommand } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";

interface Row {
  dir: string;
  argv: string;
}

interface AddCheckDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  componentId: string;
  onCreated: (check: ComponentCheck) => void;
}

export function AddCheckDialog({ open, onOpenChange, componentId, onCreated }: AddCheckDialogProps) {
  const { t } = useI18n();
  const [name, setName] = useState("");
  const [purpose, setPurpose] = useState<CheckPurpose>("test");
  const [gate, setGate] = useState<CheckGate>("required");
  const [rows, setRows] = useState<Row[]>([{ dir: "", argv: "" }]);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!open) return;
    setName("");
    setPurpose("test");
    setGate("required");
    setRows([{ dir: "", argv: "" }]);
  }, [open]);

  const submit = async () => {
    if (!name.trim()) return;
    setSubmitting(true);
    try {
      const local_commands: LocalCommand[] = rows
        .filter((r) => r.argv.trim())
        .map((r) => ({ dir: r.dir.trim(), argv: r.argv.trim().split(/\s+/) }));
      const created = await api.createCheck(componentId, { name: name.trim(), purpose, gate, local_commands });
      onCreated(created);
      onOpenChange(false);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("repositoryPage.addCheck.title")}
      description={t("repositoryPage.addCheck.description")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={submitting || !name.trim()}>
            {t("repositoryPage.addCheck.submit")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label>{t("repositoryPage.addCheck.name")}</Label>
        <Input value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-2">
          <Label>{t("repositoryPage.addCheck.purpose")}</Label>
          <Select value={purpose} onValueChange={(v) => setPurpose(v as CheckPurpose)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CHECK_PURPOSES.map((p) => (
                <SelectItem key={p} value={p}>
                  {t(`projectModel.checkPurposes.${p}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>{t("repositoryPage.addCheck.gate")}</Label>
          <Select value={gate} onValueChange={(v) => setGate(v as CheckGate)}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CHECK_GATES.map((g) => (
                <SelectItem key={g} value={g}>
                  {t(`projectModel.gates.${g}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="space-y-2">
        <Label>{t("repositoryPage.addCheck.localCommands")}</Label>
        {rows.map((row, i) => (
          <div key={i} className="flex items-center gap-2">
            <Input
              value={row.dir}
              placeholder={t("repositoryPage.localCommands.dirPlaceholder")}
              className="w-28"
              onChange={(e) => setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, dir: e.target.value } : r)))}
            />
            <Input
              value={row.argv}
              placeholder={t("repositoryPage.addCheck.argv")}
              className="flex-1 font-mono"
              onChange={(e) => setRows((prev) => prev.map((r, idx) => (idx === i ? { ...r, argv: e.target.value } : r)))}
            />
            <Button type="button" variant="ghost" size="icon" onClick={() => setRows((prev) => prev.filter((_, idx) => idx !== i))}>
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        ))}
        <Button type="button" variant="outline" size="sm" onClick={() => setRows((prev) => [...prev, { dir: "", argv: "" }])}>
          <Plus className="mr-1.5 h-3.5 w-3.5" />
          {t("repositoryPage.addCheck.addRow")}
        </Button>
      </div>
    </FormDialog>
  );
}
