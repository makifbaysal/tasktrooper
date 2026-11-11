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
    expect(group?.items.length).toBe(9);
    expect(group?.items.every((i) => i.to.startsWith("/operations/telemetry/"))).toBe(true);
  });
});
