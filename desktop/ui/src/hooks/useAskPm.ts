import { useCallback, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { useI18n } from "@/hooks/useI18n";
import { useWorkspaceOutlet } from "@/hooks/useWorkspaceOutlet";
import { startLeadConversation } from "@/lib/leadAgent";

/**
 * Hands a message to the Product Manager: a new conversation opens with the
 * message already sent. `canAsk` is false when there is no lead agent (yet),
 * and the caller falls back to its own screen.
 */
export function useAskPm() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const lead = useWorkspaceOutlet()?.leadAgent ?? null;
  const [asking, setAsking] = useState(false);

  const ask = useCallback(
    async (message: string) => {
      if (!lead || asking) return false;
      setAsking(true);
      try {
        await startLeadConversation(navigate, lead.id, message, t("agentArea.chat.newSessionTitle", { name: lead.name }));
        return true;
      } catch {
        toast.error(t("audienceStudy.askPmFailed"));
        setAsking(false);
        return false;
      }
    },
    [lead, asking, navigate, t],
  );

  return { canAsk: lead !== null, asking, ask };
}
