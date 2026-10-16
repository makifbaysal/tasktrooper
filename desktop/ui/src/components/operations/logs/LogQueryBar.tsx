import { Radio, Search } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { LOG_SEVERITIES, type LogSeverity } from "@/api";
import { isLogSeverity } from "@/components/operations/logs/severity";
import { EntityMultiSelect } from "@/components/operations/query/EntityMultiSelect";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useI18n } from "@/hooks/useI18n";

const ANY = "any";

interface LogQueryBarProps {
  text: string;
  onText: (text: string) => void;
  minSeverity?: LogSeverity;
  onMinSeverity: (severity: LogSeverity | undefined) => void;
  /** Left out on one entity's page. */
  entityFilter?: {
    entities: OpsEntity[];
    selected: string[];
    onChange: (ids: string[]) => void;
    limit: number;
    required: boolean;
  };
  live: boolean;
  onLive: (on: boolean) => void;
  liveDisabled?: boolean;
  /** Why the live tail cannot run, said beside the switch. */
  liveHint?: string;
}

/** The Logs explorer's query line: entities, a text search run on Enter, the
 * lowest severity shown, and the live-tail switch. */
export function LogQueryBar({
  text,
  onText,
  minSeverity,
  onMinSeverity,
  entityFilter,
  live,
  onLive,
  liveDisabled = false,
  liveHint,
}: LogQueryBarProps) {
  const { t } = useI18n();
  const l = (key: string, params?: Record<string, string | number>) => t(`operations.logsExplorer.${key}`, params);
  const [draft, setDraft] = useState(text);
  const liveId = useId();
  const liveHintId = useId();

  useEffect(() => setDraft(text), [text]);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <form
        role="search"
        aria-label={l("queryLabel")}
        className="flex min-w-0 flex-1 flex-wrap items-center gap-2"
        onSubmit={(ev) => {
          ev.preventDefault();
          onText(draft.trim());
        }}
      >
        {entityFilter && (
          <EntityMultiSelect
            entities={entityFilter.entities}
            selected={entityFilter.selected}
            onChange={entityFilter.onChange}
            limit={entityFilter.limit}
            required={entityFilter.required}
          />
        )}
        <div className="relative min-w-48 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input
            type="search"
            value={draft}
            onChange={(ev) => setDraft(ev.target.value)}
            placeholder={l("searchPlaceholder")}
            aria-label={l("searchLabel")}
            className="h-8 pl-8 font-mono text-caption"
          />
        </div>
        <Select value={minSeverity ?? ANY} onValueChange={(v) => onMinSeverity(isLogSeverity(v) ? v : undefined)}>
          <SelectTrigger className="h-8 w-40 text-caption" aria-label={l("minSeverity")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ANY}>{t("operations.opsQuery.severity.any")}</SelectItem>
            {LOG_SEVERITIES.map((s) => (
              <SelectItem key={s} value={s}>
                {l("atLeast", { severity: t(`operations.opsQuery.severity.${s}`) })}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="submit" size="sm" className="h-8">
          {l("run")}
        </Button>
      </form>
      <div className="flex items-center gap-2">
        <Switch
          id={liveId}
          checked={live}
          disabled={liveDisabled}
          onCheckedChange={onLive}
          aria-describedby={liveHint ? liveHintId : undefined}
        />
        <Label htmlFor={liveId} className="inline-flex cursor-pointer items-center gap-1.5 text-caption font-normal text-muted-foreground">
          <Radio className={live ? "h-3.5 w-3.5 text-success" : "h-3.5 w-3.5"} aria-hidden />
          {l("live")}
        </Label>
        {liveHint && (
          <span id={liveHintId} className="text-micro text-muted-foreground">
            {liveHint}
          </span>
        )}
      </div>
    </div>
  );
}
