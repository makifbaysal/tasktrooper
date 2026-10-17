import { Link } from "react-router-dom";
import type { CloudAccount, DataSourceField, StoreCredentialView } from "@/api";
import {
  cloudAccountsFor,
  fieldErrorText,
  fieldHelp,
  fieldLabel,
  fieldPlaceholder,
  isDestinationField,
  isMultilineField,
  isSecretField,
  optionLabel,
  storeCredentialProvider,
  storeCredentialSaved,
  type HeaderRow,
} from "@/components/operations/sources/dataSourceSchema";
import { HeadersEditor } from "@/components/operations/sources/HeadersEditor";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

interface DataSourceFieldInputProps {
  kind: string;
  field: DataSourceField;
  value: string;
  onChange: (value: string) => void;
  headerRows?: HeaderRow[];
  onHeadersChange?: (rows: HeaderRow[]) => void;
  /** The server holds a value for this secret. */
  stored?: boolean;
  removed?: boolean;
  onRemovedChange?: (removed: boolean) => void;
  /** The address moved, so a stored secret cannot be kept by leaving it empty. */
  mustResupply?: boolean;
  /** A field error code (required, invalid, …). */
  error?: string;
  disabled?: boolean;
  /** Every connected cloud account; a `cloud_account` field lists its provider's. */
  cloudAccounts?: CloudAccount[];
  /** The store credential vault; a `store_credential` field offers its provider's saved one. Null while unknown. */
  storeCredentials?: StoreCredentialView[] | null;
  /** The source holds secrets, so a `destination` field says that moving it asks for them again. */
  secretsStored?: boolean;
}

export const INTEGRATIONS_PATH = "/settings/integrations";

function accountDetail(account: CloudAccount): string {
  const meta = account.meta ?? {};
  return meta.project_id || meta.account_id || meta.client_email || meta.region || "";
}

