import { useState } from "react";
import { INCIDENT_REMEDY_KINDS, type Incident, type IncidentRemedyInput, type IncidentRemedyKind } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";

function isRemedyKind(value: string | undefined): value is IncidentRemedyKind {
  return INCIDENT_REMEDY_KINDS.some((k) => k === value);
}

export function remedyInput(kind: IncidentRemedyKind, summary: string, steps: string, confidence: number): IncidentRemedyInput {
  const lines = steps
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
  return {
    kind,
    summary: summary.trim(),
    confidence: Math.round(confidence),
    ...(lines.length > 0 ? { steps: lines } : {}),
    ...(kind === "rollback" ? { rollback: true } : {}),
  };
}

interface RemedyDialogProps {
  incident: Incident;
  open: boolean;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (remedy: IncidentRemedyInput) => void | Promise<void>;
}

/** A proposal written by hand: it replaces the current one, and the next
 * recurrence's machine triage leaves it alone. */
export function RemedyDialog({ incident, open, busy, onOpenChange, onSave }: RemedyDialogProps) {
  const { t } = useI18n();
  const a = (key: string) => t(`operations.issues.actions.${key}`);
  const [kind, setKind] = useState<IncidentRemedyKind>(isRemedyKind(incident.remedy_kind) ? incident.remedy_kind : "unknown");
  const [summary, setSummary] = useState("");
  const [steps, setSteps] = useState("");
  const [confidence, setConfidence] = useState("50");
  const [touched, setTouched] = useState(false);

  const confidenceValue = Number(confidence);
  const confidenceOK = confidence.trim() !== "" && Number.isFinite(confidenceValue) && confidenceValue >= 0 && confidenceValue <= 100;
  const summaryOK = summary.trim() !== "";
  const valid = confidenceOK && summaryOK;

  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title={a("remedyTitle")}
      description={a("remedyDescription")}
      footer={
        <>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button
            disabled={busy}
            onClick={() => {
              setTouched(true);
              if (valid) void onSave(remedyInput(kind, summary, steps, confidenceValue));
              else document.getElementById(summaryOK ? "remedy-confidence" : "remedy-summary")?.focus();
            }}
          >
            {busy ? a("remedySaving") : a("remedySave")}
          </Button>
        </>
      }
    >
      <div className="space-y-2">
        <Label htmlFor="remedy-kind">{a("remedyKind")}</Label>
        <Select value={kind} onValueChange={(v) => isRemedyKind(v) && setKind(v)}>
          <SelectTrigger id="remedy-kind">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {INCIDENT_REMEDY_KINDS.map((k) => (
              <SelectItem key={k} value={k}>
                {t(`operations.issues.detail.remedy.kinds.${k}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <Label htmlFor="remedy-summary">{a("remedySummary")}</Label>
        <Textarea
          id="remedy-summary"
          value={summary}
          onChange={(e) => setSummary(e.target.value)}
          placeholder={a("remedySummaryPlaceholder")}
          rows={3}
          aria-invalid={touched && !summaryOK}
        />
        {touched && !summaryOK && <p className="text-caption text-destructive">{a("remedySummaryRequired")}</p>}
      </div>
      <div className="space-y-2">
        <Label htmlFor="remedy-steps">{a("remedySteps")}</Label>
        <Textarea id="remedy-steps" value={steps} onChange={(e) => setSteps(e.target.value)} rows={4} />
      </div>
      <div className="space-y-2">
        <Label htmlFor="remedy-confidence">{a("remedyConfidence")}</Label>
        <Input
          id="remedy-confidence"
          type="number"
          min={0}
          max={100}
          inputMode="numeric"
          value={confidence}
          onChange={(e) => setConfidence(e.target.value)}
          aria-invalid={touched && !confidenceOK}
          className="w-28 font-mono"
        />
        {touched && !confidenceOK && <p className="text-caption text-destructive">{a("remedyConfidenceRange")}</p>}
      </div>
    </FormDialog>
  );
}
