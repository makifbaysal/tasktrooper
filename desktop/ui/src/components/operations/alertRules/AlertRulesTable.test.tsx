import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { AlertRule } from "@/api";
import { AlertRulesTable } from "@/components/operations/alertRules/AlertRulesTable";
import { OpsProvider } from "@/components/operations/shell/OpsContext";
import { I18nProvider } from "@/hooks/useI18n";

function rule(overrides: Partial<AlertRule>): AlertRule {
  return {
    id: "r-1",
    name: "Latency p95 above 800 ms",
    component_id: "c-api",
    environment: "production",
    signal: "http.latency",
    condition: { op: "gt", threshold: 800, for_seconds: 300, window_seconds: 300 },
    severity: "high",
    channels: ["desktop"],
    open_incident: true,
    enabled: false,
    state: "ok",
    state_since: "",
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

describe("AlertRulesTable", () => {
  it("badges a rule the release engineer proposed, and only that one", () => {
    render(
      <MemoryRouter>
        <I18nProvider>
          <OpsProvider>
            <AlertRulesTable
              rules={[rule({ created_by: "ai" }), rule({ id: "r-2", name: "Error ratio above 2 %", created_by: "human" }), rule({ id: "r-3", name: "Old rule" })]}
              entityOf={() => undefined}
              unitOf={() => "ms"}
              detailHref={(r) => `?rule=${r.id}`}
              onToggle={() => {}}
              busy={new Set()}
            />
          </OpsProvider>
        </I18nProvider>
      </MemoryRouter>,
    );
    const table = screen.getByRole("table", { name: "Alert rules" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(within(rows[0]).getByText("AI").closest("[title]")).toHaveAttribute(
      "title",
      "Proposed by the release engineer and saved switched off; turn it on once it looks right",
    );
    expect(within(rows[1]).queryByText("AI")).not.toBeInTheDocument();
    expect(within(rows[2]).queryByText("AI")).not.toBeInTheDocument();
  });
});
