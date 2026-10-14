import { describe, expect, it } from "vitest";
import type { ToolchainId } from "../../ipc/types.js";
import { PlanCycleError, planInstall, topologicalOrder, type PlanItem } from "./plan.js";

function item(id: ToolchainId, over: Partial<PlanItem> = {}): PlanItem {
  return { id, status: "missing", installKind: "auto", dependsOn: [], ...over };
}

/** A Mac with nothing installed, shaped like the real table's dependencies. */
const MAC: PlanItem[] = [
  item("homebrew", { installKind: "interactive" }),
  item("git", { dependsOn: ["homebrew"] }),
  item("node", { dependsOn: ["homebrew"] }),
  item("xcode", { installKind: "manual" }),
  item("xcode-clt", { installKind: "interactive" }),
  item("cocoapods", { dependsOn: ["homebrew"] }),
  item("jdk", { dependsOn: ["homebrew"] }),
  item("android-sdk", { dependsOn: ["jdk"] }),
  item("appium", { dependsOn: ["node"] }),
  item("appium-xcuitest", { dependsOn: ["appium"] }),
  item("appium-uiautomator2", { dependsOn: ["appium"] }),
];

const installed = (items: PlanItem[], ...ids: ToolchainId[]): PlanItem[] =>
  items.map((i) => (ids.includes(i.id) ? { ...i, status: "installed" as const } : i));

describe("planInstall", () => {
  it("pulls in missing dependencies transitively and puts them first", () => {
    expect(planInstall(["appium-uiautomator2"], MAC)).toEqual(["homebrew", "node", "appium", "appium-uiautomator2"]);
    expect(planInstall(["android-sdk"], MAC)).toEqual(["homebrew", "jdk", "android-sdk"]);
  });

  it("drops what is installed, requested or pulled in, and still walks past an installed dependency", () => {
    const items = installed(MAC, "homebrew", "node", "cocoapods");
    expect(planInstall(["cocoapods", "appium-xcuitest"], items)).toEqual(["appium", "appium-xcuitest"]);
  });

  it("orders by TOOLCHAIN_IDS where dependencies do not decide, whatever order was asked for", () => {
    expect(planInstall(["appium-xcuitest", "cocoapods", "git"], installed(MAC, "homebrew", "node", "appium"))).toEqual([
      "git",
      "cocoapods",
      "appium-xcuitest",
    ]);
  });

  it("folds repeats and the same dependency reached twice", () => {
    expect(planInstall(["appium-xcuitest", "appium-uiautomator2", "appium-xcuitest"], MAC)).toEqual([
      "homebrew",
      "node",
      "appium",
      "appium-xcuitest",
      "appium-uiautomator2",
    ]);
  });

  it("drops ids that do not apply to this OS (no item in the report)", () => {
    const windows: PlanItem[] = [item("git"), item("node"), item("appium", { dependsOn: ["node"] })];
    expect(planInstall(["xcode-clt", "appium-xcuitest", "git", "homebrew"], windows)).toEqual(["git"]);
  });

  it("drops manual tools, and keeps what needs one so its step can say what is missing", () => {
    expect(planInstall(["xcode"], MAC)).toEqual([]);
    const noWinget: PlanItem[] = [item("node", { installKind: "manual" }), item("appium", { dependsOn: ["node"] })];
    expect(planInstall(["appium"], noWinget)).toEqual(["appium"]);
  });

  it("plans nothing for nothing, and nothing when everything is there", () => {
    expect(planInstall([], MAC)).toEqual([]);
    expect(planInstall(["appium"], installed(MAC, "homebrew", "node", "appium"))).toEqual([]);
  });

  it("treats an unusable tool as one to install", () => {
    const items = installed(MAC, "homebrew").map((i) => (i.id === "node" ? { ...i, status: "unusable" as const } : i));
    expect(planInstall(["node"], items)).toEqual(["node"]);
  });
});

describe("topologicalOrder", () => {
  it("refuses a cycle rather than picking an order", () => {
    const deps: Partial<Record<ToolchainId, ToolchainId[]>> = { git: ["node"], node: ["git"] };
    expect(() => topologicalOrder(["git", "node"], (id) => deps[id] ?? [])).toThrow(PlanCycleError);
  });
});
