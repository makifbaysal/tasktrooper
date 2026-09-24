import { Box, Pencil, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import {
  api,
  COMMAND_PURPOSES,
  COMPONENT_ROLES,
  type Component,
  type ComponentCommand,
  type ComponentGates,
  type CommandPurpose,
  type ComponentRole,
  type RepositoryDocs,
  type RepositoryModel,
} from "@/api";
import { AddComponentDialog } from "@/components/projects/repository/AddComponentDialog";
import { ComponentRail } from "@/components/projects/repository/ComponentRail";
import { evidenceLabel } from "@/components/projects/model/EvidenceList";
import { FactValue } from "@/components/projects/model/FactValue";
import { RoleBadge } from "@/components/projects/model/RoleBadge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { HelpTooltip } from "@/components/ui/help-tooltip";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";
import { componentLabel, factValue, isOverridden } from "@/lib/project-model";

interface ComponentsTabProps {
  model: RepositoryModel;
  repositoryId: string;
  selectedComponentId: string | null;
  onSelectComponent: (id: string) => void;
  onReload: () => void;
}

export function ComponentsTab({ model, repositoryId, selectedComponentId, onSelectComponent, onReload }: ComponentsTabProps) {
  const { t } = useI18n();
  const [addOpen, setAddOpen] = useState(false);

  if (model.components.length === 0) {
    return (
      <Card className="p-6">
        <EmptyState
          icon={Box}
          title={t("repositoryPage.components.empty")}
          description={t("repositoryPage.components.emptyDesc")}
          action={
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              {t("repositoryPage.components.addComponent")}
            </Button>
          }
        />
        <AddComponentDialog
          open={addOpen}
          onOpenChange={setAddOpen}
          repositoryId={repositoryId}
          existingPaths={model.components.map((c) => c.path)}
          onCreated={(c) => {
            onReload();
            onSelectComponent(c.id);
          }}
        />
      </Card>
    );
  }

  const selected = model.components.find((c) => c.id === selectedComponentId) ?? model.components[0];

  return (
    <div className="flex gap-6">
      <ComponentRail
        components={model.components}
        selectedId={selected.id}
        onSelect={(id) => id && onSelectComponent(id)}
        footer={
          <Button variant="ghost" size="sm" className="mt-1 justify-start gap-1.5" onClick={() => setAddOpen(true)}>
            <Plus className="h-3.5 w-3.5" />
            {t("repositoryPage.components.addComponent")}
          </Button>
        }
      />
      <div className="min-w-0 flex-1 space-y-4">
        <ComponentDetail key={selected.id} component={selected} model={model} onReload={onReload} />
      </div>
      <AddComponentDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        repositoryId={repositoryId}
        existingPaths={model.components.map((c) => c.path)}
        onCreated={(c) => {
          onReload();
          onSelectComponent(c.id);
        }}
      />
    </div>
  );
}

function ComponentDetail({ component, model, onReload }: { component: Component; model: RepositoryModel; onReload: () => void }) {
  const { t } = useI18n();
  const [dismissOpen, setDismissOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const patch = async (body: Parameters<typeof api.updateComponent>[1], quiet = false) => {
    setBusy(true);
    try {
      await api.updateComponent(component.id, body);
      onReload();
    } catch (e) {
      if (!quiet) toast.error(e instanceof Error ? e.message : t("repositoryPage.components.saveFailed"));
    } finally {
      setBusy(false);
    }
  };

  const dismissed = component.status === "dismissed";

  return (
    <>
      <div className="flex items-center justify-between gap-3">
        <h2 className="truncate font-mono text-title font-semibold">
          {componentLabel(component, model.repository.name)}
        </h2>
        {dismissed ? (
          <Button size="sm" variant="outline" disabled={busy} onClick={() => patch({ status: "active" })}>
            {t("repositoryPage.components.restore")}
          </Button>
        ) : (
          <Button size="sm" variant="outline" disabled={busy} onClick={() => setDismissOpen(true)}>
            {t("repositoryPage.components.dismiss")}
          </Button>
        )}
      </div>

      <IdentityCard component={component} patch={patch} />
      <StackCard component={component} />
      <CommandsCard component={component} patch={patch} />
      <GatesCard component={component} patch={patch} />
      <DocsCard component={component} patch={patch} />
      {factValue(component.role) === "mobile" && <MobileCard component={component} />}

      <ConfirmDialog
        open={dismissOpen}
        onOpenChange={setDismissOpen}
        variant="default"
        title={t("repositoryPage.components.dismissConfirmTitle", { name: componentLabel(component, model.repository.name) })}
        description={t("repositoryPage.components.dismissConfirmDesc")}
        confirmLabel={t("repositoryPage.components.dismiss")}
        onConfirm={() => patch({ status: "dismissed" })}
      />
    </>
  );
}

interface CardProps {
  component: Component;
  patch: (body: Parameters<typeof api.updateComponent>[1], quiet?: boolean) => Promise<void>;
}

function IdentityCard({ component, patch }: CardProps) {
  const { t } = useI18n();
  const [name, setName] = useState(factValue(component.name) ?? "");

  useEffect(() => setName(factValue(component.name) ?? ""), [component.id, component.name.override, component.name.detected]);

  const role = factValue(component.role);

  return (
    <Card className="space-y-4 p-6">
      <h3 className="font-semibold">{t("repositoryPage.components.identityTitle")}</h3>
      <div className="grid gap-4 sm:grid-cols-3">
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t("repositoryPage.components.path")}</Label>
          <p className="font-mono text-body">{component.path === "." ? "/" : component.path}</p>
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t("repositoryPage.components.role")}</Label>
          <Select value={role ?? ""} onValueChange={(v) => patch({ role: v as ComponentRole })}>
            <SelectTrigger>
              <SelectValue>{role && <RoleBadge role={role} />}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {COMPONENT_ROLES.map((r) => (
                <SelectItem key={r} value={r}>
                  {t(`projectModel.roles.${r}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FactValue fact={component.role} render={() => null} onRevert={isOverridden(component.role) ? () => patch({ role: null }) : undefined} />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t("repositoryPage.components.name")}</Label>
          <Input
            value={name}
            placeholder={t("repositoryPage.components.namePlaceholder")}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => {
              const trimmed = name.trim();
              if (trimmed && trimmed !== factValue(component.name)) void patch({ name: trimmed });
            }}
          />
          <FactValue fact={component.name} render={() => null} onRevert={isOverridden(component.name) ? () => patch({ name: null }) : undefined} />
        </div>
      </div>
    </Card>
  );
}

function StackCard({ component }: { component: Component }) {
  const { t } = useI18n();
  const stack = factValue(component.stack);
  const rows: { label: string; value: string; evidence?: { path: string; line?: number; note?: string } }[] = [];
  for (const item of stack?.languages ?? []) {
    rows.push({ label: t("repositoryPage.components.languages"), value: item.version ? `${item.name} ${item.version}` : item.name, evidence: item.evidence });
  }
  for (const item of stack?.frameworks ?? []) {
    rows.push({ label: t("repositoryPage.components.frameworks"), value: item.version ? `${item.name} ${item.version}` : item.name, evidence: item.evidence });
  }
  for (const item of stack?.libraries ?? []) {
    rows.push({ label: t("repositoryPage.components.libraries"), value: item.version ? `${item.name} ${item.version}` : item.name, evidence: item.evidence });
  }
  if (stack?.runtime) {
    rows.push({
      label: t("repositoryPage.components.runtime"),
      value: stack.runtime.version ? `${stack.runtime.name} ${stack.runtime.version}` : stack.runtime.name,
      evidence: stack.runtime.evidence,
    });
  }
  if (stack?.package_manager) rows.push({ label: t("repositoryPage.components.packageManager"), value: stack.package_manager });
  if (stack?.container) rows.push({ label: t("repositoryPage.components.container"), value: stack.container });

  return (
    <Card className="space-y-3 p-6">
      <h3 className="font-semibold">{t("repositoryPage.components.stackTitle")}</h3>
      {rows.length === 0 ? (
        <p className="text-body text-muted-foreground">{t("repositoryPage.components.noStack")}</p>
      ) : (
        <table className="w-full text-body">
          <tbody>
            {rows.map((row, i) => (
              <tr key={i} className="border-t border-border first:border-t-0">
                <td className="w-40 py-2 pr-3 text-muted-foreground">{row.label}</td>
                <td className="py-2 pr-3">{row.value}</td>
                <td className="py-2 text-right">
                  {row.evidence && <HelpTooltip text={evidenceLabel(row.evidence)} />}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

function CommandsCard({ component, patch }: CardProps) {
  const { t } = useI18n();
  const [editing, setEditing] = useState<CommandPurpose | null>(null);
  const [draft, setDraft] = useState("");
  const [addingPurpose, setAddingPurpose] = useState<CommandPurpose | "">("");
  const [addingValue, setAddingValue] = useState("");

  const present = new Set(component.commands.map((c) => c.purpose));
  const missing = COMMAND_PURPOSES.filter((p) => !present.has(p));

  const startEdit = (cmd: ComponentCommand) => {
    setEditing(cmd.purpose);
    setDraft(factValue(cmd.command) ?? "");
  };

  const save = async (purpose: CommandPurpose, value: string) => {
    if (!value.trim()) return;
    await patch({ commands: { [purpose]: value.trim() } });
    setEditing(null);
  };

  return (
    <Card className="space-y-3 p-6">
      <div className="flex items-center justify-between">
        <h3 className="font-semibold">{t("repositoryPage.components.commandsTitle")}</h3>
      </div>
      <table className="w-full text-body">
        <tbody>
          {component.commands.map((cmd) => (
            <tr key={cmd.purpose} className="border-t border-border first:border-t-0">
              <td className="w-32 py-2 pr-3 text-muted-foreground">{t(`projectModel.commandPurposes.${cmd.purpose}`)}</td>
              <td className="py-2 pr-3">
                {editing === cmd.purpose ? (
                  <div className="flex items-center gap-2">
                    <Input
                      value={draft}
                      autoFocus
                      onChange={(e) => setDraft(e.target.value)}
                      onKeyDown={(e) => e.key === "Enter" && void save(cmd.purpose, draft)}
                      className="font-mono text-body"
                    />
                    <Button size="sm" onClick={() => void save(cmd.purpose, draft)}>
                      {t("common.save")}
                    </Button>
                  </div>
                ) : (
                  <FactValue
                    fact={cmd.command}
                    render={(v) => <code className="font-mono">{v}</code>}
                    onRevert={isOverridden(cmd.command) ? () => patch({ commands: { [cmd.purpose]: null } }) : undefined}
                  />
                )}
              </td>
              <td className="py-2 text-right">
                {editing !== cmd.purpose && (
                  <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => startEdit(cmd)}>
                    <Pencil className="h-3.5 w-3.5" />
                  </Button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {missing.length > 0 && (
        <div className="flex items-center gap-2 border-t border-border pt-3">
          <Select value={addingPurpose} onValueChange={(v) => setAddingPurpose(v as CommandPurpose)}>
            <SelectTrigger className="w-40">
              <SelectValue placeholder={t("repositoryPage.components.addCommand")} />
            </SelectTrigger>
            <SelectContent>
              {missing.map((p) => (
                <SelectItem key={p} value={p}>
                  {t(`projectModel.commandPurposes.${p}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            value={addingValue}
            onChange={(e) => setAddingValue(e.target.value)}
            placeholder={t("repositoryPage.components.commandValuePlaceholder")}
            className="flex-1 font-mono text-body"
          />
          <Button
            size="sm"
            disabled={!addingPurpose || !addingValue.trim()}
            onClick={async () => {
              if (!addingPurpose) return;
              await save(addingPurpose, addingValue);
              setAddingPurpose("");
              setAddingValue("");
            }}
          >
            <Plus className="mr-1 h-3.5 w-3.5" />
            {t("repositoryPage.components.addCommand")}
          </Button>
        </div>
      )}
    </Card>
  );
}

function GatesCard({ component, patch }: CardProps) {
  const { t } = useI18n();
  const gates = component.gates ?? {};

  const setGate = (next: ComponentGates) => void patch({ gates: next });

  return (
    <Card className="space-y-4 p-6">
      <h3 className="font-semibold">{t("repositoryPage.components.gatesTitle")}</h3>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
          <div>
            <p className="text-body">{t("repositoryPage.components.coverageEnabled")}</p>
            {gates.coverage_enabled === undefined && (
              <p className="text-micro text-muted-foreground">{t("repositoryPage.components.inherited")}</p>
            )}
          </div>
          <Switch
            checked={gates.coverage_enabled ?? false}
            onCheckedChange={(checked) => setGate({ ...gates, coverage_enabled: checked })}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t("repositoryPage.components.coverageThreshold")}</Label>
          <Input
            type="number"
            min={0}
            max={100}
            value={gates.coverage_threshold ?? ""}
            onChange={(e) => setGate({ ...gates, coverage_threshold: e.target.value === "" ? undefined : Number(e.target.value) })}
          />
        </div>
        <div className="flex items-center justify-between gap-3 rounded-md border border-border p-3">
          <div>
            <p className="text-body">{t("repositoryPage.components.mutationEnabled")}</p>
            {gates.mutation_enabled === undefined && (
              <p className="text-micro text-muted-foreground">{t("repositoryPage.components.inherited")}</p>
            )}
          </div>
          <Switch
            checked={gates.mutation_enabled ?? false}
            onCheckedChange={(checked) => setGate({ ...gates, mutation_enabled: checked })}
          />
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t("repositoryPage.components.mutationThreshold")}</Label>
          <Input
            type="number"
            min={0}
            max={100}
            value={gates.mutation_threshold ?? ""}
            onChange={(e) => setGate({ ...gates, mutation_threshold: e.target.value === "" ? undefined : Number(e.target.value) })}
          />
        </div>
      </div>
    </Card>
  );
}

function DocsCard({ component, patch }: CardProps) {
  const { t } = useI18n();
  const [docs, setDocs] = useState<RepositoryDocs>(component.docs ?? {});

  useEffect(() => setDocs(component.docs ?? {}), [component.id]);

  const fields: { key: keyof RepositoryDocs; labelKey: string }[] = [
    { key: "coding_standards", labelKey: "repositoryPage.components.docsCodingStandards" },
    { key: "test_standards", labelKey: "repositoryPage.components.docsTestStandards" },
    { key: "architecture", labelKey: "repositoryPage.components.docsArchitecture" },
    { key: "local_run", labelKey: "repositoryPage.components.docsLocalRun" },
  ];

  return (
    <Card className="space-y-3 p-6">
      <h3 className="font-semibold">{t("repositoryPage.components.docsTitle")}</h3>
      <div className="grid gap-3 sm:grid-cols-2">
        {fields.map((f) => (
          <div key={f.key} className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">{t(f.labelKey)}</Label>
            <Input value={docs[f.key] ?? ""} onChange={(e) => setDocs((prev) => ({ ...prev, [f.key]: e.target.value }))} />
          </div>
        ))}
      </div>
      <div className="flex justify-end">
        <Button size="sm" onClick={() => void patch({ docs })}>
          {t("common.save")}
        </Button>
      </div>
    </Card>
  );
}

function MobileCard({ component }: { component: Component }) {
  const { t } = useI18n();
  const facts = component.mobile ? factValue(component.mobile) : undefined;

  return (
    <Card className="space-y-3 p-6">
      <h3 className="font-semibold">{t("repositoryPage.components.mobileTitle")}</h3>
      {!facts ? (
        <p className="text-body text-muted-foreground">{t("repositoryPage.components.noMobileFacts")}</p>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t("repositoryPage.components.mobilePlatform")} value={facts.platform} />
          <Field label={t("repositoryPage.components.bundleId")} value={facts.identity.bundle_id} />
          <Field label={t("repositoryPage.components.packageName")} value={facts.identity.package_name} />
          <Field label={t("repositoryPage.components.xcodeScheme")} value={facts.build_targets.xcode_scheme} />
          <Field label={t("repositoryPage.components.gradleModule")} value={facts.build_targets.gradle_module} />
        </div>
      )}
    </Card>
  );
}

function Field({ label, value }: { label: string; value?: string }) {
  return (
    <div className="space-y-1">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="font-mono text-body">{value || "—"}</p>
    </div>
  );
}
