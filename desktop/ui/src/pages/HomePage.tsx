import { useState } from "react";
import { Users } from "lucide-react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { LeadWelcome } from "@/components/chat/LeadWelcome";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { useWorkspaceOutlet } from "@/hooks/useWorkspaceOutlet";
import { startLeadConversation } from "@/lib/leadAgent";

export function HomePage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const ctx = useWorkspaceOutlet();
  const [starting, setStarting] = useState(false);
  const lead = ctx?.leadAgent ?? null;

  if (lead) {
    const start = async (message: string) => {
      if (starting) return;
      setStarting(true);
      try {
        await startLeadConversation(
          navigate,
          lead.id,
          message,
          t("agentArea.chat.newSessionTitle", { name: lead.name }),
        );
      } catch {
        toast.error(t("agentArea.chat.lead.welcome.failed"));
        setStarting(false);
      }
    };
    return (
      <div className="flex h-full min-h-0 flex-1 flex-col">
        <LeadWelcome agent={lead} onSubmit={start} busy={starting} />
      </div>
    );
  }
  if (ctx?.workspaceLoading) {
    return (
      <div className="flex flex-1 items-center justify-center py-20">
        <Spinner size="lg" />
      </div>
    );
  }
  if (ctx?.teamPreparing) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <EmptyState
          icon={Users}
          title={t("frame.layout.home.preparing.title")}
          description={t("frame.layout.home.preparing.description")}
          action={
            <div className="flex flex-col items-center gap-4">
              <Spinner size="sm" />
              <Button variant="outline" asChild>
                <Link to="/board">{t("frame.layout.home.preparing.toBoard")}</Link>
              </Button>
            </div>
          }
        />
      </div>
    );
  }
  return <Navigate to="/board" replace />;
}
