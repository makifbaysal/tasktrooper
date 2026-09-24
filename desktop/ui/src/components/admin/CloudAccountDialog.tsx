import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { api, isCloudAuthError, type CloudAccount, type CloudProviderKind } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

// A short, common list, not the full region catalog: the free-text fallback
// covers the rest without this Select growing to hundreds of items.
const COMMON_AWS_REGIONS = [
  "us-east-1",
  "us-east-2",
  "us-west-1",
  "us-west-2",
  "eu-west-1",
  "eu-west-2",
  "eu-central-1",
  "ap-southeast-1",
  "ap-southeast-2",
  "ap-northeast-1",
  "sa-east-1",
  "ca-central-1",
];
const CUSTOM_REGION = "__custom__";

interface CloudAccountDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  provider: CloudProviderKind;
  /** Present to replace this account's credential; absent to connect a new one. */
  account?: CloudAccount;
  onSaved: (account: CloudAccount) => void;
}

/**
 * Connects a new cloud account, or replaces an existing one's credential —
 * same fields either way, since the server never returns a saved credential
 * to prefill: every save fully replaces what is stored.
 */
export function CloudAccountDialog({ open, onOpenChange, provider, account, onSaved }: CloudAccountDialogProps) {
  const { t } = useI18n();
  const [label, setLabel] = useState("");
  const [token, setToken] = useState("");
  const [teamId, setTeamId] = useState("");
  const [json, setJson] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretAccessKey, setSecretAccessKey] = useState("");
  const [sessionToken, setSessionToken] = useState("");
  const [region, setRegion] = useState("");
  const [customRegion, setCustomRegion] = useState("");
  const [saving, setSaving] = useState(false);
  const [authError, setAuthError] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!open) return;
    setLabel(account?.label ?? "");
    setToken("");
    setTeamId("");
    setJson("");
    setAccessKeyId("");
    setSecretAccessKey("");
    setSessionToken("");
    setRegion("");
    setCustomRegion("");
    setAuthError("");
  }, [open, account]);

  const effectiveRegion = region === CUSTOM_REGION ? customRegion.trim() : region;

  const canSave =
    provider === "vercel"
      ? token.trim() !== ""
      : provider === "gcp"
        ? json.trim() !== ""
        : accessKeyId.trim() !== "" && secretAccessKey.trim() !== "" && effectiveRegion !== "";

  const readFile = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => setJson(String(reader.result ?? "").trim());
    reader.onerror = () => toast.error(t("cloud.dialog.gcp.fileReadFailed"));
    reader.readAsText(file);
  };

  const save = async () => {
    setAuthError("");
    setSaving(true);
    try {
      const fields: Record<string, string> = {};
      if (provider === "vercel") {
        fields.token = token.trim();
        if (teamId.trim()) fields.team_id = teamId.trim();
      } else if (provider === "gcp") {
        fields.service_account_json = json.trim();
      } else {
        fields.access_key_id = accessKeyId.trim();
        fields.secret_access_key = secretAccessKey.trim();
        fields.region = effectiveRegion;
        if (sessionToken.trim()) fields.session_token = sessionToken.trim();
      }
      const saved = account
        ? await api.updateCloudAccount(account.id, { label: label.trim() || undefined, fields })
        : await api.createCloudAccount({ provider, label: label.trim() || undefined, fields });
      toast.success(account ? t("cloud.dialog.replaced") : t("cloud.dialog.connected"));
      onSaved(saved);
    } catch (err) {
      if (isCloudAuthError(err)) {
        setAuthError(err instanceof Error ? err.message : t("cloud.dialog.authErrorGeneric"));
      } else {
        toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        account
          ? t("cloud.dialog.replaceTitle", { provider: t(`cloud.providers.${provider}`) })
          : t("cloud.dialog.connectTitle", { provider: t(`cloud.providers.${provider}`) })
      }
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving || !canSave}>
            {saving ? t("cloud.dialog.saving") : t("cloud.dialog.save")}
          </Button>
        </>
      }
    >
      {authError && (
        <Notice variant="error" title={t("cloud.dialog.authErrorTitle")}>
          {authError}
        </Notice>
      )}

      <div className="space-y-1">
        <Label htmlFor="cloud-account-label">{t("cloud.dialog.labelField")}</Label>
        <Input
          id="cloud-account-label"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          placeholder={t("cloud.dialog.labelPlaceholder")}
        />
      </div>

      {provider === "vercel" && (
        <>
          <div className="space-y-1">
            <Label htmlFor="cloud-vercel-token">{t("cloud.dialog.vercel.tokenLabel")}</Label>
            <Input
              id="cloud-vercel-token"
              type="password"
              autoComplete="off"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={t("cloud.dialog.vercel.tokenPlaceholder")}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="cloud-vercel-team">{t("cloud.dialog.vercel.teamLabel")}</Label>
            <Input
              id="cloud-vercel-team"
              value={teamId}
              onChange={(e) => setTeamId(e.target.value)}
              placeholder={t("cloud.dialog.vercel.teamPlaceholder")}
            />
          </div>
          <p className="text-xs text-muted-foreground">{t("cloud.dialog.vercel.permissions")}</p>
        </>
      )}

      {provider === "gcp" && (
        <>
          <div className="space-y-1">
            <Label htmlFor="cloud-gcp-json">{t("cloud.dialog.gcp.jsonLabel")}</Label>
            <Textarea
              id="cloud-gcp-json"
              value={json}
              onChange={(e) => setJson(e.target.value)}
              placeholder={t("cloud.dialog.gcp.jsonPlaceholder")}
              className="min-h-[100px] font-mono text-xs"
            />
            <input
              ref={fileInputRef}
              type="file"
              accept="application/json,.json"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0];
                if (file) readFile(file);
                e.target.value = "";
              }}
            />
            <Button type="button" variant="outline" size="sm" onClick={() => fileInputRef.current?.click()}>
              {t("cloud.dialog.gcp.pickFile")}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">{t("cloud.dialog.gcp.permissions")}</p>
        </>
      )}

      {provider === "aws" && (
        <>
          <div className="space-y-1">
            <Label htmlFor="cloud-aws-access-key">{t("cloud.dialog.aws.accessKeyIdLabel")}</Label>
            <Input
              id="cloud-aws-access-key"
              value={accessKeyId}
              onChange={(e) => setAccessKeyId(e.target.value)}
              autoComplete="off"
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="cloud-aws-secret-key">{t("cloud.dialog.aws.secretAccessKeyLabel")}</Label>
            <Input
              id="cloud-aws-secret-key"
              type="password"
              autoComplete="off"
              value={secretAccessKey}
              onChange={(e) => setSecretAccessKey(e.target.value)}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="cloud-aws-session-token">{t("cloud.dialog.aws.sessionTokenLabel")}</Label>
            <Input
              id="cloud-aws-session-token"
              type="password"
              autoComplete="off"
              value={sessionToken}
              onChange={(e) => setSessionToken(e.target.value)}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="cloud-aws-region">{t("cloud.dialog.aws.regionLabel")}</Label>
            <Select value={region} onValueChange={setRegion}>
              <SelectTrigger id="cloud-aws-region">
                <SelectValue placeholder={t("cloud.dialog.aws.regionPlaceholder")} />
              </SelectTrigger>
              <SelectContent>
                {COMMON_AWS_REGIONS.map((r) => (
                  <SelectItem key={r} value={r}>
                    {r}
                  </SelectItem>
                ))}
                <SelectItem value={CUSTOM_REGION}>{t("cloud.dialog.aws.regionCustom")}</SelectItem>
              </SelectContent>
            </Select>
            {region === CUSTOM_REGION && (
              <Input
                value={customRegion}
                onChange={(e) => setCustomRegion(e.target.value)}
                placeholder={t("cloud.dialog.aws.regionPlaceholder")}
                className="font-mono text-xs"
              />
            )}
          </div>
          <p className="text-xs text-muted-foreground">{t("cloud.dialog.aws.permissions")}</p>
        </>
      )}
    </FormDialog>
  );
}
