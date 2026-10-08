import { Plus, Server } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api, isOAuthClientRequiredError, type Agent, type MCPOAuthStartInput, type MCPServerView } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { MCPOAuthClientDialog } from "@/components/admin/MCPOAuthClientDialog";
import { isFormValid, MCPServerForm } from "@/components/admin/MCPServerForm";
import { MCPServerTableRow } from "@/components/admin/MCPServerTableRow";
import { PageHeader } from "@/components/admin/PageHeader";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { usePolling } from "@/hooks/usePolling";
import {
  buildCreatePayload,
  buildUpdatePayload,
  CUSTOM_TEMPLATE_ID,
  formStateFromCustom,
  formStateFromServer,
  formStateFromTemplate,
  type MCPServerFormState,
} from "@/lib/mcpForm";
import { agentsListingServer, openAuthorizationPage } from "@/lib/mcpAccess";
import { MCP_TEMPLATES } from "@/lib/mcpTemplates";

const SIGN_IN_POLL_MS = 2000;
const SIGN_IN_TIMEOUT_MS = 5 * 60 * 1000;

interface PendingSignIn {
  serverId: string;
  url: string;
  startedAt: number;
}

export function MCPServersPage() {
  const { t } = useI18n();
  const [servers, setServers] = useState<MCPServerView[]>([]);
  const [initialLoading, setInitialLoading] = useState(true);
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<MCPServerView | null>(null);
  const [templateId, setTemplateId] = useState("");
  const [form, setForm] = useState<MCPServerFormState | null>(null);
  const [saving, setSaving] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [togglingId, setTogglingId] = useState<string | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [redirectUri, setRedirectUri] = useState("");
  const [signIn, setSignIn] = useState<PendingSignIn | null>(null);
  const [clientFor, setClientFor] = useState<string | null>(null);
  const [authBusyId, setAuthBusyId] = useState<string | null>(null);

  const refresh = useCallback(async (silent = false) => {
    if (!silent) setInitialLoading(true);
    try {
      const data = await api.listMCPServers();
      setServers(data.servers ?? []);
      setRedirectUri(data.oauth_redirect_uri ?? "");
    } catch (e) {
      if (!silent) {
        toast.error(e instanceof Error ? e.message : t("content.mcp.loadFailed"));
      }
    } finally {
      if (!silent) setInitialLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void refresh(false);
  }, [refresh]);
  useEffect(() => {
    api
      .listAgents()
      .then((data) => setAgents(data.agents ?? []))
      .catch(() => setAgents([]));
  }, []);
  // Silent background refresh, gated on tab visibility: a hidden tab stops
  // polling entirely and refreshes once when it comes back. Faster while a
  // sign-in is open in the browser, so "Signed in" shows up right after it.
  usePolling(() => refresh(true), signIn ? SIGN_IN_POLL_MS : 5000, true);

  useEffect(() => {
    if (!signIn) return;
    const server = servers.find((s) => s.id === signIn.serverId);
    if (server?.auth === "oauth_connected") {
      toast.success(t("content.mcp.signInDone", { id: signIn.serverId }));
      setSignIn(null);
      return;
    }
    const remaining = SIGN_IN_TIMEOUT_MS - (Date.now() - signIn.startedAt);
    const timer = window.setTimeout(() => {
      toast.error(t("content.mcp.signInTimedOut", { id: signIn.serverId }));
      setSignIn(null);
    }, Math.max(0, remaining));
    return () => window.clearTimeout(timer);
  }, [signIn, servers, t]);

  const listedBy = useMemo(() => {
    const out = new Map<string, string[]>();
    for (const server of servers) out.set(server.id, agentsListingServer(agents, server.id));
    return out;
  }, [agents, servers]);

  const startSignIn = async (serverId: string, client?: MCPOAuthStartInput) => {
    setAuthBusyId(serverId);
    try {
      const started = await api.startMCPOAuth(serverId, client);
      setClientFor(null);
      setSignIn({ serverId, url: started.authorization_url, startedAt: Date.now() });
      toast.info(t("content.mcp.signInStarted", { id: serverId }));
      await openAuthorizationPage(started.authorization_url);
    } catch (e) {
      if (isOAuthClientRequiredError(e)) {
        setClientFor(serverId);
      } else {
        toast.error(e instanceof Error && e.message ? e.message : t("content.mcp.signInFailed"));
      }
    } finally {
      setAuthBusyId(null);
    }
  };

  const disconnect = async (serverId: string) => {
    setAuthBusyId(serverId);
    try {
      await api.disconnectMCPOAuth(serverId);
      toast.success(t("content.mcp.disconnectedToast", { id: serverId }));
      await refresh(true);
    } catch (e) {
      toast.error(e instanceof Error && e.message ? e.message : t("content.mcp.disconnectFailed"));
    } finally {
      setAuthBusyId(null);
    }
  };

  const existingIds = useMemo(() => new Set(servers.map((s) => s.id)), [servers]);
  const availableTemplates = useMemo(
    () => MCP_TEMPLATES.filter((t) => !existingIds.has(t.id)),
    [existingIds],
  );

  const openCreate = () => {
    setEditing(null);
    const initialTemplate = availableTemplates[0]?.id ?? CUSTOM_TEMPLATE_ID;
    setTemplateId(initialTemplate);
    if (initialTemplate === CUSTOM_TEMPLATE_ID) {
      setForm(formStateFromCustom());
    } else {
      const template = MCP_TEMPLATES.find((t) => t.id === initialTemplate);
      setForm(template ? formStateFromTemplate(template) : formStateFromCustom());
    }
    setFormOpen(true);
  };

  const openEdit = (server: MCPServerView) => {
    setEditing(server);
    setTemplateId(server.id);
    setForm(formStateFromServer(server));
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
    setForm(null);
    setTemplateId("");
  };

  const handleTemplateChange = (id: string) => {
    setTemplateId(id);
    if (id === CUSTOM_TEMPLATE_ID) {
      setForm(formStateFromCustom());
      return;
    }
    const template = MCP_TEMPLATES.find((t) => t.id === id);
    if (template) {
      setForm(formStateFromTemplate(template));
    }
  };

  const handleSave = async () => {
    if (!form) return;
    setSaving(true);
    try {
      if (editing) {
        await api.updateMCPServer(editing.id, buildUpdatePayload(form, editing));
        toast.success(t("content.mcp.updatedToast"));
      } else {
        await api.createMCPServer(buildCreatePayload(form));
        toast.success(t("content.mcp.createdToast"));
      }
      closeForm();
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.saveFailed"));
    } finally {
      setSaving(false);
    }
  };

  const handleToggle = async (server: MCPServerView, enabled: boolean) => {
    setTogglingId(server.id);
    try {
      const formState = formStateFromServer(server);
      formState.enabled = enabled;
      await api.updateMCPServer(server.id, buildUpdatePayload(formState, server));
      toast.success(enabled ? t("content.mcp.enabledToast") : t("content.mcp.disabledToast"));
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.mcp.updateFailed"));
    } finally {
      setTogglingId(null);
    }
  };

  const handleDelete = async () => {
    if (!deleteId) return;
    setDeleting(true);
    try {
      await api.deleteMCPServer(deleteId);
      toast.success(t("content.mcp.deletedToast"));
      await refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("content.mcp.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteId(null);
    }
  };

  const canSave = form ? isFormValid(form, editing, existingIds) : false;

  return (
    <>
      <PageHeader
        title={t("content.mcp.title")}
        description={t("content.mcp.description")}
        action={
          <Button onClick={openCreate} className="gap-2">
            <Plus className="h-4 w-4" />
            {t("content.mcp.addServer")}
          </Button>
        }
      />

      {initialLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : servers.length === 0 ? (
        <EmptyState
          icon={Server}
          title={t("content.mcp.emptyTitle")}
          description={t("content.mcp.emptyDescription")}
          action={
            <Button onClick={openCreate} className="gap-2">
              <Plus className="h-4 w-4" />
              {t("content.mcp.addFirstServer")}
            </Button>
          }
        />
      ) : (
        <Card className="overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium">{t("content.mcp.colServer")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.mcp.colTransport")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.mcp.colAccess")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.mcp.colStatus")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.mcp.colTools")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("content.mcp.colEnabled")}</th>
                  <th className="px-4 py-3 w-24" />
                </tr>
              </thead>
              <tbody>
                {servers.map((server) => (
                  <MCPServerTableRow
                    key={server.id}
                    server={server}
                    expanded={expandedId === server.id}
                    onToggleExpand={() =>
                      setExpandedId((current) => (current === server.id ? null : server.id))
                    }
                    toggling={togglingId === server.id}
                    onToggleEnabled={(enabled) => handleToggle(server, enabled)}
                    onEdit={() => openEdit(server)}
                    onDelete={() => setDeleteId(server.id)}
                    listedBy={listedBy.get(server.id)}
                    onConnect={() => void startSignIn(server.id)}
                    onDisconnect={() => void disconnect(server.id)}
                    signInUrl={signIn?.serverId === server.id ? signIn.url : null}
                    authBusy={authBusyId === server.id}
                  />
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <FormDialog
        open={formOpen}
        onOpenChange={(open) => !open && closeForm()}
        title={editing ? t("content.mcp.editTitle") : t("content.mcp.createTitle")}
        description={editing ? t("content.mcp.editDescription") : t("content.mcp.createDescription")}
        className="sm:max-w-2xl"
        footer={
          <>
            <Button variant="outline" onClick={closeForm}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleSave} disabled={!canSave || saving}>
              {saving ? t("common.saving") : editing ? t("content.mcp.update") : t("content.mcp.add")}
            </Button>
          </>
        }
      >
        {form && (
          <MCPServerForm
            form={form}
            editing={editing}
            templateId={templateId}
            availableTemplates={availableTemplates}
            existingIds={existingIds}
            onFormChange={setForm}
            onTemplateChange={handleTemplateChange}
          />
        )}
      </FormDialog>

      <MCPOAuthClientDialog
        serverId={clientFor}
        redirectUri={redirectUri}
        submitting={clientFor !== null && authBusyId === clientFor}
        onCancel={() => setClientFor(null)}
        onSubmit={(client) => {
          if (clientFor) void startSignIn(clientFor, client);
        }}
      />

      <ConfirmDialog
        open={deleteId !== null}
        onOpenChange={(open) => !open && setDeleteId(null)}
        title={t("content.mcp.deleteTitle")}
        description={t("content.mcp.deleteDescription", { id: deleteId ?? "" })}
        confirmLabel={t("content.mcp.deleteConfirm")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </>
  );
}
