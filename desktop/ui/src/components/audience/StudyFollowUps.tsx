import { Loader2, Send } from "lucide-react";
import { useState, type FormEvent } from "react";
import { toast } from "sonner";
import { ApiError, api, type StudyFollowUp, AUDIENCE_NO_RUNTIME } from "@/api";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { useI18n } from "@/hooks/useI18n";

/**
 * Questions put to the people after the report: one to everyone (one model call
 * per person, so it asks first) and a question about the findings themselves,
 * answered from the report and what each person said.
 */
export function StudyFollowUps({
  studyId,
  followUps,
  people,
  onAsked,
  onNoRuntime,
}: {
  studyId: string;
  followUps: StudyFollowUp[];
  people: number;
  /** Fires when a question to everyone started, so the page polls for the replies. */
  onAsked: () => void;
  onNoRuntime: () => void;
}) {
  const { t } = useI18n();
  const f = (key: string, params?: Record<string, string | number>) => t(`audienceHub.followUps.${key}`, params);
  const [everyone, setEveryone] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [about, setAbout] = useState("");
  const [asking, setAsking] = useState(false);
  const [answer, setAnswer] = useState<{ question: string; text: string } | null>(null);

  const fail = (e: unknown, fallback: string) => {
    if (e instanceof ApiError && e.type === AUDIENCE_NO_RUNTIME) onNoRuntime();
    toast.error(e instanceof Error ? e.message : fallback);
  };

  const askEveryone = async () => {
    try {
      await api.askAudienceEveryone(studyId, everyone.trim());
      toast.success(f("started"));
      setEveryone("");
      onAsked();
    } catch (e) {
      fail(e, f("failed"));
      return false;
    }
  };

  const askAbout = async (e: FormEvent) => {
    e.preventDefault();
    const q = about.trim();
    if (!q || asking) return;
    setAsking(true);
    try {
      const out = await api.askAudienceResearch(studyId, q);
      setAnswer({ question: q, text: out.answer });
      setAbout("");
    } catch (err) {
      fail(err, f("failed"));
    } finally {
      setAsking(false);
    }
  };

  return (
    <div className="space-y-4">
      <Card className="space-y-3 p-5">
        <div>
          <h3 className="text-body font-semibold">{f("aboutTitle")}</h3>
          <p className="text-caption text-muted-foreground">{f("aboutHelp")}</p>
        </div>
        <form onSubmit={(e) => void askAbout(e)} className="flex gap-2">
          <Input value={about} onChange={(e) => setAbout(e.target.value)} placeholder={f("aboutPlaceholder")} aria-label={f("aboutTitle")} />
          <Button type="submit" disabled={asking || about.trim() === ""}>
            {asking ? <Loader2 className="animate-spin" /> : <Send />}
            {f("ask")}
          </Button>
        </form>
        {answer && (
          <div className="rounded-md bg-surface-sunken p-3 text-body">
            <p className="text-caption font-medium text-muted-foreground">{answer.question}</p>
            <p className="mt-1 whitespace-pre-line">{answer.text}</p>
          </div>
        )}
      </Card>

      <Card className="space-y-3 p-5">
        <div>
          <h3 className="text-body font-semibold">{f("everyoneTitle")}</h3>
          <p className="text-caption text-muted-foreground">{f("everyoneHelp", { people })}</p>
        </div>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (everyone.trim()) setConfirm(true);
          }}
          className="flex gap-2"
        >
          <Input value={everyone} onChange={(e) => setEveryone(e.target.value)} placeholder={f("everyonePlaceholder")} aria-label={f("everyoneTitle")} />
          <Button type="submit" variant="outline" disabled={everyone.trim() === ""}>
            {f("askEveryone")}
          </Button>
        </form>
        {followUps.length === 0 ? (
          <p className="text-caption text-muted-foreground">{f("none")}</p>
        ) : (
          <ul className="space-y-4">
            {followUps.map((g) => (
              <li key={g.group_id} className="space-y-2">
                <p className="font-medium">
                  {g.question}
                  <span className="ml-2 text-caption font-normal text-muted-foreground">{f("replies", { done: g.replies.length, asked: g.asked })}</span>
                </p>
                <ul className="space-y-1.5 border-l-2 border-border pl-3">
                  {g.replies.map((r, i) => (
                    <li key={`${r.persona_id ?? r.persona}-${i}`} className="text-body">
                      <span className="font-medium">{r.persona || f("someone")}: </span>
                      <span className="text-muted-foreground">{r.text}</span>
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={f("confirmTitle")}
        description={f("confirmBody", { people, question: everyone.trim() })}
        confirmLabel={f("askEveryone")}
        onConfirm={askEveryone}
      />
    </div>
  );
}
