import { useCallback, useEffect, useState, type FormEvent } from "react";
import { CheckCircle2, KeyRound, Loader2, Plus, Trash2 } from "lucide-react";
import { KEYS_BRIDGE_KEY, type KeysBridge } from "@ipc/channels.js";
import type { ProviderKeySummary, ProviderKeysState, ProviderKeyType } from "@ipc/types.js";
import { Button } from "@shared/ui/button.js";
import { emptyForm, formFor, PROVIDER_TYPES, toRequest, typeLabel, wantsAddress, type KeyForm } from "./form";

declare global {
  interface Window {
    [KEYS_BRIDGE_KEY]?: KeysBridge;
  }
}

const keys = window[KEYS_BRIDGE_KEY];

const FIELD =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * The API-key window. Lists this computer's providers with whether each has a
 * key, adds or replaces one, removes one. A key is typed in here and never
 * shown again — there is no call that returns one.
 */
export default function KeysApp() {
  const [providers, setProviders] = useState<ProviderKeySummary[] | null>(null);
  const [form, setForm] = useState<KeyForm>(emptyForm());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const applied = useCallback((state: ProviderKeysState, done: string) => {
    setProviders(state.providers);
    setNotice(state.runnerRestarting ? `${done} The runner is restarting to use it.` : done);
  }, []);

  useEffect(() => {
    if (!keys) return;
    keys.list().then(
      (state) => setProviders(state.providers),
      (err: unknown) => setError(message(err)),
    );
  }, []);

  const save = useCallback(
    (event: FormEvent) => {
      event.preventDefault();
      if (!keys) return;
      const request = toRequest(form);
      setForm((f) => ({ ...f, apiKey: "" }));
      setBusy(true);
      setError(null);
      setNotice(null);
      keys.set(request).then(
        (state) => {
          setBusy(false);
          setForm(emptyForm());
          applied(state, "Saved.");
        },
        (err: unknown) => {
          setBusy(false);
          setError(message(err));
        },
      );
    },
    [form, applied],
  );

  const remove = useCallback(
    (id: string) => {
      if (!keys) return;
      setBusy(true);
      setError(null);
      setNotice(null);
      keys.remove(id).then(
        (state) => {
          setBusy(false);
          setForm((f) => (f.id === id ? emptyForm() : f));
          applied(state, "Removed.");
        },
        (err: unknown) => {
          setBusy(false);
          setError(message(err));
        },
      );
    },
    [applied],
  );

  if (!keys) {
    return (
      <main className="flex h-full items-center justify-center p-8 text-sm text-muted-foreground">
        This window could not reach TaskTrooper. Close it and open API keys again.
      </main>
    );
  }

  const editing = form.id !== undefined;
  const stored = providers?.find((p) => p.id === form.id);

  return (
    <main className="h-full overflow-y-auto bg-background p-6 text-foreground">
      <header className="flex items-center gap-2">
        <KeyRound className="size-5 text-muted-foreground" />
        <h1 className="text-base font-semibold">API keys on this computer</h1>
      </header>
      <p className="mt-2 text-sm text-muted-foreground">
        Agents that run here with an API provider use these keys. They are encrypted with this computer&apos;s keychain
        and handed only to TaskTrooper&apos;s runner on this computer; TaskTrooper&apos;s servers never see them. A
        key can be replaced or removed, never shown.
      </p>

      <section className="mt-5 divide-y divide-border rounded-md border border-border">
        {providers === null ? (
          <div className="flex items-center gap-2 px-3 py-3 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </div>
        ) : providers.length === 0 ? (
          <p className="px-3 py-3 text-sm text-muted-foreground">No provider yet.</p>
        ) : (
          providers.map((p) => (
            <div key={p.id} className="flex items-start gap-3 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{typeLabel(p.type)}</p>
                <p className="selectable truncate font-mono text-xs text-muted-foreground">{p.id}</p>
                {p.base_url ? <p className="selectable truncate text-xs text-muted-foreground">{p.base_url}</p> : null}
                {p.models && p.models.length > 0 ? (
                  <p className="truncate text-xs text-muted-foreground">Models: {p.models.join(", ")}</p>
                ) : null}
              </div>
              <span className={`shrink-0 text-xs ${p.hasKey ? "text-success" : "text-muted-foreground"}`}>
                {p.hasKey ? (
                  <span className="inline-flex items-center gap-1">
                    <CheckCircle2 className="size-3.5" /> Key set
                  </span>
                ) : (
                  "No key"
                )}
              </span>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => setForm(formFor(p))}>
                {p.hasKey ? "Replace key" : "Edit"}
              </Button>
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => remove(p.id)} title={`Remove ${p.id}`}>
                <Trash2 />
              </Button>
            </div>
          ))
        )}
      </section>

      <form className="mt-6 space-y-3" onSubmit={save} autoComplete="off">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          {editing ? `Change ${form.id}` : (
            <>
              <Plus className="size-4" /> Add a provider
            </>
          )}
        </h2>

        <label className="block space-y-1">
          <span className="text-xs text-muted-foreground">Provider</span>
          <select
            className={FIELD}
            value={form.type}
            disabled={busy || editing}
            onChange={(e) => setForm({ ...emptyForm(e.target.value as ProviderKeyType) })}
          >
            {PROVIDER_TYPES.map((type) => (
              <option key={type} value={type}>
                {typeLabel(type)}
              </option>
            ))}
          </select>
        </label>

        {wantsAddress(form.type) ? (
          <label className="block space-y-1">
            <span className="text-xs text-muted-foreground">
              Address {form.type === "local" ? "(optional)" : ""} — https, or http to this computer
            </span>
            <input
              className={FIELD}
              value={form.baseUrl}
              disabled={busy}
              placeholder={form.type === "local" ? "http://127.0.0.1:1234/v1" : "https://openrouter.ai/api/v1"}
              spellCheck={false}
              onChange={(e) => setForm({ ...form, baseUrl: e.target.value })}
            />
          </label>
        ) : null}

        <label className="block space-y-1">
          <span className="text-xs text-muted-foreground">Models (optional, comma-separated; the first is the default)</span>
          <input
            className={FIELD}
            value={form.models}
            disabled={busy}
            spellCheck={false}
            onChange={(e) => setForm({ ...form, models: e.target.value })}
          />
        </label>

        <label className="block space-y-1">
          <span className="text-xs text-muted-foreground">API key{form.type === "local" ? " (optional)" : ""}</span>
          <input
            className={FIELD}
            type="password"
            value={form.apiKey}
            disabled={busy}
            autoComplete="off"
            spellCheck={false}
            placeholder={stored?.hasKey ? "Leave empty to keep the current key" : ""}
            onChange={(e) => setForm({ ...form, apiKey: e.target.value })}
          />
        </label>

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" disabled={busy}>
            {busy ? <Loader2 className="animate-spin" /> : null}
            Save
          </Button>
          {editing ? (
            <Button type="button" variant="ghost" disabled={busy} onClick={() => setForm(emptyForm())}>
              Cancel
            </Button>
          ) : null}
        </div>
        {error ? <p className="text-xs text-destructive">{error}</p> : null}
        {notice ? <p className="text-xs text-muted-foreground">{notice}</p> : null}
      </form>
    </main>
  );
}
