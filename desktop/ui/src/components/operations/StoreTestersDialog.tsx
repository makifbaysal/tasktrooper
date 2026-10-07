import { Trash2, UserPlus } from "lucide-react";
import { type FormEvent, useCallback, useEffect, useId, useRef, useState } from "react";
import { toast } from "sonner";
import { api, type StoreTester, type StoreTestGroup } from "@/api";
import { FormDialog } from "@/components/admin/FormDialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { tStatic, useI18n } from "@/hooks/useI18n";

interface StoreTestersDialogProps {
  repositoryId: string;
  /** The TestFlight group whose testers are shown; null closes the dialog. */
  group: StoreTestGroup | null;
  onOpenChange: (open: boolean) => void;
  /** +1 / -1 after an add or remove, so the group row's count follows without a store read. */
  onCountChange: (groupId: string, delta: number) => void;
}

function testerName(tester: StoreTester): string {
  return [tester.first_name, tester.last_name].filter(Boolean).join(" ");
}

export function StoreTestersDialog({ repositoryId, group, onOpenChange, onCountChange }: StoreTestersDialogProps) {
  const { t } = useI18n();
  const formId = useId();
  const [testers, setTesters] = useState<StoreTester[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [firstName, setFirstName] = useState("");
  const [lastName, setLastName] = useState("");
  const [adding, setAdding] = useState(false);
  const [removal, setRemoval] = useState<StoreTester | null>(null);
  const [removing, setRemoving] = useState(false);
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
    } catch (e) {
      if (ticket !== seq.current) return;
      setError(e instanceof Error ? e.message : tStatic("operations.storeTest.testersFailed"));
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
    setAdding(true);
    try {
      const tester = await api.addStoreTestGroupTester(repositoryId, groupId, {
        email: trimmed,
        first_name: firstName.trim(),
        last_name: lastName.trim(),
      });
      seq.current += 1;
      setTesters((prev) => [...(prev ?? []).filter((x) => x.id !== tester.id), tester]);
      onCountChange(groupId, 1);
      setEmail("");
      setFirstName("");
      setLastName("");
      toast.success(t("operations.storeTest.testerAdded"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setAdding(false);
    }
  };

  const remove = async () => {
    if (!groupId || !removal) return;
    setRemoving(true);
    try {
      await api.removeStoreTestGroupTester(repositoryId, groupId, removal.id);
      seq.current += 1;
      setTesters((prev) => (prev ?? []).filter((x) => x.id !== removal.id));
      onCountChange(groupId, -1);
      toast.success(t("operations.storeTest.testerRemoved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("common.actionFailed"));
    } finally {
      setRemoving(false);
    }
  };

  return (
    <>
      <FormDialog
        open={group !== null}
        onOpenChange={onOpenChange}
        title={t("operations.storeTest.testersTitle", { group: group?.name ?? "" })}
        description={t("operations.storeTest.testersDescription")}
        footer={
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("operations.storeTest.close")}
          </Button>
        }
      >
        {error && (
          <Notice variant="warning" title={t("operations.storeTest.testersFailed")}>
            {error}
          </Notice>
        )}
        {testers === null && <Skeleton className="h-24 w-full" />}
        {testers !== null && testers.length === 0 && !error && (
          <p className="text-sm text-muted-foreground">{t("operations.storeTest.testersEmpty")}</p>
        )}
        {testers !== null && testers.length > 0 && (
          <ul className="divide-y divide-border rounded-lg border border-border">
            {testers.map((tester) => {
              const name = testerName(tester);
              return (
                <li key={tester.id} className="flex items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm">{name || tester.email}</p>
                    {name && <p className="truncate text-xs text-muted-foreground">{tester.email}</p>}
                  </div>
                  {tester.state && (
                    <Badge variant="outline" className="py-0 font-normal">
                      {tester.state}
                    </Badge>
                  )}
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    disabled={removing}
                    onClick={() => setRemoval(tester)}
                    aria-label={t("operations.storeTest.removeTester", { email: tester.email })}
                    title={t("operations.storeTest.removeTester", { email: tester.email })}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </li>
              );
            })}
          </ul>
        )}

        <form onSubmit={(e) => void add(e)} className="space-y-2 rounded-lg border border-border p-3">
          <div className="space-y-1">
            <Label htmlFor={`${formId}-email`}>{t("operations.storeTest.email")}</Label>
            <Input
              id={`${formId}-email`}
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={adding}
              autoComplete="off"
            />
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            <div className="space-y-1">
              <Label htmlFor={`${formId}-first`}>{t("operations.storeTest.firstName")}</Label>
              <Input
                id={`${formId}-first`}
                value={firstName}
                onChange={(e) => setFirstName(e.target.value)}
                disabled={adding}
                autoComplete="off"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor={`${formId}-last`}>{t("operations.storeTest.lastName")}</Label>
              <Input
                id={`${formId}-last`}
                value={lastName}
                onChange={(e) => setLastName(e.target.value)}
                disabled={adding}
                autoComplete="off"
              />
            </div>
          </div>
          <div className="flex justify-end">
            <Button type="submit" size="sm" disabled={adding || !email.trim()}>
              <UserPlus className="h-3.5 w-3.5" />
              {t("operations.storeTest.addTester")}
            </Button>
          </div>
        </form>
      </FormDialog>

      <ConfirmDialog
        open={removal !== null}
        onOpenChange={(open) => !open && setRemoval(null)}
        title={t("operations.storeTest.removeTesterTitle", { email: removal?.email ?? "" })}
        description={t("operations.storeTest.removeTesterDescription", { group: group?.name ?? "" })}
        confirmLabel={t("operations.storeTest.removeConfirm")}
        loading={removing}
        onConfirm={remove}
      />
    </>
  );
}
