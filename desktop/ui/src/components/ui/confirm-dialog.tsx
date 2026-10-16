import { useEffect, useState, type ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { useI18n } from "@/hooks/useI18n";

interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "destructive";
  loading?: boolean;
  /**
   * Gets what the person typed into the phrase field ("" without a phrase),
   * so a caller sends that and never the phrase it already knows. Returning
   * `false` keeps the dialog open, typed text and fields included — for an
   * action the server refused.
   */
  onConfirm: (typed: string) => void | boolean | Promise<void | boolean>;
  /**
   * When set, the user must type this exact string before confirming
   * (surrounding spaces ignored, as the server ignores them). Absent, the
   * dialog behaves exactly as before — this is the only brake on production
   * actions, since there is no role system to lean on.
   */
  confirmPhrase?: string;
  /** Extra fields between the description and the phrase (a note, say). */
  children?: ReactNode;
  /** Holds the confirm button back until the caller's own fields are valid. */
  confirmDisabled?: boolean;
  /** Why the last attempt failed, shown above the buttons while the dialog stays open. */
  error?: string | null;
  errorTitle?: string;
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  cancelLabel,
  variant = "destructive",
  loading = false,
  onConfirm,
  confirmPhrase,
  children,
  confirmDisabled = false,
  error,
  errorTitle,
}: ConfirmDialogProps) {
  const { t } = useI18n();
  const [typed, setTyped] = useState("");
  const phraseRequired = Boolean(confirmPhrase);
  const phraseOK = !phraseRequired || typed.trim() === confirmPhrase;

  // Reset between openings so a previously satisfied phrase never carries
  // over into the next, different confirmation.
  useEffect(() => {
    if (!open) setTyped("");
  }, [open]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {children}
        {phraseRequired && (
          <div className="space-y-2">
            <Label htmlFor="confirm-phrase">
              {t("frame.ui.confirm.typeToConfirm", { phrase: confirmPhrase! })}
            </Label>
            <Input
              id="confirm-phrase"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              autoComplete="off"
              placeholder={confirmPhrase}
            />
          </div>
        )}
        {error && (
          <Notice variant="error" title={errorTitle ?? t("common.actionFailed")}>
            <p className="break-words text-caption">{error}</p>
          </Notice>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {cancelLabel ?? t("common.cancel")}
          </Button>
          <Button
            variant={variant}
            disabled={loading || !phraseOK || confirmDisabled}
            onClick={async () => {
              const result = await onConfirm(phraseRequired ? typed : "");
              if (result !== false) onOpenChange(false);
            }}
          >
            {loading ? t("frame.ui.confirm.processing") : confirmLabel ?? t("frame.ui.confirm.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
