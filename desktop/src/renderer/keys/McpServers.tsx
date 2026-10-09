import { useCallback, useEffect, useState, type FormEvent } from "react";
import { CheckCircle2, Loader2, Plus, Server, Trash2 } from "lucide-react";
import type { KeysBridge } from "@ipc/channels.js";
import type { McpServersState, McpServerSummary } from "@ipc/types.js";
import { Button } from "@shared/ui/button.js";
import { emptyMcpForm, mcpFormFor, mcpFormProblem, secretLabel, toMcpRequest, type McpForm, type McpTransport } from "./mcp-form";

const FIELD =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50";

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * The window's second section: the member's own MCP servers on this computer.
 * A secret (an environment variable of a command, a header of an address) is
 * typed here and never shown again — no call returns one.
 */
export default function McpServers({ keys }: { keys: KeysBridge }) {
  const [servers, setServers] = useState<McpServerSummary[] | null>(null);
  const [form, setForm] = useState<McpForm>(emptyMcpForm());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const applied = useCallback((state: McpServersState, done: string) => {
    setServers(state.servers);
    setNotice(state.runnerRestarting ? `${done} The runner is restarting to use it.` : done);
  }, []);

  useEffect(() => {
    keys.mcpList().then(
      (state) => setServers(state.servers),
      (err: unknown) => setError(message(err)),
    );
  }, [keys]);

  const save = useCallback(
    (event: FormEvent) => {
      event.preventDefault();
      const problem = mcpFormProblem(form);
      if (problem) {
        setError(problem);
        setNotice(null);
        return;
      }
      const request = toMcpRequest(form);
      setForm((f) => ({ ...f, secrets: f.secrets.map((row) => ({ ...row, value: "" })) }));
      setBusy(true);
      setError(null);
      setNotice(null);
      keys.mcpSet(request).then(
        (state) => {
          setBusy(false);
          setForm(emptyMcpForm());
          applied(state, "Saved.");
        },
        (err: unknown) => {
          setBusy(false);
          setError(message(err));
        },
      );
    },
    [form, keys, applied],
  );

  const remove = useCallback(
    (name: string) => {
      setBusy(true);
      setError(null);
      setNotice(null);
      keys.mcpRemove(name).then(
        (state) => {
          setBusy(false);
          setForm((f) => (f.editing === name ? emptyMcpForm() : f));
          applied(state, "Removed.");
        },
        (err: unknown) => {
          setBusy(false);
          setError(message(err));
        },
      );
    },
    [keys, applied],
  );

  const editing = form.editing !== undefined;
  const label = secretLabel(form.transport);

  return (
    <section className="mt-10 border-t border-border pt-6">
      <header className="flex items-center gap-2">
        <Server className="size-5 text-muted-foreground" />
        <h2 className="text-base font-semibold">MCP servers on this computer</h2>
      </header>
      <p className="mt-2 text-sm text-muted-foreground">
        Your own MCP servers, available to agents that run here as <span className="font-mono">mcp_&lt;name&gt;_&lt;tool&gt;</span>{" "}
        when the team&apos;s agent is set to use them. Environment variables and headers are encrypted with this
        computer&apos;s keychain and never leave it; put secrets there, not in the arguments.
      </p>

      <div className="mt-4 divide-y divide-border rounded-md border border-border">
        {servers === null ? (
          <div className="flex items-center gap-2 px-3 py-3 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </div>
        ) : servers.length === 0 ? (
          <p className="px-3 py-3 text-sm text-muted-foreground">No MCP server yet.</p>
        ) : (
          servers.map((s) => (
            <div key={s.name} className="flex items-start gap-3 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="selectable truncate font-mono text-sm font-medium">{s.name}</p>
                <p className="selectable truncate text-xs text-muted-foreground">
                  {s.transport === "http" ? s.url : [s.command, ...(s.args ?? [])].join(" ")}
                </p>
              </div>
              <span className={`shrink-0 text-xs ${s.hasSecret ? "text-success" : "text-muted-foreground"}`}>
                {s.hasSecret ? (
                  <span className="inline-flex items-center gap-1">
                    <CheckCircle2 className="size-3.5" /> Secret set
                  </span>
                ) : (
                  "No secret"
                )}
              </span>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => setForm(mcpFormFor(s))}>
                Edit
              </Button>
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => remove(s.name)} title={`Remove ${s.name}`}>
                <Trash2 />
              </Button>
            </div>
          ))
        )}
      </div>

      <form className="mt-5 space-y-3" onSubmit={save} autoComplete="off">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          {editing ? `Change ${form.editing}` : (
            <>
              <Plus className="size-4" /> Add an MCP server
            </>
          )}
        </h3>

        <label className="block space-y-1">
          <span className="text-xs text-muted-foreground">Name — letters, digits and hyphens</span>
          <input
            className={FIELD}
            value={form.name}
            disabled={busy || editing}
            spellCheck={false}
            placeholder="notes"
            onChange={(e) => setForm({ ...form, name: e.target.value })}
          />
        </label>

        <label className="block space-y-1">
          <span className="text-xs text-muted-foreground">Kind</span>
          <select
            className={FIELD}
            value={form.transport}
            disabled={busy || editing}
            onChange={(e) => setForm({ ...form, transport: e.target.value as McpTransport, secrets: [] })}
          >
            <option value="stdio">A command on this computer</option>
            <option value="http">An address (http)</option>
          </select>
        </label>

        {form.transport === "stdio" ? (
          <>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Command</span>
              <input
                className={FIELD}
                value={form.command}
                disabled={busy}
                spellCheck={false}
                placeholder="/usr/local/bin/notes-mcp"
                onChange={(e) => setForm({ ...form, command: e.target.value })}
              />
            </label>
            <label className="block space-y-1">
              <span className="text-xs text-muted-foreground">Arguments, one per line (optional)</span>
              <textarea
                className={FIELD}
                rows={3}
                value={form.args}
                disabled={busy}
                spellCheck={false}
                onChange={(e) => setForm({ ...form, args: e.target.value })}
              />
            </label>
          </>
        ) : (
          <label className="block space-y-1">
            <span className="text-xs text-muted-foreground">Address — http or https, without a password in it</span>
            <input
              className={FIELD}
              value={form.url}
              disabled={busy}
              spellCheck={false}
              placeholder="https://wiki.example/mcp"
              onChange={(e) => setForm({ ...form, url: e.target.value })}
            />
          </label>
        )}

        <fieldset className="space-y-2">
          <legend className="text-xs text-muted-foreground">
            {label} (optional){editing ? " — leave a value empty to keep the stored one" : ""}
          </legend>
          {form.secrets.map((row, i) => (
            <div key={i} className="flex gap-2">
              <input
                className={FIELD}
                value={row.name}
                disabled={busy}
                spellCheck={false}
                aria-label={form.transport === "http" ? "Header name" : "Variable name"}
                placeholder={form.transport === "http" ? "Authorization" : "NOTES_TOKEN"}
                onChange={(e) =>
                  setForm({ ...form, secrets: form.secrets.map((r, j) => (j === i ? { ...r, name: e.target.value } : r)) })
                }
              />
              <input
                className={FIELD}
                type="password"
                value={row.value}
                disabled={busy}
                autoComplete="off"
                spellCheck={false}
                aria-label="Value"
                onChange={(e) =>
                  setForm({ ...form, secrets: form.secrets.map((r, j) => (j === i ? { ...r, value: e.target.value } : r)) })
                }
              />
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={busy}
                title="Remove this row"
                onClick={() => setForm({ ...form, secrets: form.secrets.filter((_, j) => j !== i) })}
              >
                <Trash2 />
              </Button>
            </div>
          ))}
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={busy}
            onClick={() => setForm({ ...form, secrets: [...form.secrets, { name: "", value: "" }] })}
          >
            <Plus /> Add {form.transport === "http" ? "a header" : "a variable"}
          </Button>
        </fieldset>

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" disabled={busy}>
            {busy ? <Loader2 className="animate-spin" /> : null}
            Save
          </Button>
          {editing ? (
            <Button type="button" variant="ghost" disabled={busy} onClick={() => setForm(emptyMcpForm())}>
              Cancel
            </Button>
          ) : null}
        </div>
        {error ? <p className="text-xs text-destructive">{error}</p> : null}
        {notice ? <p className="text-xs text-muted-foreground">{notice}</p> : null}
      </form>
    </section>
  );
}
