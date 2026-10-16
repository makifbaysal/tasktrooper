import { describe, expect, it } from "vitest";
import type { Agent } from "@/api";
import {
  DESKTOP_TOOLCHAIN_IDS,
  type ToolchainId,
  type ToolchainItem,
  type ToolchainReport,
} from "@/lib/desktop-bridge";
import {
  TOOLCHAIN_GROUPS,
  enabledTeam,
  groupReport,
  installedFor,
  manualBlockers,
  planExtras,
  preselectedIds,
  recommendFor,
  toolchainMethodLine,
  toolchainNeeds,
  toolchainReason,
} from "@/lib/toolchain";

const LABELS: Partial<Record<ToolchainId, string>> = {
  homebrew: "Homebrew",
  git: "Git",
  "claude-code": "Claude Code",
  node: "Node.js",
  jdk: "JDK 17",
  xcode: "Xcode",
};

function item(id: ToolchainId, overrides: Partial<ToolchainItem> = {}): ToolchainItem {
  return {
    id,
    label: LABELS[id] ?? id,
    status: "missing",
    installKind: "auto",
    method: "Homebrew",
    dependsOn: [],
    ...overrides,
  };
}

function report(items: ToolchainItem[], platform: ToolchainReport["platform"] = "darwin"): ToolchainReport {
  return { generatedAt: 1, platform, items };
}

/** Every id, missing, auto — except `xcode`, which is the App Store's. */
function fullDarwinReport(): ToolchainReport {
  return report(
    DESKTOP_TOOLCHAIN_IDS.map((id) => item(id, id === "xcode" ? { installKind: "manual", method: "App Store" } : {})),
  );
}

const ids = (entries: { id: ToolchainId }[]) => entries.map((entry) => entry.id);

describe("toolchainNeeds", () => {
  it("always recommends git and Claude Code, for every agent", () => {
    const needs = toolchainNeeds([]);
    expect([...needs.keys()]).toEqual(["git", "claude-code"]);
    expect(needs.get("git")).toEqual({ level: "recommended", reasons: [] });
  });

  it("maps a web team's agents to their tools, with the agents as reasons", () => {
    const needs = toolchainNeeds(["frontend-developer", "ui-designer", "backend-developer"]);
    expect(needs.get("node")).toEqual({ level: "recommended", reasons: ["frontend-developer", "ui-designer", "backend-developer"] });
    expect(needs.get("chrome")?.level).toBe("recommended");
    expect(needs.get("go")).toEqual({ level: "optional", reasons: ["backend-developer"] });
    for (const id of ["dotnet", "rust"] as const) expect(needs.get(id)).toEqual({ level: "optional", reasons: ["backend-developer"] });
    expect(needs.get("docker")?.level).toBe("optional");
    expect(needs.has("android-sdk")).toBe(false);
  });

  it("lets a recommendation win over another agent's optional need", () => {
    const needs = toolchainNeeds(["backend-developer", "mobile-developer"]);
    expect(needs.get("jdk")).toEqual({ level: "recommended", reasons: ["backend-developer", "mobile-developer"] });
    expect(needs.get("flutter")?.level).toBe("optional");
  });

  it("gives QA the mobile test stack only when a mobile developer is on the team", () => {
    const withoutMobile = toolchainNeeds(["qa-agent"]);
    expect(withoutMobile.get("chrome")?.level).toBe("recommended");
    expect(withoutMobile.has("appium")).toBe(false);
    expect(withoutMobile.has("android-sdk")).toBe(false);

    const withMobile = toolchainNeeds(["qa-agent", "mobile-developer"]);
    for (const id of ["appium", "appium-xcuitest", "appium-uiautomator2", "android-sdk"] as const) {
      expect(withMobile.get(id)?.level).toBe("recommended");
      expect(withMobile.get(id)?.reasons).toContain("qa-agent");
    }
  });

  it("keeps data, games, security and release needs at their levels", () => {
    const needs = toolchainNeeds(["data-scientist", "game-developer", "security-agent", "release-engineer"]);
    expect(needs.get("python")?.level).toBe("recommended");
    expect(needs.get("uv")).toEqual({ level: "recommended", reasons: ["data-scientist", "security-agent"] });
    expect(needs.get("duckdb")?.level).toBe("optional");
    expect(needs.get("godot")?.level).toBe("optional");
    expect(needs.get("osv-scanner")?.level).toBe("optional");
    expect(needs.get("docker")).toEqual({ level: "optional", reasons: ["data-scientist", "release-engineer"] });
  });

  it("ignores agents it has no tools for", () => {
    expect([...toolchainNeeds(["product-manager", "someone-custom"]).keys()]).toEqual(["git", "claude-code"]);
  });
});

