import { describe, expect, it } from "vitest";
import { OPS_NAV, activeNavId, navGroupLabelKey, navGroupOf, navItemLabelKey } from "@/components/operations/shell/opsNavigation";

describe("telemetry navigation", () => {
  it("lights the telemetry item for its list and detail pages", () => {
    expect(activeNavId("/operations/telemetry/services")).toBe("telServices");
    expect(activeNavId("/operations/telemetry/services/checkout")).toBe("telServices");
    expect(activeNavId("/operations/telemetry/traces/abc")).toBe("telTraces");
    expect(activeNavId("/operations/telemetry/slo")).toBe("telSlo");
  });

  it("leaves the older items alone", () => {
    expect(activeNavId("/operations/logs")).toBe("logs");
    expect(activeNavId("/operations/issues")).toBe("issues");
  });

  it("reads the telemetry labels from its own dictionary", () => {
    expect(navItemLabelKey("telLogs")).toBe("telemetryHub.nav.items.telLogs");
    expect(navItemLabelKey("logs")).toBe("operations.shell.items.logs");
    expect(navGroupLabelKey(navGroupOf("telDeploys"))).toBe("telemetryHub.nav.group");
    expect(navGroupLabelKey("monitor")).toBe("operations.shell.groups.monitor");
  });

  it("gives every telemetry item a route under /operations/telemetry", () => {
    const group = OPS_NAV.find((g) => g.id === "telemetry");
    expect(group?.items.length).toBe(11);
    const telemetry = OPS_NAV.filter((g) => g.id === "telemetry" || g.id.startsWith("tel")).flatMap((g) => g.items);
    expect(telemetry.length).toBe(21);
    expect(telemetry.every((i) => i.id.startsWith("tel") && i.to.startsWith("/operations/telemetry/"))).toBe(true);
  });

  it("lights the infrastructure, apps and performance items for their detail pages", () => {
    expect(activeNavId("/operations/telemetry/overview")).toBe("telOverview");
    expect(activeNavId("/operations/telemetry/infra/hosts/web-1")).toBe("telHosts");
    expect(activeNavId("/operations/telemetry/infra/kubernetes/pods/u1")).toBe("telKubernetes");
    expect(activeNavId("/operations/telemetry/apps/shop-web")).toBe("telApps");
    expect(activeNavId("/operations/telemetry/load-tests/run-1")).toBe("telLoadTests");
    expect(navGroupLabelKey(navGroupOf("telHosts"))).toBe("telemetryHub.nav.groups.telInfra");
    expect(navGroupLabelKey(navGroupOf("telLlm"))).toBe("telemetryHub.nav.groups.telPerformance");
  });
});
