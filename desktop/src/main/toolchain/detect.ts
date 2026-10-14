import type { ToolchainId, ToolchainItem, ToolchainReport } from "../../ipc/types.js";
import { TOOLCHAIN_IDS } from "../../ipc/types.js";
import type { Detection, Installer, ProbeKit, ToolDefinition, ToolchainPlatform } from "./types.js";

/**
 * The toolchain report for this OS: every tool that applies here, what
 * detection found, and how it would be installed.
 *
 * One sweep at a time, like the preflight's (`services/preflight-sweep.ts`).
 * A caller arriving while one runs joins it — forced or not — as long as it
 * began after the last install step settled; one that began before is stale
 * (it says the install did not happen), so the caller gets the single
 * follow-up sweep queued behind it, which every such caller shares. With
 * nothing running, a finished sweep answers an unforced caller for a few
 * seconds: the page asks for a plan on every checkbox it toggles.
 */

export const REPORT_REUSE_MS = 10_000;

export interface ToolchainDetectorDeps {
  definitions: Readonly<Record<ToolchainId, ToolDefinition>>;
  platform: ToolchainPlatform;
  /** A fresh view of the machine, one per sweep. */
  kit: () => ProbeKit;
  installerFor: (id: ToolchainId) => Installer | null;
  now?: () => number;
}

const messageOf = (err: unknown): string => (err instanceof Error ? err.message : String(err));

export class ToolchainDetector {
  readonly #deps: ToolchainDetectorDeps;
  readonly #now: () => number;
  #running: { promise: Promise<ToolchainReport>; generation: number } | null = null;
  #followUp: Promise<ToolchainReport> | null = null;
  #last: { report: ToolchainReport; at: number; generation: number } | null = null;
  #generation = 0;

  constructor(deps: ToolchainDetectorDeps) {
    this.#deps = deps;
    this.#now = deps.now ?? Date.now;
  }

  report(force = false): Promise<ToolchainReport> {
    const running = this.#running;
    if (running) {
      if (running.generation === this.#generation) return running.promise;
      this.#followUp ??= running.promise
        .then(
          () => undefined,
          () => undefined,
        )
        .then(() => {
          this.#followUp = null;
          return this.#start();
        });
      return this.#followUp;
    }
    const last = this.#last;
    if (!force && last && last.generation === this.#generation && this.#now() - last.at < REPORT_REUSE_MS) {
      return Promise.resolve(last.report);
    }
    return this.#start();
  }

  /** The newest finished report, whatever its age; null before the first. */
  latest(): ToolchainReport | null {
    return this.#last?.report ?? null;
  }

  #start(): Promise<ToolchainReport> {
    const generation = this.#generation;
    const promise = this.#sweep().then((report) => {
      this.#last = { report, at: this.#now(), generation };
      return report;
    });
    const sweep = { promise, generation };
    this.#running = sweep;
    const clear = (): void => {
      if (this.#running === sweep) this.#running = null;
    };
    promise.then(clear, clear);
    return promise;
  }

  /** One tool, now, on a fresh view of the machine — what a step checks before and after it runs. */
  detect(id: ToolchainId): Promise<Detection> {
    return this.#detectOne(id, this.#deps.kit());
  }

  /** Whatever was found before — or is being found right now — is stale. */
  invalidate(): void {
    this.#generation += 1;
  }

  async #detectOne(id: ToolchainId, kit: ProbeKit): Promise<Detection> {
    try {
      return await this.#deps.definitions[id].detect(kit);
    } catch (err) {
      return { status: "missing", detail: `Could not check: ${messageOf(err)}` };
    }
  }

  async #sweep(): Promise<ToolchainReport> {
    const { definitions, platform } = this.#deps;
    const kit = this.#deps.kit();
    const ids = TOOLCHAIN_IDS.filter((id) => definitions[id].recipes[platform] !== undefined);
    const items = await Promise.all(
      ids.map(async (id): Promise<ToolchainItem | null> => {
        const installer = this.#deps.installerFor(id);
        if (!installer) return null;
        return toItem(definitions[id], installer, await this.#detectOne(id, kit));
      }),
    );
    return { generatedAt: this.#now(), platform, items: items.filter((i): i is ToolchainItem => i !== null) };
  }
}

export function toItem(tool: ToolDefinition, installer: Installer, found: Detection): ToolchainItem {
  return {
    id: tool.id,
    label: tool.label,
    status: found.status,
    ...(found.version !== undefined ? { version: found.version } : {}),
    ...(found.path !== undefined ? { path: found.path } : {}),
    ...(found.detail !== undefined ? { detail: found.detail } : {}),
    installKind: installer.installKind,
    method: installer.method,
    ...(installer.command !== undefined ? { command: installer.command } : {}),
    url: installer.url ?? tool.docsUrl,
    dependsOn: [...installer.dependsOn],
  };
}
