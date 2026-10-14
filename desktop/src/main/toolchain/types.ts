import type { ToolchainId, ToolchainInstallKind, ToolchainItem, ToolchainReport } from "../../ipc/types.js";
import type { RuntimeExposure } from "./runtime-env.js";

/**
 * The toolchain module's own vocabulary.
 *
 * Three seams, each an interface so the parts can be swapped and tested alone:
 *
 *  - `ProbeKit`: what a detector may ask of the machine. Detectors are
 *    functions of its answers, so the registry's detection is the same code on
 *    a test's fake machine and on a real one.
 *  - `InstallSpec` → `Installer`: the table says WHAT to install (data); a
 *    recipe strategy per `kind` turns it into HOW on this host, including the
 *    honest fallback to `manual` when the channel is not here (no winget, no
 *    pkexec).
 *  - `StepContext`: everything an installer may do while it runs — spawn,
 *    download, report progress, re-check detection. The job runner builds it;
 *    an installer never reaches past it.
 */

export type ToolchainPlatform = ToolchainReport["platform"];

export const TOOLCHAIN_PLATFORMS: readonly ToolchainPlatform[] = ["darwin", "win32", "linux"];

export type Detection = Pick<ToolchainItem, "status" | "version" | "path" | "detail">;

export interface ProbeResult {
  code: number;
  /** Both streams, trimmed. */
  out: string;
  timedOut: boolean;
}

export interface ProbeKit {
  readonly platform: ToolchainPlatform;
  readonly arch: string;
  readonly home: string;
  readonly env: Readonly<NodeJS.ProcessEnv>;
  /** The preflight's search (PATH, login PATH, the standard prefixes), then `extraDirs`. */
  which(name: string, extraDirs?: readonly string[]): string | null;
  /** Every copy that search finds, in the same order. */
  whichAll(name: string, extraDirs?: readonly string[]): string[];
  isExecutable(file: string): boolean;
  exists(file: string): boolean;
  list(dir: string): string[];
  run(command: string, args: readonly string[]): Promise<ProbeResult>;
  /** macOS: what `xcode-select -p` names, when that directory exists; null otherwise. Asked once per kit. */
  developerDir(): Promise<string | null>;
}

export type Detector = (kit: ProbeKit) => Promise<Detection>;

/** A download whose bytes are checked against the digest the publisher lists. */
export interface PinnedAsset {
  readonly url: string;
  readonly sha1?: string;
  readonly sha256?: string;
}

/** Node's `process.arch` spelling. */
export type HostArch = "x64" | "arm64";

/**
 * What the table says about installing one tool on one OS. Data only: no
 * function, no path computed from the machine — the strategy for `kind` does
 * that, which is what keeps every entry checkable by the registry test.
 */
export type InstallSpec =
  | { readonly kind: "brew"; readonly formula: string }
  | { readonly kind: "brew-cask"; readonly cask: string }
  | { readonly kind: "winget"; readonly id: string }
  | { readonly kind: "linux-pkg"; readonly apt: readonly string[]; readonly dnf: readonly string[] }
  | { readonly kind: "snap"; readonly name: string; readonly classic: boolean }
  /** Comes with another tool: nothing of its own to run, only that tool to install first. */
  | { readonly kind: "bundled"; readonly with: ToolchainId }
  | { readonly kind: "npm-global"; readonly pkg: string }
  | { readonly kind: "appium-driver"; readonly driver: string }
  | { readonly kind: "script"; readonly url: string; readonly interpreter: "bash" | "sh" | "powershell" }
  | {
      readonly kind: "binary-download";
      readonly name: string;
      readonly assets: Readonly<Partial<Record<HostArch, PinnedAsset>>>;
      /** Absent: the asset is the program itself. */
      readonly archive?: "gz" | "tar.gz";
      /** What to take out of a `tar.gz`; `[name]` by default. */
      readonly files?: readonly string[];
    }
  | {
      readonly kind: "android-sdk";
      readonly archives: Readonly<Partial<Record<HostArch, PinnedAsset>>>;
      readonly packages: readonly string[];
    }
  | { readonly kind: "interactive"; readonly via: "xcode-select" }
  | { readonly kind: "interactive"; readonly via: "terminal"; readonly scriptUrl: string }
  | { readonly kind: "manual"; readonly url: string; readonly via: string };

export type InstallSpecKind = InstallSpec["kind"];

export interface PlatformRecipe {
  readonly install: InstallSpec;
  /** On top of what the strategy itself needs (Homebrew for brew, Node.js for npm, …). */
  readonly dependsOn?: readonly ToolchainId[];
  /** This OS's own page, when the tool's general one is not where a person on it would go. */
  readonly docsUrl?: string;
}

export interface ToolDefinition {
  readonly id: ToolchainId;
  readonly label: string;
  /** How an installed copy reaches the agents' processes (`runtime-env.ts`); absent when something else already sees to it. */
  readonly runtime?: RuntimeExposure;
  /** Where a person reads about it; the fallback when a recipe degrades to manual. */
  readonly docsUrl: string;
  /** Installing it changes what the backend or the runner finds at its next start. */
  readonly affectsRuntime: boolean;
  /** Absent for an OS it does not apply to. */
  readonly recipes: Readonly<Partial<Record<ToolchainPlatform, PlatformRecipe>>>;
  readonly detect: Detector;
}

/** The machine as a recipe sees it when it decides how it would install. */
export interface HostFacts {
  readonly platform: ToolchainPlatform;
  readonly arch: string;
  readonly home: string;
  readonly env: Readonly<NodeJS.ProcessEnv>;
  /** A command on this machine right now. An installer asks again when it runs: an earlier step may have added it. */
  which(name: string): string | null;
}

/** One tool's install on this host, ready to run: the Strategy, bound. */
export interface Installer {
  readonly installKind: ToolchainInstallKind;
  readonly method: string;
  /** Display-only. */
  readonly command?: string;
  readonly url?: string;
  readonly dependsOn: readonly ToolchainId[];
  run(ctx: StepContext): Promise<void>;
}

export interface ExecOptions {
  env?: Readonly<Record<string, string>>;
  /** Written to stdin, which is then closed. */
  input?: string;
  cwd?: string;
  onLine?: (line: string) => void;
}

export interface DownloadOptions {
  sha1?: string;
  sha256?: string;
  onPercent?: (percent: number) => void;
}

export interface StepProgress {
  detail?: string;
  /** null clears it. */
  percent?: number | null;
  /** The step is waiting on the person (an installer window they finish). */
  waiting?: boolean;
}

export interface StepContext {
  readonly id: ToolchainId;
  readonly label: string;
  readonly signal: AbortSignal;
  readonly host: HostFacts;
  /** `userData/toolchain`: downloads and helper scripts. Exists. */
  readonly workDir: string;
  /** shell:false, its own process group, output to the step's log. Resolves with the exit code. */
  exec(command: string, args: readonly string[], opts?: ExecOptions): Promise<number>;
  download(url: string, dest: string, opts?: DownloadOptions): Promise<void>;
  progress(update: StepProgress): void;
  log(line: string): void;
  /** Detection for this step's tool, or for another one. */
  detect(id?: ToolchainId): Promise<Detection>;
  /** Rejects when the step is cancelled. */
  sleep(ms: number): Promise<void>;
}

/** A failure with a sentence meant for the person, shown as the step's error. */
export class InstallError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "InstallError";
  }
}

export class CancelledError extends Error {
  constructor() {
    super("Cancelled.");
    this.name = "CancelledError";
  }
}
