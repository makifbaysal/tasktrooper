import type { HostFacts, StepContext } from "../types.js";
import { InstallError } from "../types.js";
import { manualInstaller, type InstallerFactory } from "./shared.js";

/**
 * Linux packages, through the distribution's own package manager or snap,
 * with `pkexec` for the one root step: polkit shows the desktop's own
 * password prompt, naming the program it is for, so nothing here ever sees
 * or stores a password. No pkexec (a server install, a session without a
 * polkit agent) or no known package manager degrades to `manual` with the
 * command shown.
 *
 * Only packages from the configured repositories, never a file this app
 * downloaded: a file in the person's own directory installed as root is one
 * any process of theirs could swap between the download and the password
 * prompt.
 */

interface PackageManager {
  name: "apt-get" | "dnf";
  bin: string;
  /** What a person types without this app. */
  label: string;
}

export function linuxPackageManager(host: HostFacts): PackageManager | null {
  const apt = host.which("apt-get");
  if (apt) return { name: "apt-get", bin: apt, label: "apt" };
  const dnf = host.which("dnf");
  if (dnf) return { name: "dnf", bin: dnf, label: "dnf" };
  return null;
}

/**
 * pkexec starts apt-get with a clean environment and no terminal, so
 * debconf falls back to its defaults; the two dpkg options keep a changed
 * config file from turning into a question nobody can answer.
 */
const APT_OPTIONS = ["-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"];

/** 126: the person dismissed the password prompt. 127: polkit refused, or found no agent to ask with. */
async function asAdministrator(ctx: StepContext, args: readonly string[], what: string): Promise<void> {
  const pkexec = ctx.host.which("pkexec");
  if (!pkexec) throw new InstallError("pkexec was not found, so nothing can ask for the administrator password.");
  ctx.progress({ detail: `Running ${what}… your desktop asks for your password.` });
  let last = "";
  const code = await ctx.exec(pkexec, args, {
    onLine: (line) => {
      if (line.trim() !== "") last = line.trim();
    },
  });
  if (code === 126) throw new InstallError("The administrator password prompt was dismissed.");
  if (code === 127) throw new InstallError(`Not authorized to install as administrator${last === "" ? "." : `: ${last}`}`);
  if (code !== 0) throw new InstallError(`${what} exited with code ${code}${last === "" ? "." : `: ${last}`}`);
}

export const linuxPkg: InstallerFactory<"linux-pkg"> = (spec, tool, host) => {
  const pm = linuxPackageManager(host);
  if (!pm) return manualInstaller(tool.docsUrl, "Package manager");
  const packages = pm.name === "apt-get" ? spec.apt : spec.dnf;
  if (!host.which("pkexec")) return manualInstaller(tool.docsUrl, pm.label, `sudo ${pm.name} install -y ${packages.join(" ")}`);
  return {
    installKind: "auto",
    method: `${pm.label} (pkexec)`,
    command: `pkexec ${pm.name} install -y ${packages.join(" ")}`,
    url: tool.docsUrl,
    dependsOn: [],
    run: (ctx) => {
      const current = linuxPackageManager(ctx.host) ?? pm;
      const options = current.name === "apt-get" ? APT_OPTIONS : [];
      return asAdministrator(ctx, [current.bin, ...options, "install", "-y", ...packages], `${current.label} install ${packages.join(" ")}`);
    },
  };
};

export const snap: InstallerFactory<"snap"> = (spec, tool, host) => {
  const args = ["install", spec.name, ...(spec.classic ? ["--classic"] : [])];
  if (!host.which("snap") || !host.which("pkexec")) return manualInstaller(tool.docsUrl, "Download");
  return {
    installKind: "auto",
    method: "snap (pkexec)",
    command: `pkexec snap ${args.join(" ")}`,
    url: tool.docsUrl,
    dependsOn: [],
    run: (ctx) => {
      const snapBin = ctx.host.which("snap");
      if (!snapBin) throw new InstallError("snap is no longer on this computer.");
      return asAdministrator(ctx, [snapBin, ...args], `snap install ${spec.name}`);
    },
  };
};
