import type { ToolchainId } from "../../../ipc/types.js";
import type { HostFacts, InstallSpec, InstallSpecKind, Installer, ToolDefinition } from "../types.js";
import { androidSdk } from "./android-sdk.js";
import { brewCask, brewFormula } from "./brew.js";
import { bundled } from "./bundled.js";
import { binaryDownload, script } from "./download.js";
import { interactive } from "./interactive.js";
import { linuxPkg, snap } from "./linux.js";
import { appiumDriverInstall, npmGlobal } from "./npm.js";
import { manualInstaller, type InstallerFactory } from "./shared.js";
import { winget } from "./winget.js";

/**
 * One strategy per install method, chosen by the table entry's `kind`. Adding
 * a method is a new factory here and a new `kind` in `InstallSpec`; nothing
 * that runs installers changes.
 */
const FACTORIES: { [K in InstallSpecKind]: InstallerFactory<K> } = {
  brew: brewFormula,
  "brew-cask": brewCask,
  winget,
  "linux-pkg": linuxPkg,
  snap,
  bundled,
  "npm-global": npmGlobal,
  "appium-driver": appiumDriverInstall,
  script,
  "binary-download": binaryDownload,
  "android-sdk": androidSdk,
  interactive,
  manual: (spec) => manualInstaller(spec.url, spec.via),
};

function build<K extends InstallSpecKind>(spec: Extract<InstallSpec, { kind: K }>, tool: ToolDefinition, host: HostFacts): Installer {
  return (FACTORIES[spec.kind] as InstallerFactory<K>)(spec, tool, host);
}

/**
 * The installer for one tool on this host, or null when the tool does not
 * apply to this OS. Its `dependsOn` is the strategy's own (Homebrew for brew,
 * Node.js for npm) plus the table's.
 */
export function installerFor(tool: ToolDefinition, host: HostFacts): Installer | null {
  const recipe = tool.recipes[host.platform];
  if (!recipe) return null;
  const base = build(recipe.install, recipe.docsUrl ? { ...tool, docsUrl: recipe.docsUrl } : tool, host);
  const dependsOn: ToolchainId[] = [...new Set([...base.dependsOn, ...(recipe.dependsOn ?? [])])];
  return { ...base, dependsOn };
}
