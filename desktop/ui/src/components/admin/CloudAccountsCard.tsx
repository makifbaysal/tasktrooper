import { ChevronDown, Cloud, Pencil, Plug, RefreshCw, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { api, CLOUD_PROVIDERS, type CloudAccount, type CloudProviderKind } from "@/api";
import { CloudAccountDialog } from "@/components/admin/CloudAccountDialog";
import { FormDialog } from "@/components/admin/FormDialog";
import { ProviderIcon } from "@/components/projects/model/ProviderIcon";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { useI18n } from "@/hooks/useI18n";
import { cn, formatDate } from "@/lib/utils";

const STATUS_VARIANT: Record<CloudAccount["status"], NonNullable<BadgeProps["variant"]>> = {
  ok: "success",
  error: "destructive",
  unverified: "secondary",
};

/** Vercel reports team_name/team_slug/username, GCP project_id/client_email,
 * AWS account_id/region; each provider fills only its own keys. */
function accountIdentity(meta: Record<string, string>): string {
  return [
    meta.team_name || meta.team_slug || meta.username,
    meta.project_id,
    meta.account_id,
    meta.region,
    meta.client_email,
  ]
    .filter(Boolean)
    .join(" · ");
}

interface RenameDialogProps {
  account: CloudAccount | null;
  onOpenChange: (open: boolean) => void;
  onRenamed: (account: CloudAccount) => void;
}

function RenameDialog({ account, onOpenChange, onRenamed }: RenameDialogProps) {
  const { t } = useI18n();
  const [label, setLabel] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setLabel(account?.label ?? "");
  }, [account]);

  const save = async () => {
    if (!account) return;
    setSaving(true);
    try {
      const updated = await api.updateCloudAccount(account.id, { label: label.trim() });
      toast.success(t("cloud.accounts.renameSaved"));
      onRenamed(updated);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormDialog
      open={account !== null}
      onOpenChange={onOpenChange}
      title={t("cloud.accounts.renameTitle")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("common.cancel")}
          </Button>
          <Button onClick={() => void save()} disabled={saving || !label.trim()}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <div className="space-y-1">
        <Label htmlFor="cloud-account-rename">{t("cloud.dialog.labelField")}</Label>
        <Input id="cloud-account-rename" value={label} onChange={(e) => setLabel(e.target.value)} />
      </div>
    </FormDialog>
  );
}

interface AccountRowProps {
  account: CloudAccount;
  verifying: boolean;
  onVerify: () => void;
  onRename: () => void;
  onReplace: () => void;
  onRemove: () => void;
}

function AccountRow({ account, verifying, onVerify, onRename, onReplace, onRemove }: AccountRowProps) {
  const { t } = useI18n();
  const identity = accountIdentity(account.meta);

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
      <div className="flex min-w-0 items-start gap-2.5">
        <ProviderIcon provider={account.provider} className="mt-0.5 h-4 w-4 shrink-0" />
        <div className="min-w-0 space-y-0.5">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="truncate text-sm font-medium">{account.label}</span>
            <Badge variant={STATUS_VARIANT[account.status]}>{t(`cloud.accounts.status.${account.status}`)}</Badge>
          </div>
          {identity && <p className="truncate font-mono text-xs text-muted-foreground">{identity}</p>}
          {account.status === "error" && account.status_detail && (
            <p className="text-xs text-destructive">{account.status_detail}</p>
          )}
          <p className="text-xs text-muted-foreground">
            {account.verified_at
              ? t("cloud.accounts.verifiedAt", { date: formatDate(account.verified_at) })
              : t("cloud.accounts.neverVerified")}
          </p>
        </div>
      </div>
      <div className="flex shrink-0 flex-wrap gap-1.5">
        <Button variant="outline" size="sm" onClick={onVerify} disabled={verifying}>
          <RefreshCw className={cn("mr-1.5 h-3 w-3", verifying && "animate-spin")} />
          {t("cloud.accounts.verify")}
        </Button>
        <Button variant="outline" size="sm" onClick={onRename}>
          <Pencil className="mr-1.5 h-3 w-3" />
          {t("cloud.accounts.rename")}
        </Button>
        <Button variant="outline" size="sm" onClick={onReplace}>
          {t("cloud.accounts.replaceCredential")}
        </Button>
        <Button variant="ghost" size="sm" onClick={onRemove}>
          <Trash2 className="mr-1.5 h-3 w-3" />
          {t("cloud.accounts.remove")}
        </Button>
      </div>
    </div>
  );
}

interface CloudAccountsCardProps {
  className?: string;
}

/**
 * Every connected cloud provider account: Vercel, Google Cloud, AWS — read
 * only, used to bind component environments and read their deployments, logs
 * and errors. Replaces the old per-provider VercelCard/GoogleCloudCard, which
 * only ever held one account each and mixed the connection with hosting
 * links that Phase 2 environments now own instead.
 */
