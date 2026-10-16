import { CalendarRange, Check, ChevronDown, Clock } from "lucide-react";
import { useState } from "react";
import { FormDialog } from "@/components/admin/FormDialog";
import { OPS_PRESETS, useOpsContext, type OpsTimeRange } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { intlLocale } from "@/lib/languages";

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

export function toLocalInput(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fromLocalInput(value: string): number {
  const ms = new Date(value).getTime();
  return Number.isFinite(ms) ? ms : NaN;
}

export function useRangeLabel(): (range: OpsTimeRange) => string {
  const { t, lang } = useI18n();
  return (range) => {
    if (range.preset !== "custom") return t(`operations.shell.time.since.${range.preset}`);
    const fmt = new Intl.DateTimeFormat(intlLocale(lang), { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
    return t("operations.shell.time.between", { from: fmt.format(range.from), to: fmt.format(range.to) });
  };
}

/** New Relic's "Since 30 minutes ago" picker: the presets in a menu, a custom
 * window in a dialog. Writes the shell's URL state. */
export function OpsTimePicker() {
  const { t } = useI18n();
  const { range, setPreset, setCustom } = useOpsContext();
  const label = useRangeLabel();
  const [customOpen, setCustomOpen] = useState(false);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [invalid, setInvalid] = useState(false);

  const openCustom = () => {
    setFrom(toLocalInput(range.from));
    setTo(toLocalInput(range.to));
    setInvalid(false);
    setCustomOpen(true);
  };

  const apply = () => {
    const a = fromLocalInput(from);
    const b = fromLocalInput(to);
    if (!Number.isFinite(a) || !Number.isFinite(b) || a >= b) {
      setInvalid(true);
      return;
    }
    setCustom(a, b);
    setCustomOpen(false);
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="outline" size="sm" className="max-w-full gap-1.5" aria-label={`${t("operations.shell.time.label")}: ${label(range)}`}>
            <Clock aria-hidden />
            <span className="truncate">{label(range)}</span>
            <ChevronDown className="opacity-60" aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="min-w-[14rem]">
          <DropdownMenuLabel>{t("operations.shell.time.label")}</DropdownMenuLabel>
          {OPS_PRESETS.map((p) => (
            <DropdownMenuItem key={p} onSelect={() => setPreset(p)} aria-current={range.preset === p ? "true" : undefined}>
              <span className="flex w-4 shrink-0 justify-center">{range.preset === p && <Check className="h-3.5 w-3.5" aria-hidden />}</span>
              <span className="ml-2">{t(`operations.shell.time.presets.${p}`)}</span>
            </DropdownMenuItem>
          ))}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={openCustom} aria-current={range.preset === "custom" ? "true" : undefined}>
            <span className="flex w-4 shrink-0 justify-center">
              {range.preset === "custom" ? <Check className="h-3.5 w-3.5" aria-hidden /> : <CalendarRange className="h-3.5 w-3.5" aria-hidden />}
            </span>
            <span className="ml-2">{t("operations.shell.time.custom")}</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <FormDialog
        open={customOpen}
        onOpenChange={setCustomOpen}
        title={t("operations.shell.time.customTitle")}
        description={t("operations.shell.time.customDescription")}
        className="sm:max-w-md"
        footer={
          <>
            <Button variant="outline" onClick={() => setCustomOpen(false)}>
              {t("operations.shell.time.cancel")}
            </Button>
            <Button onClick={apply}>{t("operations.shell.time.apply")}</Button>
          </>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="ops-range-from">{t("operations.shell.time.from")}</Label>
            <Input
              id="ops-range-from"
              type="datetime-local"
              value={from}
              aria-invalid={invalid || undefined}
              onChange={(e) => {
                setFrom(e.target.value);
                setInvalid(false);
              }}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="ops-range-to">{t("operations.shell.time.to")}</Label>
            <Input
              id="ops-range-to"
              type="datetime-local"
              value={to}
              aria-invalid={invalid || undefined}
              onChange={(e) => {
                setTo(e.target.value);
                setInvalid(false);
              }}
            />
          </div>
        </div>
        {invalid && (
          <p role="alert" className="text-caption text-destructive">
            {t("operations.shell.time.invalid")}
          </p>
        )}
      </FormDialog>
    </>
  );
}
