import { EventEmitter } from "node:events";
import type { AccountMode, AccountState, RunnerPairingBundle, UserSettings } from "../../ipc/types.js";
import { ACCOUNT_LOGIN_ROUTE, normalizeAccountOrigin, originOfUrl, type OriginRules } from "./origin.js";

/**
 * Whether account mode keeps the local embedder running.
 *
 * On: the code index and its embeddings are this computer's in account mode
 * too, so the embedder starts at every launch, survives the switch into
 * account mode (`supervisor.disconnect()`, not `drain()`), and its URL goes to
 * the runner as `embeddings_base_url` (`main/runner/env.ts`), which hands it
 * to the executor. It costs little until something asks: the model loads on
 * the first request and unloads after ten idle minutes
 * (`embedder/src/server.ts`).
 */
export const ACCOUNT_MODE_RUNS_EMBEDDER = true;

/**
 * What the switch drives. Each is a step the transition names, so the order
 * of the transition is this file's and the mechanics of each step are the
 * caller's.
 */
export interface ModeControllerDeps {
  settings: { get(): UserSettings; set(patch: Partial<UserSettings>): UserSettings };
  rules: OriginRules;
  defaultOrigin: string;
  /** Stop the local backend (its Postgres with it) and the notification watcher. */
  stopLocal(): Promise<void>;
  /** Start the local backend; the window follows once it answers. */
  startLocal(): Promise<void>;
  /** Stop the runner and its executor, and forget the pairing. */
  stopAccount(): Promise<void>;
  /** Replace the window's page with the account's web app at `route`. */
  showAccount(origin: string, route: string): void;
  /** Take the account's page down; the local page comes back with the backend. */
  showLocal(): void;
  /** Clear the account partition's cookies, storage and cache. */
  clearAccountSession(origin: string): Promise<void>;
  paired(): boolean;
}

export interface ModeControllerEvents {
  state: [state: AccountState];
}

/**
 * The runtime switch between local mode and account mode.
 *
 * Entering account mode: the local backend and the notification watcher stop,
 * the mode is persisted, and the window loads `${origin}/login` in that
 * origin's own partition. Pairing follows from the page (`checkPairing`).
 *
 * Leaving it: the account's page goes first — nothing on the remote origin
 * holds the bridge while the rest happens — then the runner and the executor
 * stop and the pairing is forgotten, the mode is persisted, the partition is
 * cleared, and the local backend starts; `app://tasktrooper` loads when it
 * answers. Local data is never touched.
 *
 * Transitions are serialised: a second click waits for the first.
 */
export class ModeController extends EventEmitter<ModeControllerEvents> {
  readonly #deps: ModeControllerDeps;
  #switching = false;
  #error: string | undefined;
  #transition: Promise<unknown> = Promise.resolve();

  constructor(deps: ModeControllerDeps) {
    super();
    this.#deps = deps;
  }

  get mode(): AccountMode {
    return this.#deps.settings.get().mode === "account" ? "account" : "local";
  }

  /**
   * The account origin: the one last signed in to, when this build still
   * accepts it, and the default otherwise. A stored loopback origin from a
   * development run is not honoured by a packaged build.
   */
  get origin(): string {
    const stored = this.#deps.settings.get().accountOrigin;
    const accepted = stored ? normalizeAccountOrigin(stored, this.#deps.rules) : null;
    return accepted ?? this.#deps.defaultOrigin;
  }

  state(): AccountState {
    return {
      mode: this.mode,
      origin: this.origin,
      switching: this.#switching,
      paired: this.#deps.paired(),
      ...(this.#error !== undefined ? { error: this.#error } : {}),
    };
  }

  signIn(requested?: string): Promise<AccountState> {
    return this.#serialise(async () => {
      const origin = requested === undefined ? this.origin : normalizeAccountOrigin(requested, this.#deps.rules);
      if (!origin) {
        throw new Error(`${requested ?? ""} is not an account address this app will open. Use an https:// address.`);
      }
      if (this.mode === "account") {
        if (origin !== this.origin) {
          throw new Error(`This computer is signed in to ${this.origin}. Sign out first.`);
        }
        return this.state();
      }

      this.#begin();
      try {
        await this.#deps.stopLocal();
        this.#deps.settings.set({ mode: "account", accountOrigin: origin });
        this.#deps.showAccount(origin, ACCOUNT_LOGIN_ROUTE);
      } catch (err) {
        this.#error = describe(err);
        this.#deps.settings.set({ mode: "local" });
        void this.#deps.startLocal();
        throw err;
      } finally {
        this.#end();
      }
      return this.state();
    });
  }

  signOut(): Promise<AccountState> {
    return this.#serialise(async () => {
      if (this.mode !== "account") return this.state();
      const origin = this.origin;
      this.#begin();
      try {
        this.#deps.showLocal();
        await this.#deps.stopAccount();
        this.#deps.settings.set({ mode: "local" });
        await this.#deps.clearAccountSession(origin).catch((err: unknown) => {
          this.#error = `The account's session could not be cleared: ${describe(err)}`;
        });
      } finally {
        this.#end();
      }
      void this.#deps.startLocal();
      return this.state();
    });
  }

  /**
   * Why a pairing bundle may not be stored, or null when it may. A bundle is
   * only for the account this computer is signed in to: its `tm_base_url` is
   * where the runner dials with a bearer token, and a page must not be able to
   * point that anywhere else.
   */
  checkPairing(bundle: RunnerPairingBundle): string | null {
    if (this.mode !== "account") return "This computer is not signed in to an account.";
    const target = originOfUrl(bundle.tm_base_url);
    if (target !== this.origin) {
      return `The pairing is for ${target ?? "an unknown address"}, but this computer is signed in to ${this.origin}.`;
    }
    return null;
  }

  #begin(): void {
    this.#switching = true;
    this.#error = undefined;
    this.#emit();
  }

  #end(): void {
    this.#switching = false;
    this.#emit();
  }

  #emit(): void {
    this.emit("state", this.state());
  }

  #serialise<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.#transition.then(fn, fn);
    this.#transition = next.catch(() => undefined);
    return next;
  }
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
