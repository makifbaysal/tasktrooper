import { definitionOf } from "../registry.js";
import { manualInstaller, type InstallerFactory } from "./shared.js";

/**
 * A tool that arrives with another one — git with Apple's command line tools.
 * Its step runs nothing: the tool it comes with is its dependency, so it was
 * installed by the step before, and detection afterwards says whether it
 * really came along. It takes that tool's kind, so nobody is shown a second
 * installer that does not exist.
 */
export const bundled: InstallerFactory<"bundled"> = (spec, tool, host) => {
  const provider = definitionOf(spec.with);
  const providerKind = provider.recipes[host.platform]?.install.kind;
  if (providerKind === undefined || providerKind === "manual") return manualInstaller(tool.docsUrl, provider.label);
  return {
    installKind: providerKind === "interactive" ? "interactive" : "auto",
    method: provider.label,
    command: `comes with ${provider.label}`,
    url: tool.docsUrl,
    dependsOn: [spec.with],
    run: (ctx) => {
      ctx.progress({ detail: `Comes with ${provider.label}; checking it is there…` });
      return Promise.resolve();
    },
  };
};
