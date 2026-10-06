import { useCallback, useState } from "react";
import { toast } from "sonner";
import { api, type StoreCredentialProvider, type StoreCredentialView } from "@/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { formatDate } from "@/lib/utils";

interface AscForm {
  key_id: string;
  issuer_id: string;
  p8: string;
}

interface PlayForm {
  service_account_json: string;
}

const EMPTY_ASC: AscForm = { key_id: "", issuer_id: "", p8: "" };
const EMPTY_PLAY: PlayForm = { service_account_json: "" };

function canSave(provider: StoreCredentialProvider, ascForm: AscForm, playForm: PlayForm): boolean {
  if (provider === "asc") {
    return ascForm.key_id.trim() !== "" && ascForm.issuer_id.trim() !== "" && ascForm.p8.trim() !== "";
  }
  return playForm.service_account_json.trim() !== "";
}

interface StoreCredentialFormProps {
  provider: StoreCredentialProvider;
  /** Undefined until the vault has been read — the row is rendered as "not configured". */
  credential?: StoreCredentialView;
  onChanged: () => void;
  /** A saved key folds to a one-line summary until "Replace key" — the Integrations page. */
  collapseWhenSaved?: boolean;
  /** The caller shows the connection state itself. */
  hideStatus?: boolean;
}

/**
 * One provider's slot in the store credential vault: what is stored now, the
 * inputs that replace it, and the delete.
 *
 * Split out of the old two-provider section so Settings → Integrations can
 * give App Store Connect and Google Play a card each; the behaviour is
 * unchanged. The API never returns the stored payload (key material / service
 * account JSON), so the inputs always start empty: every save fully replaces
 * what is stored, nothing round-trips, and the server keeps a save only after
 * the store console has verified it.
 */
export function StoreCredentialForm({
  provider,
  credential,
  onChanged,
  collapseWhenSaved = false,
  hideStatus = false,
}: StoreCredentialFormProps) {
  const { t } = useI18n();
  const [ascForm, setAscForm] = useState<AscForm>(EMPTY_ASC);
  const [playForm, setPlayForm] = useState<PlayForm>(EMPTY_PLAY);
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [editing, setEditing] = useState(false);
  const saved = credential?.configured ?? false;

  const providerLabel =
    provider === "asc"
      ? t("projectAdmin.prodOps.storeCredentialASC")
      : t("projectAdmin.prodOps.storeCredentialPlay");

  const save = useCallback(async () => {
    setBusy(true);
    try {
      // Trim on submit, matching what canSave already validates: a key_id
      // pasted from a console with a trailing space would otherwise reach
      // App Store Connect verbatim and fail validation for no visible
      // reason.
      const data: Record<string, string> =
        provider === "asc"
          ? {
              key_id: ascForm.key_id.trim(),
              issuer_id: ascForm.issuer_id.trim(),
              p8: ascForm.p8.trim(),
            }
          : { service_account_json: playForm.service_account_json.trim() };
      await api.saveStoreCredential(provider, data);
      if (provider === "asc") setAscForm(EMPTY_ASC);
      else setPlayForm(EMPTY_PLAY);
      setEditing(false);
      toast.success(t("projectAdmin.prodOps.storeCredentialSaved"));
      onChanged();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
    }
  }, [provider, ascForm, playForm, onChanged, t]);

  const remove = useCallback(async () => {
    setBusy(true);
    try {
      await api.deleteStoreCredential(provider);
      toast.success(t("projectAdmin.prodOps.storeCredentialDeleted"));
      onChanged();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setBusy(false);
      setConfirmDelete(false);
    }
  }, [provider, onChanged, t]);

  const confirm = (
    <ConfirmDialog
      open={confirmDelete}
      onOpenChange={(open) => !open && setConfirmDelete(false)}
      title={t("projectAdmin.prodOps.storeCredentialDeleteTitle")}
      description={t("projectAdmin.prodOps.storeCredentialDeleteDescription", { provider: providerLabel })}
      confirmLabel={t("projectAdmin.prodOps.storeCredentialDelete")}
      loading={busy}
      onConfirm={remove}
    />
  );

  if (collapseWhenSaved && saved && !editing && credential) {
    return (
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-xs text-muted-foreground">
          {t("settingsPages.integrations.keySaved", { date: formatDate(credential.updated_at) })}
        </span>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
            {t("settingsPages.integrations.replaceKey")}
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmDelete(true)}>
            {t("projectAdmin.prodOps.storeCredentialDelete")}
          </Button>
        </div>
        {confirm}
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className={hideStatus ? "hidden" : "flex flex-wrap items-center gap-2"}>
        {credential?.configured ? (
          <Badge variant="success">{t("projectAdmin.prodOps.storeCredentialConfigured")}</Badge>
        ) : (
          <Badge variant="outline">{t("projectAdmin.prodOps.storeCredentialNotConfigured")}</Badge>
        )}
        {credential?.configured && (
          <span className="text-xs text-muted-foreground">
            {t("projectAdmin.prodOps.storeCredentialUpdated", { date: formatDate(credential.updated_at) })}
          </span>
        )}
      </div>

      {provider === "asc" ? (
        <div className="space-y-3">
          <div className="space-y-1">
            <Label htmlFor="asc-key-id">{t("projectAdmin.prodOps.storeCredentialKeyId")}</Label>
            <Input
              id="asc-key-id"
              value={ascForm.key_id}
              onChange={(e) => setAscForm((prev) => ({ ...prev, key_id: e.target.value }))}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="asc-issuer-id">{t("projectAdmin.prodOps.storeCredentialIssuerId")}</Label>
            <Input
              id="asc-issuer-id"
              value={ascForm.issuer_id}
              onChange={(e) => setAscForm((prev) => ({ ...prev, issuer_id: e.target.value }))}
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="asc-p8">{t("projectAdmin.prodOps.storeCredentialP8")}</Label>
            <Textarea
              id="asc-p8"
              value={ascForm.p8}
              onChange={(e) => setAscForm((prev) => ({ ...prev, p8: e.target.value }))}
              placeholder="-----BEGIN PRIVATE KEY-----"
              className="min-h-[100px] font-mono text-xs"
            />
          </div>
        </div>
      ) : (
        <div className="space-y-1">
          <Label htmlFor="play-sa-json">{t("projectAdmin.prodOps.storeCredentialServiceAccountJson")}</Label>
          <Textarea
            id="play-sa-json"
            value={playForm.service_account_json}
            onChange={(e) => setPlayForm({ service_account_json: e.target.value })}
            placeholder="{ ... }"
            className="min-h-[100px] font-mono text-xs"
          />
        </div>
      )}

      <p className="text-xs text-muted-foreground">{t("projectAdmin.prodOps.storeCredentialReplaceHint")}</p>

      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={busy || !canSave(provider, ascForm, playForm)} onClick={() => void save()}>
          {t("projectAdmin.prodOps.storeCredentialSave")}
        </Button>
        {collapseWhenSaved && editing ? (
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setEditing(false)}>
            {t("settingsPages.integrations.cancel")}
          </Button>
        ) : (
          credential?.configured && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => setConfirmDelete(true)}>
              {t("projectAdmin.prodOps.storeCredentialDelete")}
            </Button>
          )
        )}
      </div>

      {confirm}
    </div>
  );
}
