import { Apple, Bot, Check, ChevronDown, ChevronRight, Copy, ExternalLink, Loader2, X } from "lucide-react";
import { type MouseEvent, type ReactNode, useState } from "react";
import { toast } from "sonner";
import type { MobileStorePlatform, StoreTestGroup } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import {
  isTestBuildActive,
  tailLines,
  testBuildStatusBadgeVariant,
  testBuildStatusLabelKey,
  testGroupName,
} from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";
import { cn } from "@/lib/utils";

const LOG_TAIL_LINES = 40;

export function StorePlatformIcon({ platform, className }: { platform: MobileStorePlatform; className?: string }) {
  const Icon = platform === "ios" ? Apple : Bot;
  return <Icon className={cn("h-4 w-4 shrink-0 text-muted-foreground", className)} aria-hidden="true" />;
}

export function TestBuildStatusBadge({ status }: { status: string }) {
  const { t } = useI18n();
  return (
    <Badge variant={testBuildStatusBadgeVariant(status)} className="gap-1">
      {isTestBuildActive(status) && <Loader2 className="h-3 w-3 animate-spin" aria-hidden />}
      {t(testBuildStatusLabelKey(status))}
    </Badge>
  );
}

export function CopyButton({ value, label }: { value: string; label: string }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      toast.success(t("operations.storeTest.linkCopied"));
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error(t("operations.storeTest.copyFailed"));
    }
  };
  return (
    <Button
      type="button"
      size="icon"
      variant="ghost"
      className="h-7 w-7"
      onClick={() => void copy()}
      aria-label={label}
      title={label}
    >
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
    </Button>
  );
}

/**
 * An outbound link. On the desktop it is handed to the shell before the
 * anchor can navigate — the same reason as the task's PR link in
 * TaskDetailDrawer: the hosted view otherwise decides first and can get stuck.
 */
export function ExternalAnchor({ href, children, className }: { href: string; children: ReactNode; className?: string }) {
  const { t } = useI18n();
  const onClick = (event: MouseEvent<HTMLAnchorElement>) => {
    event.stopPropagation();
    const runner = desktopRunner();
    if (!runner) return;
    event.preventDefault();
    void runner.openExternal(href).then((opened) => {
      if (!opened) toast.error(t("boardArea.components.taskDetail.pullRequestLinkFailed"));
    });
  };
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      onClick={onClick}
      className={cn("inline-flex items-center gap-1 text-xs text-primary underline-offset-2 hover:underline", className)}
    >
      {children}
      <ExternalLink className="h-3 w-3 shrink-0" aria-hidden />
    </a>
  );
}

/** A collapsed log tail with a show/hide toggle; renders nothing without a log. */
export function LogTail({ text, defaultOpen = false }: { text?: string; defaultOpen?: boolean }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(defaultOpen);
  const shown = tailLines(text, LOG_TAIL_LINES);
  if (!shown) return null;
  return (
    <div className="space-y-1">
      <Button
        type="button"
        size="sm"
        variant="ghost"
        className="h-6 px-1.5 text-xs text-muted-foreground"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {open ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
        {open ? t("operations.storeTest.hideLog") : t("operations.storeTest.showLog")}
      </Button>
      {open && (
        <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded border border-border bg-muted/40 p-2 font-mono text-[11px] leading-snug text-muted-foreground">
          {shown}
        </pre>
      )}
    </div>
  );
}

/** The groups a build is open to, by name; `onRemove` adds an x to each removable chip. */
export function TestBuildGroupChips({
  ids,
  groups,
  onRemove,
  disabled,
}: {
  ids: string[];
  groups: StoreTestGroup[] | null;
  onRemove?: (groupId: string) => void;
  disabled?: boolean;
}) {
  const { t } = useI18n();
  if (ids.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("operations.storeTest.notOpen")}</p>;
  }
  return (
    <div className="flex flex-wrap items-center gap-1">
      {ids.map((id) => {
        const name = testGroupName(id, groups);
        const removable = onRemove && !groups?.find((g) => g.id === id)?.all_builds;
        return (
          <Badge key={id} variant="outline" className="gap-1 py-0 pr-1 font-normal">
            <span className="max-w-40 truncate">{name}</span>
            {removable && (
              <Button
                type="button"
                size="icon"
                variant="ghost"
                className="h-4 w-4 rounded-full p-0 [&_svg]:size-3"
                disabled={disabled}
                onClick={() => onRemove(id)}
                aria-label={t("operations.storeTest.removeFromGroup", { group: name })}
                title={t("operations.storeTest.removeFromGroup", { group: name })}
              >
                <X aria-hidden />
              </Button>
            )}
          </Badge>
        );
      })}
    </div>
  );
}

interface ActionSurfaceProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Inline renders the form as an expansion in place instead of a dialog. The
   * task drawer is already a modal Dialog, and HumanUatDecision keeps its
   * forms out of nested modals (see its note on Radix's aria-hide/inert).
   */
  inline?: boolean;
  title: string;
  description?: string;
  children: ReactNode;
  footer: ReactNode;
}

/** One form, shown either as a FormDialog or inline in the page that asked for it. */
export function ActionSurface({ open, onOpenChange, inline = false, title, description, children, footer }: ActionSurfaceProps) {
  if (!inline) {
    return (
      <FormDialog open={open} onOpenChange={onOpenChange} title={title} description={description} footer={footer}>
        {children}
      </FormDialog>
    );
  }
  if (!open) return null;
  return (
    <section className="space-y-3 rounded-lg border border-border bg-background p-3" aria-label={title}>
      <div className="space-y-1">
        <p className="text-sm font-medium">{title}</p>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      {children}
      <div className="flex flex-wrap justify-end gap-2">{footer}</div>
    </section>
  );
}
