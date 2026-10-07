import { readdir, stat } from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { app, net, protocol } from "electron";

/**
 * The origin the product is served from inside this app.
 *
 * The assets ship in the bundle and are served from here, so no screen is a
 * network round trip and the page's origin is a constant this process controls.
 *
 * ## Why a custom scheme rather than file://
 *
 * `file://` is not an origin in the sense the web platform means. It has no
 * host, so it is opaque to `localStorage`, IndexedDB and cookies, and it is not
 * a secure context, so anything gated on that is simply missing. A registered
 * scheme is a real origin: `app://tasktrooper` looks to the page like a host it
 * was served from, and its stored state survives a restart.
 *
 * The page reaches the backend cross-origin, at `http://127.0.0.1:<port>`,
 * which is why the backend's CORS list names this origin.
 */
export const APP_SCHEME = "app";
export const APP_HOST = "tasktrooper";
export const APP_ORIGIN = `${APP_SCHEME}://${APP_HOST}`;

/**
 * Must run before `app.whenReady()`. Electron reads the privilege table once,
 * while the network service starts; registering later leaves the scheme without
 * a secure context and without fetch access, and neither failure names itself —
 * the page just behaves as if the browser were ten years old.
 */
export function registerAppSchemePrivileges(): void {
  protocol.registerSchemesAsPrivileged([
    {
      scheme: APP_SCHEME,
      privileges: {
        standard: true,
        secure: true,
        supportFetchAPI: true,
        // No CORS bypass. The page talks to the backend cross-origin and the
        // backend answers with the headers that permit it (this origin is in
        // its CORS list); granting the scheme a blanket exemption would let a
        // bug here reach any host.
        corsEnabled: true,
        stream: true,
        // V8 caches the compiled bundle between launches, as it does for an
        // http(s) page. Without it every launch parses and compiles the whole
        // SPA from source again before the first screen can render.
        codeCache: true,
      },
    },
  ]);
}

/**
 * The origin of a URL, for schemes the URL parser does not consider special.
 *
 * `new URL("app://tasktrooper/x").origin` is the string `"null"`. That is the
 * standard's answer for any non-special scheme, and it is a trap in both
 * directions: a check for `origin === APP_ORIGIN` can never pass, and a check
 * that two origins are EQUAL passes for every pair of custom-scheme URLs,
 * because both sides are `"null"`. The first only refuses honest calls; the
 * second would let `evil://anything` count as same-origin with the product.
 *
 * `protocol + host` says what origin means here, and agrees with `origin` for
 * http(s) — `host` carries the port when there is one. Null for a URL with no
 * host, so an opaque origin can never compare equal to another one.
 */
export function originOf(raw: string): string | null {
  try {
    const url = new URL(raw);
    return url.host === "" ? null : `${url.protocol}//${url.host}`;
  } catch {
    return null;
  }
}

/**
 * Where the built web assets are.
 *
 * Packaged: `Contents/Resources/web`, which is where electron-builder stages
 * `ui/dist`. Dev: `ui/dist` inside this package — the SPA is a workspace of
 * this app, not a sibling repository, and `npm run build:ui` is what fills it.
 * The two names have to agree with `electron-builder.yml`'s `to: web`.
 */
export function webRoot(): string {
  if (app.isPackaged) return path.join(process.resourcesPath, "web");
  return path.join(app.getAppPath(), "ui", "dist");
}

/**
 * Where the role agent catalog is.
 *
 * Packaged: `Contents/Resources/catalog`, the extraResources staging agreed
 * with electron-builder.yml's `to: catalog`. Dev: the monorepo's `catalog/`
 * next to this package. Either way the backend is handed this path as
 * `AGENT_CATALOG_REPO` and syncs its six role agents from it at boot
 * (server/resources/config.yml: agent_catalog.source).
 */
export function catalogRoot(): string {
  if (app.isPackaged) return path.join(process.resourcesPath, "catalog");
  return path.resolve(app.getAppPath(), "..", "catalog");
}

/**
 * Serve the bundle. Call once, after the app is ready.
 *
 * Two rules, and the second is the one that matters:
 *
 *   - Anything resolving to a real file inside the root is served.
 *   - Anything else falls back to index.html, because this is a single-page
 *     app: `/settings/llm` is a route the router knows, not a file, and a
 *     reload on that path must not 404.
 *
 * That fallback is exactly why the containment check cannot be skipped. A
 * request naming `../../etc/passwd` has to fail as "outside the root" — not be
 * quietly answered with index.html, and certainly not read. `path.join` on a
 * decoded URL is how a static handler becomes an arbitrary file read.
 */
export function serveAppScheme(): void {
  const root = webRoot();
  const files = new WebRootFiles(root);

  protocol.handle(APP_SCHEME, async (request) => {
    const url = new URL(request.url);
    if (url.host !== APP_HOST) return new Response("not found", { status: 404 });

    const relative = decodeURIComponent(url.pathname).replace(/^\/+/, "");
    const candidate = path.resolve(root, relative);
    const inside = candidate === root || candidate.startsWith(root + path.sep);

    const index = path.join(root, "index.html");
    const file = inside && relative !== "" && (await files.has(candidate)) ? candidate : index;

    // The single most confusing failure this handler can produce, so it says so
    // rather than answering quietly. An API call arriving here means the page
    // never got its base and is calling its own origin; the fallback would hand
    // it index.html with a 200, and the app would report a parse failure — or
    // nothing at all — several layers from the cause.
    if (file !== candidate && (relative === "api" || relative.startsWith("api/"))) {
      console.warn(
        `[app-scheme] ${request.method} /${relative} was requested on the app's own origin. ` +
          `The page has no API base, so it is calling itself. See CLOUD_CHANNELS.apiBase.`,
      );
    }
    if (file === index && !(await files.has(index))) {
      // A build that shipped no assets is a window with nothing in it, and the
      // reason is worth saying once rather than leaving a blank frame.
      return new Response(`No web assets at ${root}. Run \`npm run build:ui\` first.`, {
        status: 500,
        headers: { "content-type": "text/plain; charset=utf-8" },
      });
    }
    // Not `file://${file}`: on Windows that is `file://C:\...`, which reads
    // C: as a host, and anywhere a `#`, `?` or `%` in the install path would
    // be parsed as part of the URL rather than the file name.
    return net.fetch(pathToFileURL(file).toString());
  });
}

/**
 * Which files the web root holds, answered without blocking the main process.
 *
 * The root is listed once, asynchronously, the first time it is asked about;
 * a hashed asset is then a Set lookup. A name the listing does not have — a
 * client-side route, or a file a dev rebuild added since — gets one async
 * `stat`, so a rebuilt `ui/dist` is still served correctly without a restart.
 */
export class WebRootFiles {
  readonly #root: string;
  #listing: Promise<Set<string>> | null = null;

  constructor(root: string) {
    this.#root = root;
  }

  async has(file: string): Promise<boolean> {
    this.#listing ??= listFiles(this.#root);
    if ((await this.#listing).has(file)) return true;
    try {
      return (await stat(file)).isFile();
    } catch {
      return false;
    }
  }
}

async function listFiles(root: string): Promise<Set<string>> {
  try {
    const entries = await readdir(root, { recursive: true, withFileTypes: true });
    return new Set(entries.filter((e) => e.isFile()).map((e) => path.join(e.parentPath, e.name)));
  } catch {
    return new Set();
  }
}
