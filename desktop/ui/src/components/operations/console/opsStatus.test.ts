import { describe, expect, it } from "vitest";
import type { Incident } from "@/api";
import { compareOpsStatus, deriveOpsHealth, healthSeverity, incidentEnvironment, type OpsStatus } from "@/components/operations/console/opsStatus";

const incident = (severity: Incident["severity"], status: Incident["status"] = "open") => ({ severity, status });

describe("deriveOpsHealth", () => {
  it("is critical for an open critical or high incident, whatever the health", () => {
    expect(deriveOpsHealth({ connected: true, health: "healthy", incidents: [incident("critical")] })).toEqual({
      status: "critical",
      reason: "incidentCritical",
      count: 1,
    });
    expect(deriveOpsHealth({ connected: false, incidents: [incident("high", "fixing")] }).status).toBe("critical");
  });

  it("is critical for a failing resource and a warning for a degraded one", () => {
    expect(deriveOpsHealth({ connected: true, health: "failed", incidents: [] })).toEqual({ status: "critical", reason: "resourceFailed" });
    expect(deriveOpsHealth({ connected: true, health: "degraded", incidents: [] })).toEqual({ status: "warning", reason: "resourceDegraded" });
  });

  it("warns for an open medium or low incident, and for errors in the last 24 h", () => {
    expect(deriveOpsHealth({ connected: true, health: "healthy", incidents: [incident("low", "triaging")] }).reason).toBe("incidentWarning");
    expect(deriveOpsHealth({ connected: true, health: "healthy", incidents: [], errors24h: 4 })).toEqual({
      status: "warning",
      reason: "errors",
      count: 4,
    });
  });

  it("is healthy when the resource says so and nothing else is wrong", () => {
    expect(deriveOpsHealth({ connected: true, health: "healthy", incidents: [] }).status).toBe("healthy");
    expect(deriveOpsHealth({ connected: true, health: "deploying", incidents: [], errors24h: 0 }).status).toBe("healthy");
  });

  it("is not reporting without an environment or with unknown health", () => {
    expect(deriveOpsHealth({ connected: false, incidents: [] })).toEqual({ status: "notReporting", reason: "notConnected" });
    expect(deriveOpsHealth({ connected: true, health: "unknown", incidents: [] })).toEqual({ status: "notReporting", reason: "noHealth" });
  });

  it("ignores incidents that are no longer open", () => {
    expect(deriveOpsHealth({ connected: true, health: "healthy", incidents: [incident("critical", "resolved"), incident("high", "ignored")] }).status).toBe(
      "healthy",
    );
  });
});

describe("healthSeverity", () => {
  it("is the one mapping of stored health to severity", () => {
    expect(healthSeverity("failed")).toBe("critical");
    expect(healthSeverity("degraded")).toBe("warning");
    expect(healthSeverity("healthy")).toBeUndefined();
    expect(healthSeverity(undefined)).toBeUndefined();
  });
});

describe("incidentEnvironment", () => {
  it("reads production only from empty, prod* and live", () => {
    expect(incidentEnvironment("")).toBe("production");
    expect(incidentEnvironment(undefined)).toBe("production");
    expect(incidentEnvironment("prod")).toBe("production");
    expect(incidentEnvironment("Production")).toBe("production");
    expect(incidentEnvironment("live")).toBe("production");
  });

  it("reads pre-production labels as staging, never production", () => {
    for (const label of ["preprod", "pre-prod", "uat", "qa", "test", "stage", "STG", "staging"]) {
      expect(incidentEnvironment(label), label).toBe("staging");
    }
    expect(incidentEnvironment("preview")).toBe("preview");
    expect(incidentEnvironment("dev")).toBe("development");
    expect(incidentEnvironment("local")).toBe("development");
  });

  it("reads prd and a production word with a region around it as production", () => {
    for (const label of ["prd", "PRD-eu", "eu-prod", "prod-eu", "production-eu", "us-east-1_production", "eu-live"]) {
      expect(incidentEnvironment(label), label).toBe("production");
    }
  });

  it("never reads a negated or pre-production label as production", () => {
    for (const label of ["eu-preprod", "eu-pre-prod", "eu-staging", "stg-eu"]) {
      expect(incidentEnvironment(label), label).toBe("staging");
    }
    for (const label of ["non-prod", "nonprod", "not-live"]) {
      expect(incidentEnvironment(label), label).toBeUndefined();
    }
    expect(incidentEnvironment("eu-dev")).toBe("development");
  });

  it("matches no environment for a label it does not know", () => {
    expect(incidentEnvironment("canary-eu")).toBeUndefined();
  });
});

describe("compareOpsStatus", () => {
  it("orders critical first and not reporting last", () => {
    const sorted = (["notReporting", "healthy", "critical", "warning"] as OpsStatus[]).slice().sort(compareOpsStatus);
    expect(sorted).toEqual(["critical", "warning", "healthy", "notReporting"]);
  });
});
