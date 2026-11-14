import { useState } from "react";
import { Users } from "lucide-react";
import { Link, Navigate, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import type { AttachmentMeta, MessageMention } from "@/api";
import { LeadWelcome } from "@/components/chat/LeadWelcome";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { useMentionOptions } from "@/hooks/useMentionOptions";
import { useWorkspaceOutlet } from "@/hooks/useWorkspaceOutlet";
import { useVoiceAvailable } from "@/hooks/useVoiceConversation";
import { startLeadConversation, startLeadVoiceConversation } from "@/lib/leadAgent";
import { agentRole, logEvent } from "@/lib/analytics";

export function HomePage() {
  const { t, lang } = useI18n();
  const navigate = useNavigate();
  const voiceAvailable = useVoiceAvailable(lang);
  const ctx = useWorkspaceOutlet();
  const [starting, setStarting] = useState(false);
  const mentionOptions = useMentionOptions();
  const lead = ctx?.leadAgent ?? null;

  if (lead) {
    const start = async (message: string, mentions: MessageMention[], attachments: AttachmentMeta[]) => {
      if (starting) return;
      setStarting(true);
      try {
        await startLeadConversation(
          navigate,
          lead.id,
          message,
          t("agentArea.chat.newSessionTitle", { name: lead.name }),
          mentions,
          attachments,
        );
        logEvent("chat_start", { agent_role: agentRole(lead), source: "home_quick_ask" });
      } catch {
        toast.error(t("agentArea.chat.lead.welcome.failed"));
        setStarting(false);
      }
    };
    const talk = async () => {
      if (starting) return;
      setStarting(true);
      try {
        await startLeadVoiceConversation(navigate, lead.id, t("agentArea.chat.newSessionTitle", { name: lead.name }));
        logEvent("chat_start", { agent_role: agentRole(lead), source: "home_voice" });
      } catch {
        toast.error(t("agentArea.chat.lead.welcome.failed"));
        setStarting(false);
      }
    };
    return (
      <div className="flex h-full min-h-0 flex-1 flex-col">
        <LeadWelcome
          agent={lead}
          onSubmit={start}
          busy={starting}
          mentionOptions={mentionOptions}
          onStartVoice={voiceAvailable ? () => void talk() : undefined}
        />
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
        <EmptyState mascot="working"
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
