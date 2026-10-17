import "@testing-library/jest-dom/vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import type { OpsGolden } from "@/api";
import { indexGolden } from "@/components/operations/console/goldenMetrics";
import type { ExplorerRow } from "@/components/operations/console/opsEntities";
import { viewFor } from "@/components/operations/console/roleViews";
import { EntityTable } from "@/components/operations/explorer/EntityTable";
import { I18nProvider } from "@/hooks/useI18n";

const NOW = Date.parse("2026-10-17T10:00:00Z");

function worker(id: string, name: string): ExplorerRow {
  return {
    key: id,
    kind: "entity",
    entityId: id,
    name,
    role: "worker",
    type: "workers",
    repositoryId: "r-jobs",
    projectIds: [],
    incidents: [],
    health: { status: "healthy", reason: "healthy" },
  } as ExplorerRow;
}

describe("Workers explorer golden columns", () => {
  it("shows a backlog the server flags live as a value with no sparkline, and a history source's with one", () => {
    const golden: OpsGolden = {
      generated_at: "",
      components: [
        { component_id: "c-live", signals: [{ signal: "queue.backlog", unit: "messages", bound: true, live: true, value: 42, previous: null, spark: [[NOW, 42]] }] },
        { component_id: "c-one", signals: [{ signal: "queue.backlog", unit: "messages", bound: true, value: 9, previous: null, spark: [[NOW, 9]] }] },
        {
          component_id: "c-hist",
          signals: [{ signal: "queue.backlog", unit: "messages", bound: true, value: 7, previous: 5, spark: [[NOW - 60_000, 5], [NOW, 7]] }],
        },
      ],
    };
    render(
      <MemoryRouter>
        <I18nProvider>
          <EntityTable
            rows={[worker("c-live", "mailer"), worker("c-hist", "billing"), worker("c-one", "invoices")]}
            golden={viewFor("worker").golden}
            goldenValues={indexGolden(golden)}
            goldenRead={{ data: golden, loading: false }}
          />
        </I18nProvider>
      </MemoryRouter>,
    );
    const row = (name: string) => screen.getByRole("link", { name }).closest("tr") as HTMLElement;
    const live = within(row("mailer")).getByText("42 messages").parentElement as HTMLElement;
    expect(live).toHaveAttribute("title", "Live value: its data source keeps no history");
    expect(live.querySelector("svg")).toBeNull();
    const hist = within(row("billing")).getByText("7 messages").parentElement as HTMLElement;
    expect(hist).not.toHaveAttribute("title");
    expect(hist.querySelector("svg")).not.toBeNull();
    const one = within(row("invoices")).getByText("9 messages").parentElement as HTMLElement;
    expect(one).not.toHaveAttribute("title");
  });
});
