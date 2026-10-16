import { Trash2, UserPlus } from "lucide-react";
import { useCallback, useEffect, useId, useRef, useState, type FormEvent } from "react";
import { api, type StoreTester, type StoreTestGroup } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { describeRefusal } from "@/components/operations/releases/mobileReleases";
import { testerStateLabelKey } from "@/components/operations/storeTestBuilds";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";
import { useStoreAction } from "@/hooks/useMobileReleases";

interface GroupTestersDialogProps {
  repositoryId: string;
  /** The TestFlight group whose testers are shown; null closes the dialog. */
  group: StoreTestGroup | null;
  onOpenChange: (open: boolean) => void;
  /** +1 / -1 after an add or remove, so the group's count follows without a store read. */
  onCountChange: (groupId: string, delta: number) => void;
}

function testerName(tester: StoreTester): string {
  return [tester.first_name, tester.last_name].filter(Boolean).join(" ");
}

/** One TestFlight group's testers: the list, add by email, remove. */
export function GroupTestersDialog({ repositoryId, group, onOpenChange, onCountChange }: GroupTestersDialogProps) {
  const { t } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.storeTest.${key}`, params);
  const formId = useId();
  const [testers, setTesters] = useState<StoreTester[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [removal, setRemoval] = useState<StoreTester | null>(null);
  const { busy, run } = useStoreAction();
  const seq = useRef(0);
  const groupId = group?.id;

  const load = useCallback(async () => {
    if (!groupId) return;
    const ticket = ++seq.current;
    try {
      const next = await api.listStoreTestGroupTesters(repositoryId, groupId);
      if (ticket !== seq.current) return;
      setTesters(next);
      setError(null);
    } catch (err) {
      if (ticket !== seq.current) return;
      setError(describeRefusal(err).detail || tStatic("operations.storeTest.testersFailed"));
      setTesters((prev) => prev ?? []);
    }
  }, [groupId, repositoryId]);

  useEffect(() => {
    seq.current += 1;
    setTesters(null);
    setError(null);
    setEmail("");
    setFirstName("");
    setLastName("");
    void load();
  }, [load]);

  const add = async (event: FormEvent) => {
    event.preventDefault();
    const trimmed = email.trim();
    if (!groupId || !trimmed) return;
    const body = { email: trimmed, first_name: firstName.trim(), last_name: lastName.trim() };
    await run("add", () => api.addStoreTestGroupTester(repositoryId, groupId, body), (tester) => {
      seq.current += 1;
      setTesters((prev) => [...(prev ?? []).filter((x) => x.id !== tester.id), tester]);
      onCountChange(groupId, 1);
      setEmail("");
      setFirstName("");
      setLastName("");
      return s("testerAdded");
    });
  };

  const remove = async () => {
    if (!groupId || !removal) return;
    const gone = removal;
    await run("remove", () => api.removeStoreTestGroupTester(repositoryId, groupId, gone.id), () => {
      seq.current += 1;
      setTesters((prev) => (prev ?? []).filter((x) => x.id !== gone.id));
      onCountChange(groupId, -1);
      return s("testerRemoved");
    });
  };

  return (
    <>
      <FormDialog
        open={group !== null}
        onOpenChange={onOpenChange}
        title={s("testersTitle", { group: group?.name ?? "" })}
        description={s("testersDescription")}
        footer={
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {s("close")}
          </Button>
        }
      >
        {error && (
          <Notice variant="warning" title={s("testersFailed")}>
            {error}
          </Notice>
        )}
        {testers === null && <Skeleton className="h-24 w-full" />}
        {testers !== null && testers.length === 0 && !error && (
          <p className="text-caption text-muted-foreground">{s("testersEmpty")}</p>
        )}
        {testers !== null && testers.length > 0 && (
          <ul className="max-h-64 divide-y divide-border overflow-y-auto rounded-lg border border-border">
            {testers.map((tester) => {
              const name = testerName(tester);
              return (
                <li key={tester.id} className="flex items-center gap-2 px-3 py-1.5">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-body">{name || tester.email}</p>
                    {name && <p className="truncate font-mono text-micro text-muted-foreground">{tester.email}</p>}
                  </div>
                  {tester.state && (
                    <Badge variant="outline" className="py-0 font-normal" title={tester.state}>
                      {t(testerStateLabelKey(tester.state))}
                    </Badge>
                  )}
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    disabled={busy !== null}
                    onClick={() => setRemoval(tester)}
                    aria-label={s("removeTester", { email: tester.email })}
                    title={s("removeTester", { email: tester.email })}
                  >
                    <Trash2 className="h-3.5 w-3.5" aria-hidden />
                  </Button>
                </li>
              );
            })}
          </ul>
        )}

        <form onSubmit={(e) => void add(e)} className="space-y-2 border-t border-border pt-3">
          <div className="space-y-1">
            <Label htmlFor={`${formId}-email`}>{s("email")}</Label>
            <Input
              id={`${formId}-email`}
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={busy !== null}
              autoComplete="off"
            />
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor={`${formId}-first`}>{s("firstName")}</Label>
              <Input id={`${formId}-first`} value={firstName} onChange={(e) => setFirstName(e.target.value)} disabled={busy !== null} autoComplete="off" />
            </div>
            <div className="space-y-1">
              <Label htmlFor={`${formId}-last`}>{s("lastName")}</Label>
              <Input id={`${formId}-last`} value={lastName} onChange={(e) => setLastName(e.target.value)} disabled={busy !== null} autoComplete="off" />
            </div>
          </div>
          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={busy !== null || !email.trim()}>
              <UserPlus aria-hidden />
              {s("addTester")}
            </Button>
          </div>
        </form>
      </FormDialog>

      <ConfirmDialog
        open={removal !== null}
        onOpenChange={(open) => !open && setRemoval(null)}
        title={s("removeTesterTitle", { email: removal?.email ?? "" })}
        description={s("removeTesterDescription", { group: group?.name ?? "" })}
        confirmLabel={s("removeConfirm")}
        loading={busy === "remove"}
        onConfirm={remove}
      />
    </>
  );
}
