import { NavLink, Outlet, useNavigate, useParams } from "react-router-dom";
import { useCallback, useEffect, useMemo, useState } from "react";
import { ArrowLeft, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type Agent } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Spinner } from "@/components/ui/spinner";
import { PageContent } from "@/components/layout/PageContent";
import { PageSuspense } from "@/components/layout/PageSuspense";
import { useI18n } from "@/hooks/useI18n";
import { cn } from "@/lib/utils";

export function WorkspaceAgentLayout() {
  const { t } = useI18n();
  const { agentId } = useParams();
  const navigate = useNavigate();
  const tabs = [
    { to: "settings", label: t("frame.layout.agentLayout.settings") },
    { to: "skills", label: t("frame.layout.agentLayout.skills") },
    { to: "rules", label: t("frame.layout.agentLayout.rules") },
    { to: "columns", label: t("frame.layout.agentLayout.columns") },
    { to: "memory", label: t("frame.layout.agentLayout.memory") },
    { to: "performance", label: t("frame.layout.agentLayout.performance") },
  ];
  const [agent, setAgent] = useState<Agent | null>(null);
  const [loading, setLoading] = useState(true);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const load = useCallback(async () => {
    if (!agentId || agentId === "new") {
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      setAgent(await api.getAgent(agentId));
    } catch {
      setAgent(null);
    } finally {
      setLoading(false);
    }
  }, [agentId]);

  useEffect(() => {
    load();
  }, [load]);

  const outletContext = useMemo(() => ({ agent, refreshAgent: load }), [agent, load]);
  const isNew = agentId === "new";

  const handleDelete = async () => {
    if (!agentId || isNew) return;
    setDeleting(true);
    try {
      await api.deleteAgent(agentId);
      toast.success(t("frame.layout.agentLayout.deleted"));
      navigate("/board");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("frame.layout.agentLayout.deleteFailed"));
    } finally {
      setDeleting(false);
      setDeleteOpen(false);
    }
  };

  if (!agentId) return null;

  return (
    <PageContent className="flex min-h-0 flex-1 flex-col">
      <div className="mb-6 border-b border-border pb-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            {loading ? (
              <Spinner size="sm" />
            ) : (
              <>
                <h1 className="text-display font-semibold">
                  {isNew ? t("frame.layout.agentLayout.newAgent") : agent?.name ?? t("frame.layout.agentLayout.agent")}
                </h1>
                {agent && (
                  <Badge variant={agent.enabled ? "success" : "secondary"}>
                    {agent.enabled ? t("frame.layout.agentLayout.enabled") : t("frame.layout.agentLayout.disabled")}
                  </Badge>
                )}
              </>
            )}
          </div>
          <div className="flex items-center gap-2">
            {!isNew && agent && (
              <Button variant="outline" size="sm" className="gap-2" asChild>
                <NavLink to={`/agents/${agentId}/chat`}>
                  <ArrowLeft className="h-4 w-4" />
                  {t("frame.layout.agentLayout.backToChat")}
                </NavLink>
              </Button>
            )}
            {!isNew && (
              <Button
                variant="outline"
                size="sm"
                className="gap-2 text-destructive hover:text-destructive"
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 className="h-4 w-4" />
                {t("frame.layout.agentLayout.delete")}
              </Button>
            )}
          </div>
        </div>
        {!isNew && (
          <nav className="mt-3 flex flex-wrap gap-1">
            {tabs.map(({ to, label }) => (
              <NavLink
                key={to}
                to={`/agents/${agentId}/${to}`}
                className={({ isActive }) =>
                  cn(
                    "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                    isActive
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground",
                  )
                }
              >
                {label}
              </NavLink>
            ))}
          </nav>
        )}
      </div>

      <PageSuspense>
        <Outlet context={outletContext} />
      </PageSuspense>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("frame.layout.agentLayout.deleteTitle")}
        description={t("frame.layout.agentLayout.deleteDescription")}
        confirmLabel={t("frame.layout.agentLayout.delete")}
        loading={deleting}
        onConfirm={handleDelete}
      />
    </PageContent>
  );
}
