import { Cloud, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import { api, ApiError, type DataSource } from "@/api";
import { DataSourceDetailDrawer } from "@/components/operations/sources/DataSourceDetailDrawer";
import { DataSourceForm } from "@/components/operations/sources/DataSourceForm";
import { DataSourceList } from "@/components/operations/sources/DataSourceList";
import { RecipeCatalog } from "@/components/operations/sources/RecipeCatalog";
import { kindLabel, unusedCloudAccounts } from "@/components/operations/sources/dataSourceSchema";
import { useOpsPageScope } from "@/components/operations/shell/OpsContext";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Notice } from "@/components/ui/notice";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useCloudAccounts, useDataSources, useRecipeCatalog } from "@/hooks/useDataSources";
import { useStoreCredentials } from "@/hooks/useStoreCredentials";
import { useI18n } from "@/hooks/useI18n";

type SourcesTab = "sources" | "recipes";

type FormTarget = { mode: "create"; initial?: { kind: string; values: Record<string, string> } } | { mode: "edit"; id: string };

function isNotFound(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404;
}

export function DataSourcesPage() {
  const { t } = useI18n();
  const s = (key: string, params?: Record<string, string | number>) => t(`operations.sources.${key}`, params);
  const [params, setParams] = useSearchParams();
  const tab: SourcesTab = params.get("tab") === "recipes" ? "recipes" : "sources";
  const openId = params.get("source");
  const { kinds, sources, loading, upsert, remove } = useDataSources();
  const catalog = useRecipeCatalog();
  const { accounts } = useCloudAccounts();
  const store = useStoreCredentials();
  const offers = useMemo(() => unusedCloudAccounts(accounts, kinds, sources), [accounts, kinds, sources]);
  const [form, setForm] = useState<FormTarget | null>(null);
  const [verifying, setVerifying] = useState<ReadonlySet<string>>(() => new Set());
  const [deleteTarget, setDeleteTarget] = useState<DataSource | null>(null);
  const [deleting, setDeleting] = useState(false);
  useOpsPageScope({ time: false, env: false });

  const setParam = (key: string, value: string | null) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value === null) next.delete(key);
        else next.set(key, value);
        return next;
      },
      { replace: true },
    );

  const opened = openId ? (sources.find((x) => x.id === openId) ?? null) : null;
  const editing = form?.mode === "edit" ? (sources.find((x) => x.id === form.id) ?? null) : null;

  // Deleted elsewhere (another window, an agent): the row goes, and so do
  // the drawer and the form that were open on it.
  const dropGone = (source: DataSource) => {
    remove(source.id);
    if (openId === source.id) setParam("source", null);
    setForm((f) => (f?.mode === "edit" && f.id === source.id ? null : f));
    toast.error(s("gone", { name: source.name }));
  };

  const verify = async (source: DataSource) => {
    setVerifying((prev) => new Set(prev).add(source.id));
    try {
      const updated = await api.verifyDataSource(source.id);
      upsert(updated);
      if (updated.status === "ok") toast.success(s("verified", { name: updated.name }));
      else toast.error(updated.status_detail || s("verifyFailed", { name: updated.name }));
    } catch (err) {
      if (isNotFound(err)) dropGone(source);
      else toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setVerifying((prev) => {
        const next = new Set(prev);
        next.delete(source.id);
        return next;
      });
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await api.deleteDataSource(deleteTarget.id);
      remove(deleteTarget.id);
      if (openId === deleteTarget.id) setParam("source", null);
      toast.success(s("deleted"));
    } catch (err) {
      if (isNotFound(err)) dropGone(deleteTarget);
      else toast.error(err instanceof Error ? err.message : t("common.actionFailed"));
    } finally {
      setDeleting(false);
      setDeleteTarget(null);
    }
  };

  const addButton = (
    <Button onClick={() => setForm({ mode: "create" })} disabled={kinds.length === 0}>
      <Plus aria-hidden />
      {s("add")}
    </Button>
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          <Tabs value={tab} onValueChange={(v) => setParam("tab", v === "recipes" ? "recipes" : null)} variant="pill" className="w-fit">
            <TabsList aria-label={s("title")}>
              <TabsTrigger value="sources">{s("tabs.sources")}</TabsTrigger>
              <TabsTrigger value="recipes">{s("tabs.recipes")}</TabsTrigger>
            </TabsList>
          </Tabs>
          <p className="max-w-prose text-caption text-muted-foreground">{s("description")}</p>
        </div>
        {tab === "sources" && sources.length > 0 && addButton}
      </div>

      {tab === "sources" && !loading && offers.length > 0 && (
        <div className="space-y-2">
          {offers.map((offer) => (
            <Notice
              key={offer.account.id}
              variant="info"
              title={s("cloudOffer.title", { provider: t(`cloud.providers.${offer.account.provider}`) })}
            >
              <p>
                {s("cloudOffer.body", {
                  account: offer.account.label || offer.account.id,
                  kind: kindLabel(t, offer.kind),
                })}
              </p>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() =>
                  setForm({ mode: "create", initial: { kind: offer.kind.kind, values: { [offer.field.key]: offer.account.id } } })
                }
              >
                <Cloud aria-hidden />
                {s("cloudOffer.action", { kind: kindLabel(t, offer.kind) })}
              </Button>
            </Notice>
          ))}
        </div>
      )}

      {tab === "recipes" ? (
        <RecipeCatalog recipes={catalog.recipes} signals={catalog.signals} kinds={kinds} loading={catalog.loading} />
      ) : loading && sources.length === 0 ? (
        <Skeleton className="h-48 w-full" />
      ) : (
        <DataSourceList
          sources={sources}
          kinds={kinds}
          cloudAccounts={accounts}
          verifying={verifying}
          onAdd={kinds.length > 0 ? () => setForm({ mode: "create" }) : undefined}
          onOpen={(source) => setParam("source", source.id)}
          onVerify={(source) => void verify(source)}
          onEdit={(source) => setForm({ mode: "edit", id: source.id })}
          onDelete={setDeleteTarget}
        />
      )}

      <DataSourceDetailDrawer
        source={opened}
        kind={opened ? kinds.find((k) => k.kind === opened.kind) : undefined}
        cloudAccounts={accounts}
        verifying={opened !== null && verifying.has(opened.id)}
        onOpenChange={(open) => !open && setParam("source", null)}
        onVerify={(source) => void verify(source)}
        onEdit={(source) => setForm({ mode: "edit", id: source.id })}
      />

      {form && (form.mode === "create" || editing) && (
        <DataSourceForm
          open
          onOpenChange={(open) => !open && setForm(null)}
          kinds={kinds}
          source={editing}
          onSaved={upsert}
          onGone={dropGone}
          cloudAccounts={accounts}
          storeCredentials={store.loading && store.credentials.length === 0 ? null : store.credentials}
          initial={form.mode === "create" ? form.initial : undefined}
        />
      )}

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={s("deleteTitle", { name: deleteTarget?.name ?? "" })}
        description={s("deleteDescription")}
        confirmLabel={s("delete")}
        loading={deleting}
        onConfirm={confirmDelete}
      />
    </div>
  );
}
