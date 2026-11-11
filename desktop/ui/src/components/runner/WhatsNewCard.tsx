import { Sparkles } from "lucide-react";
import { useState } from "react";
import { WhatsNewDialog } from "@/components/layout/WhatsNewDialog";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/hooks/useI18n";
import { latestWhatsNew } from "@/lib/whatsNew/entries";

/** Settings → General: reopen the latest release notes after dismissing the pop-up. */
export function WhatsNewCard() {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const latest = latestWhatsNew();
  if (!latest) return null;

  return (
    <Card className="w-full space-y-4 p-6">
      <div className="space-y-1">
        <Label className="flex items-center gap-2">
          <Sparkles className="h-4 w-4" />
          {t("whatsNew.card.title")}
        </Label>
        <p className="text-sm text-muted-foreground">{t("whatsNew.card.help")}</p>
      </div>
      <Button variant="outline" onClick={() => setOpen(true)}>
        {t("whatsNew.card.open")}
      </Button>
      <WhatsNewDialog entry={open ? latest : null} onClose={() => setOpen(false)} />
    </Card>
  );
}
