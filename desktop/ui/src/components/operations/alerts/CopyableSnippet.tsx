import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { copyText } from "@/lib/clipboard";
import { cn } from "@/lib/utils";

interface CopyableSnippetProps {
  value: string;
  /** The copy button's accessible name. */
  copyLabel: string;
  /** Names the block for a screen reader. */
  label?: string;
  /** Wrap long lines (a URL) instead of scrolling them (code). */
  wrap?: boolean;
  className?: string;
}

/** A read-only mono block with a copy button in its corner. */
export function CopyableSnippet({ value, copyLabel, label, wrap = false, className }: CopyableSnippetProps) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const copy = async () => {
    if (await copyText(value)) {
      setCopied(true);
      toast.success(t("operations.alertConnections.reveal.copied"));
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error(t("operations.alertConnections.reveal.copyFailed"));
    }
  };

  return (
    <div className={cn("relative", className)}>
      <pre
        aria-label={label}
        className={cn(
          "max-h-56 overflow-auto rounded-md border border-border bg-muted/40 py-2.5 pl-3 pr-11 font-mono text-micro leading-relaxed",
          wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre",
        )}
      >
        {value}
      </pre>
      <Button
        type="button"
        size="icon"
        variant="ghost"
        className="absolute right-1.5 top-1.5 h-7 w-7"
        onClick={() => void copy()}
        aria-label={copyLabel}
        title={copyLabel}
      >
        {copied ? <Check className="h-3.5 w-3.5" aria-hidden /> : <Copy className="h-3.5 w-3.5" aria-hidden />}
      </Button>
    </div>
  );
}
