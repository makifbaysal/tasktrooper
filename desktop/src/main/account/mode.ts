import { EventEmitter } from "node:events";
import type { AccountMode, AccountState, RunnerPairingBundle, UserSettings } from "../../ipc/types.js";
import { ACCOUNT_HOME_ROUTE, ACCOUNT_LOGIN_ROUTE, normalizeAccountOrigin, originOfUrl, type OriginRules } from "./origin.js";

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

/** How long signing out waits for the account's server to acknowledge the revocation. */
export const REVOKE_WAIT_MS = 2_000;

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
  /** Stop the runner and its executor; the pairing stays. */
  pauseAccount(): Promise<void>;
  /** Start the runner again when this computer is paired. */
  resumeAccount(): Promise<void>;
  /** Replace the window's page with the account's web app at `route`. */
  showAccount(origin: string, route: string): void;
  /** Take the account's page down; the local page comes back with the backend. */
  showLocal(): void;
  /**
   * Best-effort: ask the account's server to end the session this computer's
   * partition holds (its logout endpoint). May reject or hang; the caller
   * bounds the wait and ignores the outcome — an unreachable account must not
   * make signing out fail or slow.
   */
  revokeAccountSession(origin: string): Promise<void>;
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
 * holds the bridge while the rest happens — then the mode is persisted as
 * local (before anything that can fail, so the next launch is local whatever
 * happens below), the account's server is asked to end the session
 * (best-effort, bounded), the runner and the executor stop and the pairing is
 * forgotten, the partition is cleared, and the local backend starts;
 * `app://tasktrooper` loads when it answers. Local data is never touched.
 *
 * Running locally FOR NOW (`useLocalForNow`) is the third state, for when the
 * account's page cannot be reached: still signed in and paired, the persisted
 * mode still `account`, but the runner is stopped and the local backend and
 * `app://tasktrooper` are what run — `mode` reads "local" while it lasts.
 * Signing in again goes back to the account; signing out is the one way that
 * forgets it; the next launch simply tries the account again.
 *
 * Transitions are serialised: a second click waits for the first.
 */
export class ModeController extends EventEmitter<ModeControllerEvents> {
  readonly #deps: ModeControllerDeps;
  #switching = false;
  #temporaryLocal = false;
  #error: string | undefined;
  #transition: Promise<unknown> = Promise.resolve();

  constructor(deps: ModeControllerDeps) {
    super();
    this.#deps = deps;
  }

  /** The mode in effect: local while running locally for now. */
  get mode(): AccountMode {
    return this.#signedIn && !this.#temporaryLocal ? "account" : "local";
  }

  get temporaryLocal(): boolean {
    return this.#temporaryLocal;
  }

  /** The persisted mode: signed in, whether or not the account is on screen. */
  get #signedIn(): boolean {
    return this.#deps.settings.get().mode === "account";
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
      temporaryLocal: this.#temporaryLocal,
      ...(this.#error !== undefined ? { error: this.#error } : {}),
    };
  }

  signIn(requested?: string): Promise<AccountState> {
    return this.#serialise(async () => {
      const origin = requested === undefined ? this.origin : normalizeAccountOrigin(requested, this.#deps.rules);
      if (!origin) {
        throw new Error(`${requested ?? ""} is not an account address this app will open. Use an https:// address.`);
      }
      if (this.#temporaryLocal) {
        if (origin !== this.origin) {
          throw new Error(`This computer is signed in to ${this.origin}. Sign out first.`);
        }
        return this.#backToAccount();
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

  /**
   * Run locally for now: the account's page goes, the runner stops with its
   * pairing kept, and the local backend starts. Nothing about the account is
   * forgotten or written — the next launch opens it again.
   */
  useLocalForNow(): Promise<AccountState> {
    return this.#serialise(async () => {
      if (this.mode !== "account") return this.state();
      this.#begin();
      this.#deps.showLocal();
      // On regardless: the account's page is already gone, and a window with
      // neither page is the one outcome worse than a runner that stopped
      // untidily.
      await this.#deps.pauseAccount().catch((err: unknown) => {
        this.#error = `The runner did not stop cleanly: ${describe(err)}`;
      });
      this.#temporaryLocal = true;
      this.#end();
      void this.#deps.startLocal();
      return this.state();
    });
  }

  async #backToAccount(): Promise<AccountState> {
    this.#begin();
    try {
      await this.#deps.stopLocal();
      this.#temporaryLocal = false;
      this.#deps.showAccount(this.origin, ACCOUNT_HOME_ROUTE);
    } catch (err) {
      this.#temporaryLocal = true;
      this.#error = describe(err);
      void this.#deps.startLocal();
      throw err;
    } finally {
      this.#end();
    }
    void this.#deps.resumeAccount();
    return this.state();
  }

  signOut(): Promise<AccountState> {
    return this.#serialise(async () => {
      const whileLocal = this.#temporaryLocal;
      if (!whileLocal && this.mode !== "account") return this.state();
      const origin = this.origin;
      this.#begin();
      try {
        // While local for now the account's page is already gone and the
        // local backend already up; otherwise the page goes first.
        if (!whileLocal) this.#deps.showLocal();
        // Persist first: whatever fails below, the next launch is local and
        // makes no attempt at the account.
        this.#deps.settings.set({ mode: "local" });
        this.#temporaryLocal = false;
        await this.#forgetAccount(origin);
      } finally {
        this.#end();
      }
      if (!whileLocal) void this.#deps.startLocal();
      return this.state();
    });
  }

  /**
   * The account's half of signing out. Each step is its own: a failure in one
   * (an unreachable server, a runner that will not stop, a busy partition)
   * never keeps the next from running and never fails the sign-out.
   */
  async #forgetAccount(origin: string): Promise<void> {
    await this.#bestEffortRevoke(origin);
    await this.#deps.stopAccount().catch((err: unknown) => {
      this.#error = `The runner did not stop cleanly: ${describe(err)}`;
    });
    await this.#deps.clearAccountSession(origin).catch((err: unknown) => {
      this.#error = `The account's session could not be cleared: ${describe(err)}`;
    });
  }

  /** Asks the server to end the session, waiting at most `REVOKE_WAIT_MS` and saying nothing of the outcome. */
  async #bestEffortRevoke(origin: string): Promise<void> {
    let timer: NodeJS.Timeout | undefined;
    const bound = new Promise<void>((resolve) => {
      timer = setTimeout(resolve, REVOKE_WAIT_MS);
    });
    try {
      await Promise.race([Promise.resolve().then(() => this.#deps.revokeAccountSession(origin)).catch(() => undefined), bound]);
    } finally {
      clearTimeout(timer);
    }
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
