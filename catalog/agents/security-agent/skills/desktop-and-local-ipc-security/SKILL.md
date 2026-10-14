---
name: desktop-and-local-ipc-security
category: security
description: Use when the diff adds or changes a loopback HTTP or WebSocket server, a bind address, a Unix socket, named pipe, XPC or D-Bus service, an Electron or Tauri main-process handler, a custom URL scheme or file association, a privileged helper or updater, or code that spawns tools inside directories a user or download supplied
tech_stack: Desktop & local IPC
source: cloudflare/security-audit-skill (MIT) desktop, mobile and local-IPC attack classes, adapted to diff review; own wording; Electron security checklist and platform IPC documentation cited, not reproduced
---
# Desktop and Local IPC Security

## Overview

A desktop app's attackers are not "the internet" in general. They are a web page open in the user's browser (which can send requests to `127.0.0.1`), another app or OS user on the same machine, a document or archive the user opened, a link the user clicked, and remote content loaded into a webview. Each reaches the app through a different door: a loopback port, a socket, a URL scheme, an IPC channel, a file.

**Core principle:** name the attacker's starting position, then show the check that stops it at that door. A process name, a path, a port number or a claimed sender field is not authentication — an OS peer credential, a code-signature check, or an unguessable per-launch token is.

## 1. Loopback HTTP and WebSocket Servers

Any web page can send requests to `http://127.0.0.1:<port>`. "Simple" requests — a form `POST`, `text/plain` bodies, `GET` with side effects — need no CORS preflight and still execute; DNS rebinding lets a page on `attacker.example` read responses once its name resolves to `127.0.0.1`; a WebSocket upgrade carries no CORS check at all.

```go
// ❌ a local API that runs agent tasks, open to every page in every browser on the machine
mux.HandleFunc("/api/run", runHandler)
http.ListenAndServe("127.0.0.1:"+port, mux)

// ✅ a per-launch random token in a header (pages cannot read or guess it, and it is never a cookie),
//    plus a Host check that defeats DNS rebinding for the routes that need no token
func guard(next http.Handler, token string, port string) http.Handler {
	allowed := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] { http.Error(w, "bad host", http.StatusForbidden); return }
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized); return
		}
		next.ServeHTTP(w, r)
	})
}
```

Findings: a new local route outside the token middleware that changes state or returns private data; authentication by cookie on a loopback API (CSRF from any page); CORS on the local server reflecting any origin; a WebSocket endpoint that skips the token or the `Origin` check; the token placed in a URL query string that lands in logs or browser history.

## 2. Bind Address

`":8080"`, `"0.0.0.0"` and `"[::]"` listen on every interface — the LAN, a café Wi-Fi, a VPN. A local-only service must bind `127.0.0.1` (or `::1`). Go's `http.ListenAndServe(":"+port, …)`, Node's `server.listen(port)` with no host and Python's `host="0.0.0.0"` all expose it.

## 3. Unix Sockets, Named Pipes, XPC, D-Bus

- **Unix sockets:** created in a world-writable directory (`/tmp`) with a predictable name, or with mode `0777`/a permissive umask — another OS user connects or pre-creates the path. Check `SO_PEERCRED`/`getpeereid` when the service acts for a specific user.
- **Windows named pipes:** the default DACL, a missing `PIPE_REJECT_REMOTE_CLIENTS`, or a pipe name another process can create first (squatting).
- **macOS XPC and helpers:** the client verified by PID (reusable) instead of its audit token and code-signing requirement.
- **D-Bus:** a method on the system bus with no polkit action or a policy that allows any user.
- **Claimed identity:** the channel belongs to one app or user but a request field names another — bind every method to the peer credential, not the field.

## 4. Electron and Tauri Main Process

Renderer-side settings are in web-frontend-security §7; the main process is where privilege lives.

```ts
// ❌ any frame — including remote content or an injected iframe — can ask the main process to run a command
ipcMain.handle("run", (_e, cmd: string, args: string[]) => execFile(cmd, args));

// ✅ check the sender's origin on every call, and expose an operation, not a primitive
ipcMain.handle("open-project", (e, id: string) => {
  if (new URL(e.senderFrame.url).origin !== "app://tasktrooper") throw new Error("forbidden");
  return projects.open(validateId(id));
});
```

