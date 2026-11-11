import { Sparkles } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";
import { whatsNewKey, type WhatsNewEntry } from "@/lib/whatsNew/entries";
import { useWhatsNewOnLaunch } from "@/hooks/useWhatsNew";

export const RELEASES_URL = "https://github.com/makifbaysal/tasktrooper/releases/tag";

export function releaseUrl(version: string): string {
  return `${RELEASES_URL}/v${version}`;
}

interface WhatsNewDialogProps {
  entry: WhatsNewEntry | null;
  onClose: () => void;
}

/** A centered pop-up with one release's notes. Escape, the backdrop, the X and "Got it" all close it. */
export function WhatsNewDialog({ entry, onClose }: WhatsNewDialogProps) {
  const { t } = useI18n();
  const key = entry ? whatsNewKey(entry.version) : "";

  const seeAll = () => {
    if (!entry) return;
    const url = releaseUrl(entry.version);
    const runner = desktopRunner();
    if (runner) void runner.openExternal(url).catch(() => undefined);
    else window.open(url, "_blank", "noopener,noreferrer");
  };

  return (
    <Dialog open={entry !== null} onOpenChange={(open) => !open && onClose()}>
      {entry && (
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Sparkles className="h-4 w-4 text-primary" />
              {t("whatsNew.dialogTitle", { version: entry.version })}
            </DialogTitle>
            <DialogDescription>{t(`whatsNew.${key}.title`)}</DialogDescription>
          </DialogHeader>
          <ul className="list-disc space-y-2 pl-5 text-sm">
            {entry.items.map((item) => (
              <li key={item}>{t(`whatsNew.${key}.items.${item}`)}</li>
            ))}
          </ul>
          <DialogFooter className="gap-2 sm:gap-0">
            <Button variant="outline" onClick={seeAll}>
              {t("whatsNew.seeAll")}
            </Button>
            <Button onClick={onClose}>{t("whatsNew.gotIt")}</Button>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}

/** Mounted once in the workspace shell: opens the notes on the first launch of a new version. */
export function WhatsNewOnLaunch() {
  const { entry, dismiss } = useWhatsNewOnLaunch();
  return <WhatsNewDialog entry={entry} onClose={dismiss} />;
}
