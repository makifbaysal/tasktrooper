import { useMemo } from "react";
import type { MCPAccess, MCPEnvSchemaField, MCPServerView } from "@/api";
import { KeyValueEditor } from "@/components/admin/KeyValueEditor";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import {
  type MCPServerFormState,
  resolveSecretFields,
  CUSTOM_TEMPLATE_ID,
} from "@/lib/mcpForm";
import type { MCPTemplate } from "@/lib/mcpTemplates";
import { exampleFolderPath } from "@/lib/platform";

interface MCPServerFormProps {
  form: MCPServerFormState;
  editing: MCPServerView | null;
  templateId: string;
  availableTemplates: MCPTemplate[];
  existingIds: Set<string>;
  onFormChange: (form: MCPServerFormState) => void;
  onTemplateChange: (templateId: string) => void;
}

export function MCPServerForm({
  form,
  editing,
  templateId,
  availableTemplates,
  existingIds,
  onFormChange,
  onTemplateChange,
}: MCPServerFormProps) {
  const { t } = useI18n();
  const secretFields = useMemo(() => {
    if (editing) {
      return resolveSecretFields(editing);
    }
    const template = availableTemplates.find((t) => t.id === templateId);
    return resolveSecretFields({
      env: form.env,
      headers: form.headers,
      secret_fields: template?.secret_fields,
      env_schema: template?.env_schema,
    });
  }, [editing, availableTemplates, templateId, form.env, form.headers]);

  const envSchema = useMemo((): MCPEnvSchemaField[] => {
    if (editing?.env_schema?.length) {
      return editing.env_schema;
    }
    const template = availableTemplates.find((t) => t.id === templateId);
    return template?.env_schema ?? [];
  }, [editing, availableTemplates, templateId]);

  const missingRequired = useMemo(
    () =>
      (editing?.missing_config ?? []).map((field) =>
        field.location === "args" ? field.description || `args[${field.key}]` : field.key,
      ),
    [editing],
  );

  const update = (patch: Partial<MCPServerFormState>) => {
    onFormChange({ ...form, ...patch });
  };

  const updateSecretDraft = (key: string, value: string) => {
    onFormChange({
      ...form,
      secretDrafts: { ...form.secretDrafts, [key]: value },
    });
  };

  const idConflict = !editing && form.id.trim() !== "" && existingIds.has(form.id.trim());
  const isCustom = !editing && templateId === CUSTOM_TEMPLATE_ID;

  return (
    <div className="space-y-4">
      {!editing && (
        <div className="space-y-2">
          <Label>{t("frame.admin.mcpForm.template")}</Label>
          <Select value={templateId} onValueChange={onTemplateChange}>
            <SelectTrigger>
              <SelectValue placeholder={t("frame.admin.mcpForm.selectTemplate")} />
            </SelectTrigger>
            <SelectContent>
              {availableTemplates.map((template) => (
                <SelectItem key={template.id} value={template.id}>
                  {template.label} — {template.description}
                </SelectItem>
              ))}
              <SelectItem value={CUSTOM_TEMPLATE_ID}>{t("frame.admin.mcpForm.customServer")}</SelectItem>
            </SelectContent>
          </Select>
          {availableTemplates.length === 0 && templateId !== CUSTOM_TEMPLATE_ID && (
            <p className="text-xs text-muted-foreground">
              {t("frame.admin.mcpForm.allTemplatesAdded")}
            </p>
          )}
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="mcp-id">{t("frame.admin.mcpForm.id")}</Label>
        {editing || !isCustom ? (
          <div className="rounded-md border border-border bg-muted/40 px-3 py-2 text-sm">{form.id}</div>
        ) : (
          <Input
            id="mcp-id"
            value={form.id}
            onChange={(e) => update({ id: e.target.value })}
            placeholder={t("frame.admin.mcpForm.idPlaceholder")}
          />
        )}
        {idConflict && <p className="text-xs text-destructive">{t("frame.admin.mcpForm.idConflict")}</p>}
      </div>

      <div className="flex items-center gap-2">
        <Switch checked={form.enabled} onCheckedChange={(enabled) => update({ enabled })} />
        <Label>{t("frame.admin.mcpForm.enabled")}</Label>
      </div>

      <div className="space-y-2">
        <Label htmlFor="mcp-access">{t("frame.admin.mcpForm.access")}</Label>
        <Select value={form.access} onValueChange={(access: MCPAccess) => update({ access })}>
          <SelectTrigger id="mcp-access">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="listed">{t("frame.admin.mcpForm.accessListed")}</SelectItem>
            <SelectItem value="all">{t("frame.admin.mcpForm.accessAll")}</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          {form.access === "all"
            ? t("frame.admin.mcpForm.accessAllHint")
            : t("frame.admin.mcpForm.accessListedHint")}
        </p>
      </div>

      <div className="space-y-2">
        <Label>Transport</Label>
        <Select
          value={form.transport}
          onValueChange={(transport: "stdio" | "http") => update({ transport })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="stdio">stdio</SelectItem>
            <SelectItem value="http">http</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {form.transport === "stdio" && (
        <>
          <div className="space-y-2">
            <Label htmlFor="mcp-command">{t("frame.admin.mcpForm.command")}</Label>
            <Input
              id="mcp-command"
              value={form.command}
              onChange={(e) => update({ command: e.target.value })}
              placeholder="npx"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="mcp-args">{t("frame.admin.mcpForm.args")}</Label>
            <Textarea
              id="mcp-args"
              value={form.argsText}
              onChange={(e) => update({ argsText: e.target.value })}
              placeholder={`-y\n@modelcontextprotocol/server-filesystem\n${exampleFolderPath()}`}
              className="min-h-[100px] font-mono text-xs"
            />
          </div>
        </>
      )}

      {form.transport === "http" && (
        <div className="space-y-2">
          <Label htmlFor="mcp-url">URL</Label>
          <Input
            id="mcp-url"
            value={form.url}
            onChange={(e) => update({ url: e.target.value })}
            placeholder="https://example.com/mcp"
          />
        </div>
      )}

      {missingRequired.length > 0 && (
        <p className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs leading-snug text-warning">
          {t("frame.admin.mcpForm.missingRequired", { fields: missingRequired.join(", ") })}
        </p>
      )}

      <KeyValueEditor
        label={t("frame.admin.mcpForm.envVars")}
        description={envSchema.length > 0 ? t("frame.admin.mcpForm.envHint") : undefined}
        entries={form.env}
        secretKeys={secretFields}
        secretDrafts={form.secretDrafts}
        storedSecrets={form.storedSecrets}
        onEntriesChange={(env) => update({ env })}
        onSecretDraftChange={updateSecretDraft}
      />

      {form.transport === "http" && (
        <KeyValueEditor
          label={t("frame.admin.mcpForm.httpHeaders")}
          entries={form.headers}
          secretKeys={secretFields}
          secretDrafts={form.secretDrafts}
          storedSecrets={form.storedSecrets}
          secretKeyPrefix="header:"
          onEntriesChange={(headers) => update({ headers })}
          onSecretDraftChange={updateSecretDraft}
        />
      )}

      <div className="space-y-2">
        <Label htmlFor="mcp-tools">{t("frame.admin.mcpForm.allowedTools")}</Label>
        <Textarea
          id="mcp-tools"
          value={form.allowedToolsText}
          onChange={(e) => update({ allowedToolsText: e.target.value })}
          placeholder="model_search"
          className="min-h-[72px] font-mono text-xs"
        />
      </div>
    </div>
  );
}

export function isFormValid(form: MCPServerFormState, editing: MCPServerView | null, existingIds: Set<string>): boolean {
  if (!form.id.trim()) return false;
  if (!editing && existingIds.has(form.id.trim())) return false;
  if (form.transport === "stdio" && !form.command.trim()) return false;
  if (form.transport === "http" && !form.url.trim()) return false;
  return true;
}
