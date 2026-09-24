import { Bot, Pencil, Plus, RotateCcw, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { api, type CheckGate, type CheckPurpose, CHECK_PURPOSES, type ComponentCheck, type RepositoryModel } from "@/api";
import { AddCheckDialog } from "@/components/projects/repository/AddCheckDialog";
import { ComponentRail } from "@/components/projects/repository/ComponentRail";
import { LocalCommandsDialog } from "@/components/projects/repository/LocalCommandsDialog";
import { evidenceLabel } from "@/components/projects/model/EvidenceList";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useI18n } from "@/hooks/useI18n";
import { factValue, isOverridden, localCommandsDisplay } from "@/lib/project-model";
import { cn } from "@/lib/utils";

interface ChecksTabProps {
  model: RepositoryModel;
  selectedComponentId: string | null;
  onSelectComponent: (id: string) => void;
  onReload: () => void;
}

const GATES: CheckGate[] = ["required", "info", "off"];

function workflowJobLabel(check: ComponentCheck): string {
  const basename = check.workflow.split("/").pop() || check.workflow;
  return `${basename} › ${check.job_name || check.job_key}`;
}

export function ChecksTab({ model, selectedComponentId, onSelectComponent, onReload }: ChecksTabProps) {
  const { t } = useI18n();
  const [addOpen, setAddOpen] = useState(false);
  const [localCommandsCheck, setLocalCommandsCheck] = useState<ComponentCheck | null>(null);
  const [deleteCheck, setDeleteCheck] = useState<ComponentCheck | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  if (model.components.length === 0) return null;
  const selected = model.components.find((c) => c.id === selectedComponentId) ?? model.components[0];
  const checks = model.checks.filter((c) => c.component_id === selected.id);
  const requiredLocalCommands = checks.filter(
    (c) => c.status === "active" && !c.missing && factValue(c.gate) === "required",
  );

  const patch = async (checkId: string, body: Parameters<typeof api.updateCheck>[1]) => {
    setBusyId(checkId);
    try {
      await api.updateCheck(checkId, body);
      onReload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="flex gap-6">
      <ComponentRail
        components={model.components}
        selectedId={selected.id}
        onSelect={(id) => id && onSelectComponent(id)}
        renderTrailing={(c) =>
          t("repositoryPage.checks.checksCount", {
            count: model.checks.filter((chk) => chk.component_id === c.id && chk.status === "active").length,
          })
        }
      />
      <div className="min-w-0 flex-1 space-y-4">
        <Card className="flex items-start gap-3 p-4">
          <Bot className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" aria-hidden />
          <div className="min-w-0 space-y-1">
            <p className="font-medium">{t("repositoryPage.checks.handoffTitle")}</p>
            {requiredLocalCommands.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t("repositoryPage.checks.handoffEmpty")}</p>
            ) : (
              <code className="block whitespace-pre-wrap break-all rounded-md bg-muted px-3 py-2 font-mono text-caption">
                {localCommandsDisplay(requiredLocalCommands.flatMap((c) => factValue(c.local_commands) ?? []))}
              </code>
            )}
          </div>
        </Card>

        <div className="flex justify-end">
          <Button size="sm" onClick={() => setAddOpen(true)}>
            <Plus className="mr-1.5 h-3.5 w-3.5" />
            {t("repositoryPage.checks.addCheck")}
          </Button>
        </div>

        <Card className="overflow-x-auto p-0">
          <table className="w-full text-body">
            <thead>
              <tr className="border-b border-border text-left text-micro text-muted-foreground">
                <th className="px-4 py-2 font-medium">{t("repositoryPage.checks.ciJob")}</th>
                <th className="px-4 py-2 font-medium">{t("repositoryPage.checks.purpose")}</th>
                <th className="px-4 py-2 font-medium">{t("repositoryPage.checks.trigger")}</th>
                <th className="px-4 py-2 font-medium">{t("repositoryPage.checks.localCommands")}</th>
                <th className="px-4 py-2 font-medium">{t("repositoryPage.checks.gate")}</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {checks.map((check) => {
                const commands = factValue(check.local_commands) ?? [];
                const gate = factValue(check.gate);
                const dismissed = check.status === "dismissed";
                return (
                  <tr key={check.id} className={cn("border-b border-border last:border-b-0 align-top", dismissed && "opacity-50")}>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-1.5">
                        <span className="font-mono text-caption">{workflowJobLabel(check)}</span>
                        {check.source === "manual" && <Badge variant="secondary">{t("repositoryPage.checks.manual")}</Badge>}
                        {check.missing && <Badge variant="warning">{t("repositoryPage.checks.missing")}</Badge>}
                      </div>
                      <p className="mt-1 text-micro text-muted-foreground">
                        {t("repositoryPage.checks.mappingFootnote", {
                          evidence:
                            (check.purpose.evidence?.length ?? 0) > 0
                              ? (check.purpose.evidence ?? []).map(evidenceLabel).join(", ")
                              : t("repositoryPage.checks.mappingUnknown"),
                        })}
                      </p>
                    </td>
                    <td className="px-4 py-3">
                      <PurposeCell
                        check={check}
                        disabled={busyId === check.id}
                        onChange={(p) => patch(check.id, { purpose: p })}
                        onRevert={() => patch(check.id, { purpose: null })}
                      />
                    </td>
                    <td className="px-4 py-3 text-micro text-muted-foreground">
                      {(check.triggers ?? []).join(", ") || "—"}
                      {(check.path_filters?.length ?? 0) > 0 && (
                        <div className="mt-0.5 font-mono">{check.path_filters!.join(", ")}</div>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-start gap-1.5">
                        <span className="font-mono text-caption">
                          {commands.length > 0 ? localCommandsDisplay(commands) : t("repositoryPage.checks.noLocalCommands")}
                        </span>
                        <Button variant="ghost" size="icon" className="h-6 w-6 shrink-0" onClick={() => setLocalCommandsCheck(check)}>
                          <Pencil className="h-3 w-3" />
                        </Button>
                      </div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-1">
                        {GATES.map((g) => (
                          <button
                            key={g}
                            type="button"
                            disabled={busyId === check.id}
                            onClick={() => patch(check.id, { gate: g })}
                            className={cn(
                              "rounded-full px-2 py-0.5 text-micro font-medium transition-colors",
                              gate === g ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground hover:bg-muted/70",
                            )}
                          >
                            {t(`projectModel.gates.${g}`)}
                          </button>
                        ))}
                        {isOverridden(check.gate) && (
                          <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => patch(check.id, { gate: null })}>
                            <RotateCcw className="h-3 w-3" />
                          </Button>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex items-center justify-end gap-1">
                        {dismissed ? (
                          <Button variant="ghost" size="sm" onClick={() => patch(check.id, { status: "active" })}>
                            {t("repositoryPage.checks.restore")}
                          </Button>
                        ) : (
                          <Button variant="ghost" size="sm" onClick={() => patch(check.id, { status: "dismissed" })}>
                            {t("repositoryPage.checks.dismiss")}
                          </Button>
                        )}
                        {check.source === "manual" && (
                          <Button variant="ghost" size="icon" className="h-7 w-7 text-muted-foreground hover:text-destructive" onClick={() => setDeleteCheck(check)}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </Card>
      </div>

      <AddCheckDialog open={addOpen} onOpenChange={setAddOpen} componentId={selected.id} onCreated={onReload} />
      {localCommandsCheck && (
        <LocalCommandsDialog
          open={Boolean(localCommandsCheck)}
          onOpenChange={(open) => !open && setLocalCommandsCheck(null)}
          checkId={localCommandsCheck.id}
          initial={factValue(localCommandsCheck.local_commands) ?? []}
          onSaved={onReload}
        />
      )}
      <ConfirmDialog
        open={Boolean(deleteCheck)}
        onOpenChange={(open) => !open && setDeleteCheck(null)}
        title={t("repositoryPage.checks.deleteConfirmTitle", { name: deleteCheck ? workflowJobLabel(deleteCheck) : "" })}
        description={t("repositoryPage.checks.deleteConfirmDesc")}
        confirmLabel={t("repositoryPage.checks.delete")}
        onConfirm={async () => {
          if (!deleteCheck) return;
          try {
            await api.deleteCheck(deleteCheck.id);
            onReload();
          } catch (e) {
            toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
          }
        }}
      />
    </div>
  );
}

function PurposeCell({
  check,
  disabled,
  onChange,
  onRevert,
}: {
  check: ComponentCheck;
  disabled: boolean;
  onChange: (purpose: CheckPurpose) => void;
  onRevert: () => void;
}) {
  const { t } = useI18n();
  const purpose = factValue(check.purpose);
  return (
    <div className="flex items-center gap-1.5">
      <Select value={purpose} onValueChange={(v) => onChange(v as CheckPurpose)}>
        <SelectTrigger className="h-8 w-32" disabled={disabled}>
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
      {isOverridden(check.purpose) && (
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onRevert}>
          <RotateCcw className="h-3 w-3" />
        </Button>
      )}
    </div>
  );
}
