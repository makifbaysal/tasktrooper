import type { DataSourceField } from "@/api";
import {
  fieldErrorText,
  fieldHelp,
  fieldLabel,
  isSecretField,
  optionLabel,
  type HeaderRow,
} from "@/components/operations/sources/dataSourceSchema";
import { HeadersEditor } from "@/components/operations/sources/HeadersEditor";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
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
  };
  const describedBy =
    [help && ids.help, stored && secret && ids.stored, error && ids.error].filter(Boolean).join(" ") || undefined;
  const invalid = error ? true : undefined;

  let control;
  if (headers) {
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
  } else {
    control = (
      <Input
        id={id}
        type={secret ? "password" : field.type === "url" ? "url" : "text"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={secret && stored && !mustResupply ? f("secretSavedPlaceholder") : field.placeholder}
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
      <Label id={ids.label} htmlFor={headers ? undefined : id}>
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
