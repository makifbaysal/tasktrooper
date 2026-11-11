import { ExternalLink } from "lucide-react";
import type { MouseEvent, ReactNode } from "react";
import { toast } from "sonner";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";

interface ExternalLinkAnchorProps {
  href: string;
  children: ReactNode;
  className?: string;
}

/** A link out of the app: through the desktop shell's bridge when there is one (a plain anchor leaves the shell stuck), else a new tab. */
export function ExternalLinkAnchor({ href, children, className }: ExternalLinkAnchorProps) {
  const { t } = useI18n();
  const onClick = (event: MouseEvent<HTMLAnchorElement>) => {
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
      className={className ?? "inline-flex items-center gap-1 text-primary hover:underline"}
    >
      <ExternalLink className="h-3 w-3 shrink-0" />
      {children}
    </a>
  );
}
