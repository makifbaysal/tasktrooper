import { useEffect, useMemo, useState } from "react";
import { api, type ToolPolicy } from "@/api";
import { MCPServerPicker } from "@/components/admin/MCPServerPicker";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { useI18n } from "@/hooks/useI18n";
import {
  builtinToolLabel,
  isBuiltinTool,
  selectedBuiltinTools,
  selectedMCPServers,
  withAllowedBuiltinTools,
  withAllowedMCPServers,
} from "@/lib/toolPolicy";

interface ToolPolicyFormProps {
  value: ToolPolicy;
  onChange: (policy: ToolPolicy) => void;
}

export function ToolPolicyForm({ value, onChange }: ToolPolicyFormProps) {
  const { t } = useI18n();
  const [tools, setTools] = useState<{ name: string; description: string }[]>([]);
  const [mcpServerIds, setMcpServerIds] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    Promise.all([api.listTools(true), api.listMCPServers()])
      .then(([toolsData, mcpData]) => {
        setTools(
          (toolsData.tools ?? [])
            .map((tool) => ({
              name: tool.function?.name ?? "",
              description: tool.function?.description ?? "",
            }))
            .filter((tool) => tool.name && isBuiltinTool(tool.name)),
        );
        setMcpServerIds((mcpData.servers ?? []).map((s) => s.id));
      })
      .catch(() => {
        setTools([]);
        setMcpServerIds([]);
      })
      .finally(() => setLoading(false));
  }, []);

  const builtinNames = useMemo(() => tools.map((t) => t.name), [tools]);

  const toolOptions = useMemo(
    () =>
      tools.map((tool) => ({
        value: tool.name,
        label: builtinToolLabel(tool.name),
        description: tool.description,
      })),
    [tools],
  );

  const selectedTools = useMemo(
    () => selectedBuiltinTools(value, builtinNames),
    [value, builtinNames],
  );

  const selectedServers = useMemo(
    () => selectedMCPServers(value, mcpServerIds),
    [value, mcpServerIds],
  );

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        {t("frame.admin.toolPolicy.description")}
      </p>

      <MultiSelectPicker
        label={t("frame.admin.toolPolicy.builtinTools")}
        options={toolOptions}
        selected={selectedTools}
        onChange={(allow_tools) => onChange(withAllowedBuiltinTools(value, allow_tools))}
        loading={loading}
        emptyText={t("frame.admin.toolPolicy.noBuiltinTools")}
      />

      <div className="space-y-1.5">
        <MCPServerPicker
          label={t("frame.admin.toolPolicy.mcpServers")}
          selected={selectedServers}
          onChange={(ids) => onChange(withAllowedMCPServers(value, ids))}
        />
        <p className="text-xs text-muted-foreground">{t("frame.admin.toolPolicy.mcpServersHint")}</p>
      </div>
    </div>
  );
}