describe("recommendFor", () => {
  it("lists only what the team needs and this computer lacks, in report order", () => {
    const r = report([
      item("homebrew"),
      item("git", { status: "installed", version: "2.45" }),
      item("claude-code"),
      item("node", { status: "unusable", detail: "node 14 is too old" }),
      item("chrome", { status: "installed" }),
      item("docker"),
    ]);
    const recs = recommendFor(["frontend-developer"], r);
    expect(ids(recs)).toEqual(["claude-code", "node"]);
    expect(recs[0].reasons).toEqual([]);
    expect(recs[1].reasons).toEqual(["frontend-developer"]);
  });

  it("never recommends what this OS does not have in its report", () => {
    const windows = report(
      DESKTOP_TOOLCHAIN_IDS.filter((id) => !["homebrew", "xcode", "xcode-clt", "cocoapods", "appium-xcuitest"].includes(id)).map(
        (id) => item(id, { method: "winget" }),
      ),
      "win32",
    );
    const recs = ids(recommendFor(["mobile-developer"], windows));
    expect(recs).not.toContain("xcode");
    expect(recs).not.toContain("xcode-clt");
    expect(recs).not.toContain("appium-xcuitest");
    expect(recs).toEqual(expect.arrayContaining(["jdk", "android-sdk", "appium-uiautomator2", "flutter"]));
  });

  it("preselects recommended tools the app can install, never optional or manual ones", () => {
    const recs = recommendFor(["mobile-developer"], fullDarwinReport());
    const byId = new Map(recs.map((entry) => [entry.id, entry]));
    expect(byId.get("xcode-clt")?.preselected).toBe(true);
    expect(byId.get("xcode")).toMatchObject({ level: "recommended", preselected: false });
    expect(byId.get("flutter")).toMatchObject({ level: "optional", preselected: false });
    expect(preselectedIds(recs)).not.toContain("xcode");
    expect(preselectedIds(recs)).toContain("android-sdk");
  });

  it("preselects interactive installs, which the shell can still start", () => {
    const recs = recommendFor([], report([item("git", { installKind: "interactive" })]));
    expect(recs[0].preselected).toBe(true);
  });

  it("splits off what is already installed", () => {
    const r = report([item("git", { status: "installed" }), item("claude-code"), item("node", { status: "installed" })]);
    expect(ids(installedFor(["frontend-developer"], r))).toEqual(["git", "node"]);
  });
});

describe("manual dependencies", () => {
  // No winget: Node.js can only be installed by hand, and Appium (an npm
  // package) and its drivers wait on it.
  const noWinget = report([
    item("node", { installKind: "manual", method: undefined, url: "https://nodejs.org" }),
    item("appium", { method: "npm", dependsOn: ["node"] }),
    item("appium-uiautomator2", { method: "appium", dependsOn: ["appium"] }),
    item("jdk", { method: "winget" }),
  ], "win32");

  it("finds the manual tool an item waits on, through its other dependencies", () => {
    const byId = new Map(noWinget.items.map((entry) => [entry.id, entry]));
    expect(manualBlockers(byId.get("appium")!, noWinget).map((entry) => entry.id)).toEqual(["node"]);
    expect(manualBlockers(byId.get("appium-uiautomator2")!, noWinget).map((entry) => entry.id)).toEqual(["node"]);
    expect(manualBlockers(byId.get("jdk")!, noWinget)).toEqual([]);
  });

  it("is not blocked once the manual dependency is installed", () => {
    const installed = report(noWinget.items.map((entry) => (entry.id === "node" ? { ...entry, status: "installed" as const } : entry)));
    expect(manualBlockers(installed.items[1], installed)).toEqual([]);
  });

  it("never preselects what waits on a manual tool", () => {
    const recs = recommendFor(["mobile-developer"], noWinget);
    const byId = new Map(recs.map((entry) => [entry.id, entry]));
    expect(byId.get("appium")).toMatchObject({ level: "recommended", installable: false, preselected: false });
    expect(byId.get("appium")?.blockedBy.map((entry) => entry.id)).toEqual(["node"]);
    expect(byId.get("node")).toMatchObject({ installable: false, preselected: false });
    expect(preselectedIds(recs)).toEqual(["jdk"]);
  });
});

