import { useEffect, useMemo, useState } from "react";
import { api, type MCPServerView } from "@/api";
import { MultiSelectPicker } from "@/components/admin/MultiSelectPicker";
import { useI18n } from "@/hooks/useI18n";

interface MCPServerPickerProps {
  label?: string;
  selected: string[];
  onChange: (ids: string[]) => void;
}

export function MCPServerPicker({ label, selected, onChange }: MCPServerPickerProps) {
  const { t } = useI18n();
  const resolvedLabel = label ?? t("frame.admin.mcpPicker.defaultLabel");
  const [servers, setServers] = useState<MCPServerView[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    setLoading(true);
    api
      .listMCPServers()
      .then((data) => setServers(data.servers ?? []))
      .catch(() => setServers([]))
      .finally(() => setLoading(false));
  }, []);

  const options = useMemo(
    () =>
      servers.map((server) => {
        const status =
          server.status === "connected"
            ? t("frame.admin.mcpPicker.connected")
            : server.enabled === false
              ? t("frame.admin.mcpPicker.disabled")
              : server.status;
        const access =
          (server.access ?? "all") === "all"
            ? t("frame.admin.mcpPicker.accessAll")
            : t("frame.admin.mcpPicker.accessListed");
        return { value: server.id, label: server.id, description: `${status} · ${access}` };
      }),
    [servers, t],
  );

  return (
    <MultiSelectPicker
      label={resolvedLabel}
      options={options}
      selected={selected}
      onChange={onChange}
      loading={loading}
      emptyText={t("frame.admin.mcpPicker.notFound")}
    />
  );
}