/** One field of a kind's schema, drawn from its type alone — no per-kind UI. */
export function DataSourceFieldInput({
  kind,
  field,
  value,
  onChange,
  headerRows = [],
  onHeadersChange,
  stored = false,
  removed = false,
  onRemovedChange,
  mustResupply = false,
  error,
  disabled = false,
  cloudAccounts = [],
  storeCredentials = null,
  secretsStored = false,
}: DataSourceFieldInputProps) {
  const { t } = useI18n();
  const f = (key: string, params?: Record<string, string | number>) => t(`operations.sources.form.${key}`, params);
  const id = `ds-field-${field.key}`;
  const label = fieldLabel(t, kind, field);
  const help = fieldHelp(t, kind, field);
  const secret = isSecretField(field);
  const headers = field.type === "headers";
  const ids = {
    label: `${id}-label`,
    help: `${id}-help`,
    error: `${id}-error`,
    stored: `${id}-stored`,
    destination: `${id}-destination`,
  };
  const destinationHint = secretsStored && isDestinationField(field);
  const describedBy =
    [help && ids.help, stored && secret && ids.stored, destinationHint && ids.destination, error && ids.error].filter(Boolean).join(" ") ||
    undefined;
  const invalid = error ? true : undefined;

  let control;
  const accounts = field.type === "cloud_account" ? cloudAccountsFor(cloudAccounts, field) : [];
  const providerName = field.provider ? t(`cloud.providers.${field.provider}`) : t("operations.sources.form.cloudAccountAny");
  const storeProvider = field.type === "store_credential" ? storeCredentialProvider(field) : undefined;
  const storeName = storeProvider ? t(`operations.telemetry.sources.storeCredentials.${storeProvider}`) : "";
  const storeSaved = storeProvider ? storeCredentialSaved(storeCredentials, storeProvider) : undefined;
  if (field.type === "store_credential" && storeSaved === false) {
    control = (
      <div id={id} className="rounded-md border border-dashed border-border px-3 py-2.5 text-caption" aria-describedby={describedBy}>
        <p className="text-muted-foreground">{t("operations.telemetry.sources.noStoreCredential", { credential: storeName })}</p>
        <Link to={INTEGRATIONS_PATH} className="font-medium text-info hover:underline">
          {t("operations.telemetry.sources.saveStoreCredential")}
        </Link>
      </div>
    );
  } else if (field.type === "store_credential") {
    control = (
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger id={id} aria-invalid={invalid} aria-describedby={describedBy}>
          <SelectValue placeholder={t("operations.telemetry.sources.storeCredentialPlaceholder", { credential: storeName })} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={storeProvider ?? ""}>
            {storeName}
            <span className="ml-2 text-muted-foreground">{t("operations.telemetry.sources.storeCredentialWorkspace")}</span>
          </SelectItem>
        </SelectContent>
      </Select>
    );
  } else if (field.type === "cloud_account" && accounts.length === 0) {
    control = (
      <div id={id} className="rounded-md border border-dashed border-border px-3 py-2.5 text-caption" aria-describedby={describedBy}>
        <p className="text-muted-foreground">{f("noCloudAccount", { provider: providerName })}</p>
        <Link to={INTEGRATIONS_PATH} className="font-medium text-info hover:underline">
          {f("connectCloudAccount")}
        </Link>
      </div>
    );
  } else if (field.type === "cloud_account") {
    control = (
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger id={id} aria-invalid={invalid} aria-describedby={describedBy}>
          <SelectValue placeholder={f("cloudAccountPlaceholder", { provider: providerName })} />
        </SelectTrigger>
        <SelectContent>
          {accounts.map((account) => {
            const detail = accountDetail(account);
            return (
              <SelectItem key={account.id} value={account.id}>
                {account.label || account.id}
                {detail && detail !== account.label && <span className="ml-2 font-mono text-muted-foreground">{detail}</span>}
              </SelectItem>
            );
          })}
        </SelectContent>
      </Select>
    );
  } else if (headers) {
    control = (
      <HeadersEditor
        rows={headerRows}
        onChange={(rows) => onHeadersChange?.(rows)}
        placeholder={field.placeholder}
        disabled={disabled || removed}
        invalid={Boolean(error)}
        labelledBy={ids.label}
        describedBy={describedBy}
      />
    );
  } else if (field.type === "select") {
    control = (
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger id={id} aria-invalid={invalid} aria-describedby={describedBy}>
          <SelectValue placeholder={f("selectPlaceholder")} />
        </SelectTrigger>
        <SelectContent>
          {(field.options ?? []).map((option) => (
            <SelectItem key={option} value={option}>
              {optionLabel(t, kind, field, option)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    );
  } else if (isMultilineField(field)) {
    control = (
      <Textarea
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={secret && stored && !mustResupply ? f("secretSavedPlaceholder") : fieldPlaceholder(t, kind, field)}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        autoComplete="off"
        spellCheck={false}
        disabled={disabled || removed}
        rows={4}
        className="font-mono text-micro"
      />
    );
  } else {
    control = (
      <Input
        id={id}
        type={secret ? "password" : field.type === "url" ? "url" : "text"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={secret && stored && !mustResupply ? f("secretSavedPlaceholder") : fieldPlaceholder(t, kind, field)}
        aria-invalid={invalid}
        aria-describedby={describedBy}
        autoComplete={secret ? "new-password" : "off"}
        spellCheck={false}
        disabled={disabled || removed}
        className={field.type === "url" || secret ? "font-mono text-caption" : undefined}
      />
    );
  }

  return (
    <div className="space-y-1.5" data-ds-field={field.key}>
      <Label
        id={ids.label}
        htmlFor={
          headers || (field.type === "cloud_account" && accounts.length === 0) || (field.type === "store_credential" && storeSaved === false)
            ? undefined
            : id
        }
      >
        {label}
        {!field.required && <span className="ml-1 font-normal text-muted-foreground">({f("optional")})</span>}
      </Label>
      {control}
      {secret && stored && (
        <div id={ids.stored} className="flex flex-wrap items-center gap-x-2 text-caption text-muted-foreground">
          <span>
            {removed
              ? f("secretRemoving")
              : mustResupply
                ? f("secretResupply")
                : headers
                  ? f("headersSaved")
                  : f("secretSaved")}
          </span>
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto p-0 text-caption"
            onClick={() => onRemovedChange?.(!removed)}
            disabled={disabled}
            aria-label={removed ? f("secretKeepLabel", { field: label }) : f("secretRemoveLabel", { field: label })}
          >
            {removed ? f("secretKeep") : f("secretRemove")}
          </Button>
        </div>
      )}
      {destinationHint && (
        <p id={ids.destination} className="text-caption text-muted-foreground">
          {f("destinationHint")}
        </p>
      )}
      {help && (
        <p id={ids.help} className="text-caption text-muted-foreground">
          {help}
        </p>
      )}
      {error && (
        <p id={ids.error} className="text-caption text-destructive">
          {fieldErrorText(t, field.key, error)}
        </p>
      )}
    </div>
  );
}