Also: `shell.openExternal(url)` without a scheme allowlist (`file:`, `smb:`, custom schemes launch other programs); missing `setWindowOpenHandler` and `will-navigate` handlers, so a link navigates the privileged window to remote content; a custom protocol handler (`protocol.handle`) that serves files by joining a path from the URL (path-traversal-and-file-handling); Tauri commands with broad `fs`/`shell` scopes reachable from remote content.

## 5. URL Schemes, File Associations and argv

A custom scheme (`tasktrooper://`), a file association or a share target can be fired by any web page, email or file. Treat everything it carries as untrusted:

- an action triggered without user confirmation or session binding (import a project, connect an account, run a task, change a setting);
- an auth callback through the scheme accepted without the pending `state` and PKCE verifier of a flow this app started;
- argv from `second-instance` or `open-url` passed on to a child process or parsed for switches — a URL can carry `--flag`-shaped text; put `--` before operands and parse the URL, not the argv string.

## 6. Privileged Helpers, Updaters and Installers

- A helper running as root/SYSTEM that takes a path, command or setting from an unprivileged caller and acts without its own per-operation check.
- A path the helper reads or executes that a lower-privileged user can write (an update staging directory, a log directory, a plugin folder) — or that it checks, then reopens (TOCTOU, path-traversal-and-file-handling §2).
- An updater that does not verify the signature, accepts a downgrade, or fetches over plain HTTP (supply-chain-and-dependency-review §7).

## 7. Spawning Tools in Supplied Directories

A directory the user did not author — an extracted archive, a downloaded workspace, a cloned third-party repo — carries configuration that tools execute: `.git/config` (`core.fsmonitor`, `core.sshCommand`, `core.hooksPath`), `.npmrc`, `.envrc`, `Makefile`, `.vscode/tasks.json`, a `pyproject.toml` build backend. Code that runs `git status`, `npm`, `make` or a language server inside such a directory without isolation runs the directory's code. Also: resolving a bare program name through a `PATH` that includes the working directory.

## 8. Local Secrets and Device Lifecycle

- A token file or local database readable by other OS users (`0644` in a shared location, a world-readable temp file).
- Secrets passed on argv (visible in `ps` to every local user) — pass them by environment or file descriptor.
- Cached data, notifications or webview storage that survive sign-out and appear under the next account.

## Not Findings

- The same OS user reading their own files, config or keychain items — the attacker is the victim.
- Malware already running as the user — it already has the user's authority.
- A loopback route that needs the token, or a health check that returns nothing private.

## Severity Guide

Any web page reaching command execution, file read or account actions through a loopback server, URL scheme or IPC → CRITICAL. A service exposed to the LAN without authentication → HIGH. Another local user reading credentials or driving a privileged helper → HIGH. A secret on argv → MEDIUM.

## Common Mistakes

- Assuming `127.0.0.1` means "only this app can call it" — browsers can, and so can every local process.
- Reporting a loopback API without checking whether the token middleware already wraps it.
- Reporting same-user file permissions on a single-user desktop as a vulnerability.
- Treating `contextIsolation: true` as protection for an `ipcMain` handler that executes whatever the renderer sends.

## Red Flags

- A new route registered on the local server outside the authenticated group, or authenticated by cookie.
- `listen(":port")`, `0.0.0.0` or `[::]` on a local-only service.
- `ipcMain.handle`/`ipcMain.on` that never reads `event.senderFrame`, and passes arguments to `exec`, `spawn`, `fs` or `shell.openExternal`.
- `setAsDefaultProtocolClient` or `open-url` handling that performs an action directly.
- `git`, `npm`, `make` or a build tool spawned with `cwd` set to an extracted or downloaded directory.

## References (names and links only)

[Electron security checklist](https://www.electronjs.org/docs/latest/tutorial/security) · [Tauri security](https://v2.tauri.app/security/) · [OWASP Desktop App Security Top 10](https://owasp.org/www-project-desktop-app-security-top-10/) · CWE-214, 306, 346, 427, 668, 1385 · [Cloudflare security-audit-skill](https://github.com/cloudflare/security-audit-skill)
