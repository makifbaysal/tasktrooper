# TaskTrooper UI

React + TypeScript + Vite SPA. It is the **desktop app's** UI: `..` (the desktop shell)
bundles `dist/` and serves it from `app://tasktrooper`. It also runs in a
browser against a local server, which is the development path. It is not a
deployable web app — no hosting, no CDN, no multi-tenant anything.

Docs index:

- [Frontend Components (Atomic Design)](.ai/frontend-components.md) — mandatory
- [API Specification](.ai/api-spec.md) — the endpoints this app calls

## Code comments

Do not add code comments unless truly necessary — a non-obvious invariant, a
workaround, or a WHY that isn't clear from the code itself. Never explain WHAT
the code does; well-named identifiers already do that.

## Frontend UI rule (mandatory)

All UI in `src/` follows **Atomic Design**: atom → molecule → organism →
template → page. Before writing any UI:

1. **Reuse first.** Never hand-roll a button, card, badge, input, dialog,
   header, empty state or list row — import the shared component.
2. **No ad-hoc equivalents.** A `<div className="rounded-lg border p-4">` that
   duplicates `Card`, or a bare styled `<button>` that duplicates `Button`, is
   a bug. Raw elements are for genuinely one-off layout wrappers only.
3. **Place new components at the correct atomic level.** Atoms only in
   `src/components/ui/`; molecules/organisms in the closest feature directory
   (`chat/`, `board/`, `workspace/`, `agent/`, `projects/`, `admin/`, `runner/`,
   `setup/`) or `layout/` for structural pieces; pages in `src/pages/`.
4. **Update shared components backward-compatibly.** New props get defaults.
   Before any breaking change, `grep -rn "<ComponentName" src` for every caller
   and update them in the same change.

The full rules and the component inventory live in
[.ai/frontend-components.md](.ai/frontend-components.md). Read it before UI
work; it is the authority, this section is the summary.

## Auth

There is no login screen and no account. `src/lib/auth.ts` resolves one bearer
token — the desktop shell's `window.__tasktrooperDesktop.apiToken`, else
`VITE_API_KEY` — and `main.tsx` renders `ConfigErrorPage` when there is none.
Nothing else in `src/` may read a credential.

## API calls

- One function per endpoint in `src/api.ts`, all through `request()`, which
  goes to `apiUrl()` (`src/lib/apiBase.ts`) with `authHeaders()`.
- `apiUrl()` prefixes the desktop shell's `apiBase` if present, else
  `VITE_API_BASE`, else nothing (the dev proxy).
- Never point an `<img src>` at `/v1/attachments/{id}` — it needs the
  Authorization header; use `attachments/useAttachmentBlob`.
- No route outside `/v1`, `/admin` and `/health` exists. There is no gateway,
  no control plane and no OAuth broker to call.

## Desktop bridge

`src/lib/desktop-bridge.ts` is one half of a contract whose other half is
`../src/ipc/host.ts`. They are separate declarations because this app
builds with no knowledge of that package; **change both together**. The shell
exposes `info()`, `apiBase`, `apiToken` and `runner` (process supervision,
preflight, settings, diagnostics); `runner.connect()`/`disconnect()` start and
stop the **backend**, not a tunnel.

## Locales

`src/locales/en.ts` is the source of truth and defines `Dict`; every other
language (`tr`, `es`, `de`, `fr`, `pt`, `zh` — registered in
`src/lib/languages.ts`) is typed as `Dict`, so a missing or extra key fails
`tsc`. Keys built at runtime escape that check — `npm run check:locales`
compares every language's flattened key set and `{placeholder}` names with
`en`. A new English key needs a translation in every language.

Only `en` is bundled with the app's entry (it is every lookup's fallback); the
others are lazy chunks loaded by `loadLocale` in `hooks/useI18n` — `main.tsx`
awaits the stored language before the first render and `setLang` switches only
once its dictionary has loaded. Import a dictionary statically only in tests.

## Verify before committing

```bash
npm ci
npx tsc --noEmit
npm run build
npm run check:locales
```
