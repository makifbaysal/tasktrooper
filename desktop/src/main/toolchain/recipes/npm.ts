import { accessSync, constants, existsSync } from "node:fs";
import path from "node:path";
import type { StepContext } from "../types.js";
import { execOrFail, requireCommand, type InstallerFactory } from "./shared.js";

function writable(dir: string): boolean {
  try {
    accessSync(dir, constants.W_OK);
    return true;
  } catch {
    return false;
  }
}

/**
 * A distribution's or nodejs.org's own Node keeps its global prefix under
 * /usr or /usr/local, which only root can write — `npm install -g` there fails
 * with EACCES. Rather than run npm as root, such an install goes to
 * `~/.local`, whose `bin` the preflight already searches.
 */
async function prefixArgs(ctx: StepContext, npm: string): Promise<string[]> {
  if (ctx.host.platform === "win32") return [];
  let prefix = "";
  const code = await ctx.exec(npm, ["prefix", "-g"], {
    onLine: (line) => {
      if (line.trim().startsWith("/")) prefix = line.trim();
    },
  });
  if (code !== 0 || prefix === "") return [];
  const modules = path.join(prefix, "lib", "node_modules");
  if (existsSync(modules) ? writable(modules) : writable(prefix)) return [];
  const local = path.join(ctx.host.home, ".local");
  ctx.log(`${prefix} is not writable without administrator rights; installing under ${local} instead.`);
  return ["--prefix", local];
}

export const npmGlobal: InstallerFactory<"npm-global"> = (spec, tool) => ({
  installKind: "auto",
  method: "npm",
  command: `npm install -g ${spec.pkg}`,
  url: tool.docsUrl,
  dependsOn: ["node"],
  run: async (ctx) => {
    const npm = requireCommand(ctx, "npm", "Node.js");
    const prefix = await prefixArgs(ctx, npm);
    ctx.progress({ detail: `Running npm install -g ${spec.pkg}…` });
    await execOrFail(ctx, npm, ["install", "-g", ...prefix, spec.pkg], `npm install -g ${spec.pkg}`);
  },
});

export const appiumDriverInstall: InstallerFactory<"appium-driver"> = (spec, tool) => ({
  installKind: "auto",
  method: "Appium",
  command: `appium driver install ${spec.driver}`,
  url: tool.docsUrl,
  dependsOn: ["appium"],
  run: async (ctx) => {
    const appium = requireCommand(ctx, "appium", "Appium");
    ctx.progress({ detail: `Running appium driver install ${spec.driver}…` });
    await execOrFail(ctx, appium, ["driver", "install", spec.driver], `appium driver install ${spec.driver}`);
  },
});
