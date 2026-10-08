import { useEffect, useState } from "react";
import type { MCPOAuthStartInput } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";

interface MCPOAuthClientDialogProps {
  serverId: string | null;
  redirectUri?: string;
  submitting: boolean;
  onCancel: () => void;
  onSubmit: (client: MCPOAuthStartInput) => void;
}

/**
 * Asks for a hand-registered OAuth client when the server's authorization
 * server offers no dynamic registration. The secret is sent once with the
 * start request and stored encrypted on the server; it is never read back.
 */
export function MCPOAuthClientDialog({ serverId, redirectUri, submitting, onCancel, onSubmit }: MCPOAuthClientDialogProps) {
  const { t } = useI18n();
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");

  useEffect(() => {
    setClientId("");
    setClientSecret("");
  }, [serverId]);

  return (
    <FormDialog
      open={serverId !== null}
      onOpenChange={(open) => !open && onCancel()}
      title={t("content.mcp.clientTitle", { id: serverId ?? "" })}
      description={t("content.mcp.clientDescription")}
      footer={
        <>
          <Button variant="outline" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button
            disabled={!clientId.trim() || submitting}
            onClick={() => onSubmit({ client_id: clientId.trim(), client_secret: clientSecret.trim() || undefined })}
          >
            {t("content.mcp.clientSubmit")}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {redirectUri && (
          <div className="space-y-1">
            <Label>{t("content.mcp.clientRedirect")}</Label>
            <code className="block select-all break-all rounded-md border border-border bg-muted/40 px-3 py-2 text-xs">
              {redirectUri}
            </code>
            <p className="text-xs text-muted-foreground">{t("content.mcp.clientRedirectHint")}</p>
          </div>
        )}
        <div className="space-y-2">
          <Label htmlFor="mcp-oauth-client-id">{t("content.mcp.clientId")}</Label>
          <Input id="mcp-oauth-client-id" value={clientId} onChange={(e) => setClientId(e.target.value)} autoComplete="off" />
        </div>
        <div className="space-y-2">
          <Label htmlFor="mcp-oauth-client-secret">{t("content.mcp.clientSecret")}</Label>
          <Input
            id="mcp-oauth-client-secret"
            type="password"
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
            autoComplete="off"
          />
        </div>
      </div>
    </FormDialog>
  );
}
