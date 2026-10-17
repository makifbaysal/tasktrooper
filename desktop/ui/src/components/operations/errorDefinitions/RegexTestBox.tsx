import { useMemo } from "react";
import { testPattern } from "@/components/operations/errorDefinitions/errorDefinitions";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

const CONTEXT = 60;

interface RegexTestBoxProps {
  id: string;
  pattern: string;
  sample: string;
  onSampleChange: (sample: string) => void;
  disabled?: boolean;
}

/** A pasted error message the message pattern is tried on as it is typed,
 * the matched part marked. */
export function RegexTestBox({ id, pattern, sample, onSampleChange, disabled }: RegexTestBoxProps) {
  const { t } = useI18n();
  const r = (key: string, params?: Record<string, string | number>) => t(`operations.errorDefinitions.regexTest.${key}`, params);
  const result = useMemo(() => testPattern(pattern, sample), [pattern, sample]);
  const resultId = `${id}-result`;

  let line;
  switch (result.state) {
    case "empty":
      line = <span className="text-muted-foreground">{r("empty")}</span>;
      break;
    case "noSample":
      line = <span className="text-muted-foreground">{r("noSample")}</span>;
      break;
    case "unsupported":
      line = <span className="text-destructive">{r("unsupported")}</span>;
      break;
    case "invalid":
      line = <span className="text-warning">{r("invalid", { message: result.message })}</span>;
      break;
    case "noMatch":
      line = <span className="font-medium text-muted-foreground">{r("noMatch")}</span>;
      break;
    case "match": {
      const from = Math.max(0, result.start - CONTEXT);
      const to = Math.min(sample.length, result.end + CONTEXT);
      line = (
        <span className="space-y-1">
          <span className="block font-medium text-success">{r("match")}</span>
          <span className="block font-mono break-all text-foreground">
            {from > 0 && "…"}
            {sample.slice(from, result.start)}
            <mark className="rounded-sm bg-success/25 px-0.5 text-foreground">{sample.slice(result.start, result.end) || r("emptyMatch")}</mark>
            {sample.slice(result.end, to)}
            {to < sample.length && "…"}
          </span>
        </span>
      );
      break;
    }
  }

  return (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-caption">
        {r("label")}
      </Label>
      <Textarea
        id={id}
        value={sample}
        onChange={(e) => onSampleChange(e.target.value)}
        placeholder={r("placeholder")}
        rows={3}
        disabled={disabled}
        aria-describedby={resultId}
        className="font-mono text-caption"
      />
      <div id={resultId} aria-live="polite" className="text-caption">
        {line}
      </div>
      <p className="text-micro text-muted-foreground">{r("engineNote")}</p>
    </div>
  );
}