describe("groupReport", () => {
  it("puts every id in exactly one group", () => {
    const grouped = TOOLCHAIN_GROUPS.flatMap((group) => group.ids);
    expect([...grouped].sort()).toEqual([...DESKTOP_TOOLCHAIN_IDS].sort());
  });

  it("groups the report's items and drops groups with nothing on this OS", () => {
    const groups = groupReport(report([item("git"), item("chrome"), item("docker"), item("homebrew")]));
    expect(groups.map((group) => group.id)).toEqual(["basics", "web", "infra"]);
    expect(groups[0].items.map((entry) => entry.id)).toEqual(["homebrew", "git"]);
  });

  it("gives the backend languages a group of their own", () => {
    const groups = groupReport(report([item("node"), item("go"), item("rust"), item("dotnet"), item("jdk"), item("docker")]));
    expect(groups.map((group) => group.id)).toEqual(["basics", "backend", "infra"]);
    expect(groups[1].items.map((entry) => entry.id)).toEqual(["go", "jdk", "dotnet", "rust"]);
  });
});

describe("planExtras", () => {
  it("names the dependencies the plan put in front of the selection", () => {
    const r = report([item("homebrew"), item("jdk"), item("android-sdk")]);
    expect(planExtras(["homebrew", "jdk", "android-sdk"], ["android-sdk"], r)).toEqual(["Homebrew", "JDK 17"]);
    expect(planExtras(["android-sdk"], ["android-sdk"], r)).toEqual([]);
  });

});

describe("labels", () => {
  const t = (key: string, params?: Record<string, string | number>) => `${key}${params ? JSON.stringify(params) : ""}`;

  it("names the agents that need a tool, or says every agent does", () => {
    expect(toolchainReason(t, [], {})).toBe("toolchain.reasons.everyone");
    expect(toolchainReason(t, ["qa-agent", "x"], { "qa-agent": "QA Agent" })).toBe(
      'toolchain.reasons.agents{"agents":"QA Agent, x"}',
    );
  });

  it("adds the install kind to the method only when it is not unattended", () => {
    expect(toolchainMethodLine(t, { method: "Homebrew", installKind: "auto" })).toBe('toolchain.method{"method":"Homebrew"}');
    expect(toolchainMethodLine(t, { method: "App Store", installKind: "manual" })).toBe(
      'toolchain.method{"method":"App Store"} · toolchain.kind.manual',
    );
  });
});

describe("enabledTeam", () => {
  function agent(id: string, name: string, enabled: boolean, slug?: string): Agent {
    return { id, name, enabled, catalog_slug: slug } as Agent;
  }

  it("reads enabled agents by catalog slug", () => {
    expect(
      enabledTeam([
        agent("1", "Mobile Developer", true, "mobile-developer"),
        agent("2", "QA Agent", false, "qa-agent"),
        agent("3", "Hand-made", true),
      ]),
    ).toEqual([{ slug: "mobile-developer", name: "Mobile Developer" }]);
  });

  it("falls back to names on a server that predates catalog slugs", () => {
    const team = enabledTeam([agent("1", "Mobile Developer", true), agent("2", "QA Agent", true), agent("3", "Data Scientist", false)]);
    expect(team.map((member) => member.slug).sort()).toEqual(["mobile-developer", "qa-agent"]);
  });
});
