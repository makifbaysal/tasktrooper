import { mkdirSync, readdirSync, statSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import type { ToolchainId, ToolchainJobState, ToolchainReport } from "../../ipc/types.js";
import { ToolchainDetector } from "./detect.js";
import { javaHomeOf } from "./detectors.js";
import type { FetchLike } from "./fetch-file.js";
import { ToolchainJobRunner } from "./job.js";
import { createHostFacts, createProbeKit, createStepMachine, currentPlatform } from "./machine.js";
import { planInstall } from "./plan.js";
import { ThrottledPublisher } from "./publisher.js";
import { installerFor } from "./recipes/index.js";
import { TOOLCHAIN_DEFINITIONS } from "./registry.js";
import { installedRuntime, mergeRuntime, runtimeFromReport, type Disk, type ToolchainRuntime } from "./runtime-env.js";
import { ToolchainStateStore } from "./store.js";
import type { Installer, ToolDefinition } from "./types.js";

/**
 * The toolchain feature behind one object: what `index.ts` wires to the
 * bridge, and the only thing it knows about this module. Detection, planning,
 * the job and its persistence stay separate parts; this composes them and
 * keeps the rules that span them — a plan is made against a report, a settled
 * step makes the report stale, every change reaches the page (throttled) and
 * the file, a finished job is announced.
 */

/** What the person is asked to confirm, one line per tool. */
export interface PlannedTool {
  id: ToolchainId;
  label: string;
  method: string;
}

export interface ToolchainHooks {
  /** Every state change, throttled — to the page. */
  publish(state: ToolchainJobState): void;
  /** Once per job, when its last step settled. */
  onJobFinished(state: ToolchainJobState): void;
}

export interface ToolchainParts {
  definitions: Readonly<Record<ToolchainId, ToolDefinition>>;
  detector: ToolchainDetector;
  runner: ToolchainJobRunner;
  store: ToolchainStateStore;
  installerFor(id: ToolchainId): Installer | null;
  workDir: string;
  /** The install locations as they are on disk now; see `runtime-env.ts`. */
  installed?: () => ToolchainRuntime;
}

/** Pages this app opens for a tool: https, or the Mac App Store's own scheme. */
export function isOpenablePage(url: string): boolean {
  try {
    const { protocol } = new URL(url);
    return protocol === "https:" || protocol === "macappstore:";
  } catch {
    return false;
  }
}

export class ToolchainService {
  readonly #parts: ToolchainParts;
  readonly #publisher: ThrottledPublisher<ToolchainJobState>;

  constructor(parts: ToolchainParts, hooks: ToolchainHooks) {
    this.#parts = parts;
    const { runner, store, detector } = parts;
    this.#publisher = new ThrottledPublisher(() => runner.snapshot(), [hooks.publish, (state) => store.save(state)]);
    runner.onChange((phaseChanged) => this.#publisher.notify(phaseChanged));
    runner.onSettled(() => detector.invalidate());
    // A fresh report first: it is what `runtime()` hands the next backend or
    // runner, and the preflight the hook runs may restart one.
    runner.onFinished((state) => {
      void detector.report(true).then(
        () => hooks.onJobFinished(state),
        () => hooks.onJobFinished(state),
      );
    });
  }

  /** What the agents' processes need to reach the tools reported installed: PATH additions and JAVA_HOME. */
  runtime(): ToolchainRuntime {
    const { definitions, detector } = this.#parts;
    const reported = runtimeFromReport(detector.latest(), (id) => definitions[id].runtime, javaHomeOf);
    return mergeRuntime(reported, this.#parts.installed?.() ?? { pathDirs: [] });
  }

  report(force = false): Promise<ToolchainReport> {
    return this.#parts.detector.report(force);
  }

  /** Leaves out what the running job will already install — appending it would add nothing. */
  async plan(ids: readonly ToolchainId[]): Promise<ToolchainId[]> {
    if (ids.length === 0) return [];
    const pending = new Set(this.#parts.runner.pendingIds());
    return planInstall(ids, (await this.report()).items).filter((id) => !pending.has(id));
  }

  state(): ToolchainJobState {
    return this.#parts.runner.snapshot();
  }

  /** Resolves as soon as the job has its steps; the installs run after. */
  async install(ids: readonly ToolchainId[]): Promise<ToolchainJobState> {
    return this.enqueue(await this.plan(ids));
  }

  /** `planned` as it is — what the person confirmed, not a fresh plan that could differ from it. */
  enqueue(planned: readonly ToolchainId[]): ToolchainJobState {
    if (planned.length > 0) mkdirSync(this.#parts.workDir, { recursive: true });
    return this.#parts.runner.enqueue(planned);
  }

  describe(ids: readonly ToolchainId[]): PlannedTool[] {
    return ids.map((id) => ({
      id,
      label: this.#parts.definitions[id].label,
      method: this.#parts.installerFor(id)?.method ?? "",
    }));
  }

  cancel(): ToolchainJobState {
    return this.#parts.runner.cancel();
  }

  dismiss(): ToolchainJobState {
    return this.#parts.runner.dismiss();
  }

  /** The page for a tool on this OS, from the table — or null when there is none this app would open. */
  pageUrl(id: ToolchainId): string | null {
    const installer = this.#parts.installerFor(id);
    if (!installer) return null;
    const url = installer.url ?? this.#parts.definitions[id].docsUrl;
    return isOpenablePage(url) ? url : null;
  }

  /** Quit: the running installer's tree is killed and the job is written, synchronously. */
  shutdown(): void {
    this.#publisher.dispose();
    this.#parts.store.saveNow(this.#parts.runner.shutdown());
  }
}

const DISK: Disk = {
  isDir: (dir) => {
    try {
      return statSync(dir).isDirectory();
    } catch {
      return false;
    }
  },
  list: (dir) => {
    try {
      return readdirSync(dir);
    } catch {
      return [];
    }
  },
};

export interface ToolchainOptions {
  /** Electron's `net.fetch` in the app, so downloads go through the system's proxy. */
  fetch?: FetchLike;
}

/** The composition root: the real machine, the real table, the state file under `userData`. */
export function createToolchainService(userData: string, hooks: ToolchainHooks, options: ToolchainOptions = {}): ToolchainService {
  const platform = currentPlatform();
  const definitions = TOOLCHAIN_DEFINITIONS;
  const workDir = path.join(userData, "toolchain");
  const store = new ToolchainStateStore(path.join(userData, "toolchain-state.json"));
  const installer = (id: ToolchainId): Installer | null => installerFor(definitions[id], createHostFacts());
  const detector = new ToolchainDetector({ definitions, platform, kit: createProbeKit, installerFor: installer });
  const runner = new ToolchainJobRunner(
    {
      installerFor: installer,
      label: (id) => definitions[id].label,
      affectsRuntime: (id) => definitions[id].affectsRuntime,
      detect: (id) => detector.detect(id),
      machine: createStepMachine(workDir, options.fetch),
    },
    store.load(),
  );
  const installed = (): ToolchainRuntime => installedRuntime({ platform, home: os.homedir(), env: process.env }, DISK);
  return new ToolchainService({ definitions, detector, runner, store, installerFor: installer, workDir, installed }, hooks);
}
