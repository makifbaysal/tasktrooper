import { ExternalLink, MessageSquarePlus } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useI18n } from "@/hooks/useI18n";
import { desktopRunner } from "@/lib/desktop-bridge";
import { feedbackIssueUrl, type FeedbackKind } from "@/lib/feedback";

const KINDS: FeedbackKind[] = ["bug", "feature"];

/**
 * "Send feedback": a short form that opens a pre-filled GitHub issue in the
 * real browser. Nothing is posted from here — the user reviews it and submits
 * it on GitHub with their own account.
 */
export function FeedbackButton() {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [kind, setKind] = useState<FeedbackKind>("bug");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [includeEnvironment, setIncludeEnvironment] = useState(true);
  const [environment, setEnvironment] = useState<{ version: string; platform: string } | null>(null);

  useEffect(() => {
    if (!open || environment) return;
    void window.__tasktrooperDesktop?.info?.().then(
      (info) => setEnvironment({ version: info.version, platform: info.platform }),
      () => undefined,
    );
  }, [open, environment]);

  const reset = () => {
    setKind("bug");
    setTitle("");
    setDescription("");
    setIncludeEnvironment(true);
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!title.trim()) return;
    const url = feedbackIssueUrl({
      kind,
      title,
      description,
      ...(includeEnvironment && environment ? { environment } : {}),
    });
    const runner = desktopRunner();
    // In the shell a `window.open` would ask the hosted view to navigate; the
    // shell opens the real browser instead. A `noopener` window.open always
    // returns null, so there is nothing to check in a browser.
    let opened = true;
    if (runner) opened = await runner.openExternal(url).catch(() => false);
    else window.open(url, "_blank", "noopener,noreferrer");
    if (!opened) {
      toast.error(t("frame.layout.header.feedback.openFailed"));
      return;
    }
    toast.success(t("frame.layout.header.feedback.opened"));
    setOpen(false);
    reset();
  };

  return (
    <>
      <Button
        variant="ghost"
        size="icon"
        onClick={() => setOpen(true)}
        title={t("frame.layout.header.feedback.button")}
        aria-label={t("frame.layout.header.feedback.button")}
      >
        <MessageSquarePlus className="h-4 w-4" />
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          <form onSubmit={(e) => void submit(e)} className="space-y-4">
            <DialogHeader>
              <DialogTitle>{t("frame.layout.header.feedback.title")}</DialogTitle>
              <DialogDescription>{t("frame.layout.header.feedback.description")}</DialogDescription>
            </DialogHeader>

            <div className="space-y-2">
              <Label htmlFor="feedback-kind">{t("frame.layout.header.feedback.kindLabel")}</Label>
              <Select value={kind} onValueChange={(v) => setKind(v as FeedbackKind)}>
                <SelectTrigger id="feedback-kind">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KINDS.map((k) => (
                    <SelectItem key={k} value={k}>
                      {t(`frame.layout.header.feedback.kinds.${k}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="feedback-title">{t("frame.layout.header.feedback.titleLabel")}</Label>
              <Input
                id="feedback-title"
                value={title}
                maxLength={200}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={t("frame.layout.header.feedback.titlePlaceholder")}
                autoFocus
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="feedback-description">{t("frame.layout.header.feedback.descriptionLabel")}</Label>
              <Textarea
                id="feedback-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t(`frame.layout.header.feedback.descriptionPlaceholder.${kind}`)}
                rows={6}
              />
            </div>

            {environment ? (
              <div className="flex items-center gap-2">
                <Checkbox
                  id="feedback-environment"
                  checked={includeEnvironment}
                  onCheckedChange={(checked) => setIncludeEnvironment(checked === true)}
                />
                <Label htmlFor="feedback-environment" className="font-normal text-muted-foreground">
                  {t("frame.layout.header.feedback.includeEnvironment", {
                    version: environment.version,
                    platform: environment.platform,
                  })}
                </Label>
              </div>
            ) : null}

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={!title.trim()}>
                <ExternalLink className="mr-2 h-4 w-4" />
                {t("frame.layout.header.feedback.submit")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