export function CloudAccountsCard({ className }: CloudAccountsCardProps) {
  const { t } = useI18n();
  const [accounts, setAccounts] = useState<CloudAccount[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [verifyingId, setVerifyingId] = useState<string | null>(null);
  const [connectProvider, setConnectProvider] = useState<CloudProviderKind | null>(null);
  const [renameTarget, setRenameTarget] = useState<CloudAccount | null>(null);
  const [replaceTarget, setReplaceTarget] = useState<CloudAccount | null>(null);
  const [removeTarget, setRemoveTarget] = useState<CloudAccount | null>(null);
  const [removing, setRemoving] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await api.listCloudAccounts();
      setAccounts(res.accounts);
    } catch (err) {
      setAccounts([]);
      toast.error(err instanceof Error ? err.message : t("cloud.accounts.loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const replaceAccount = (updated: CloudAccount) => {
    setAccounts((prev) => (prev ?? []).map((a) => (a.id === updated.id ? updated : a)));
  };

  const verify = async (account: CloudAccount) => {
    setVerifyingId(account.id);
    try {
      const updated = await api.verifyCloudAccount(account.id);
      replaceAccount(updated);
      if (updated.status === "ok") toast.success(t("cloud.accounts.verified"));
      else toast.error(updated.status_detail || t("cloud.accounts.verifyFailed"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("cloud.accounts.verifyFailed"));
    } finally {
      setVerifyingId(null);
    }
  };

  const remove = async () => {
    if (!removeTarget) return;
    setRemoving(true);
    try {
      await api.deleteCloudAccount(removeTarget.id);
      setAccounts((prev) => (prev ?? []).filter((a) => a.id !== removeTarget.id));
      toast.success(t("cloud.accounts.removed"));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setRemoving(false);
      setRemoveTarget(null);
    }
  };

  return (
    <Card className={cn("mt-4 w-full space-y-3 p-6", className)}>
      <div className="flex items-center justify-between gap-2">
        <Label className="flex items-center gap-2">
          <Cloud className="h-4 w-4" />
          {t("cloud.accounts.title")}
        </Label>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm">
              <Plug className="mr-1.5 h-3.5 w-3.5" />
              {t("cloud.accounts.connect")}
              <ChevronDown className="ml-1.5 h-3 w-3" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent>
            {CLOUD_PROVIDERS.map((provider) => (
              <DropdownMenuItem key={provider} onSelect={() => setConnectProvider(provider)} className="gap-2">
                <ProviderIcon provider={provider} />
                {t("cloud.accounts.connectProvider", { provider: t(`cloud.providers.${provider}`) })}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <p className="text-sm text-muted-foreground">{t("cloud.accounts.description")}</p>

      {loading ? (
        <Skeleton className="h-16 w-full" />
      ) : (accounts?.length ?? 0) === 0 ? (
        <p className="text-sm text-muted-foreground">{t("cloud.accounts.empty")}</p>
      ) : (
        <div className="divide-y divide-border rounded-md border border-border/60">
          {(accounts ?? []).map((account) => (
            <AccountRow
              key={account.id}
              account={account}
              verifying={verifyingId === account.id}
              onVerify={() => void verify(account)}
              onRename={() => setRenameTarget(account)}
              onReplace={() => setReplaceTarget(account)}
              onRemove={() => setRemoveTarget(account)}
            />
          ))}
        </div>
      )}

      {connectProvider && (
        <CloudAccountDialog
          open={connectProvider !== null}
          onOpenChange={(open) => !open && setConnectProvider(null)}
          provider={connectProvider}
          onSaved={(saved) => {
            setAccounts((prev) => [...(prev ?? []), saved]);
            setConnectProvider(null);
          }}
        />
      )}

      {replaceTarget && (
        <CloudAccountDialog
          open={replaceTarget !== null}
          onOpenChange={(open) => !open && setReplaceTarget(null)}
          provider={replaceTarget.provider}
          account={replaceTarget}
          onSaved={(saved) => {
            replaceAccount(saved);
            setReplaceTarget(null);
          }}
        />
      )}

      <RenameDialog
        account={renameTarget}
        onOpenChange={(open) => !open && setRenameTarget(null)}
        onRenamed={replaceAccount}
      />

      <ConfirmDialog
        open={removeTarget !== null}
        onOpenChange={(open) => !open && setRemoveTarget(null)}
        title={t("cloud.accounts.removeTitle")}
        description={t("cloud.accounts.removeDescription", { label: removeTarget?.label ?? "" })}
        confirmLabel={t("cloud.accounts.remove")}
        loading={removing}
        onConfirm={remove}
      />
    </Card>
  );
}
