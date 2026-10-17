import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import { ZERO_UUID, type OpsLogEntry } from "@/api";
import { LogTable } from "@/components/operations/logs/LogTable";
import type { OpsEntity } from "@/components/operations/shell/useOpsEntities";
import { I18nProvider } from "@/hooks/useI18n";

const api: OpsEntity = { id: "c-api", name: "api", role: "backend", repositoryId: "r", repositoryName: "shop", projectIds: [], environments: [] };

function entry(overrides: Partial<OpsLogEntry>): OpsLogEntry {
  return {
    timestamp: "2026-10-17T10:00:00.123Z",
    severity: "error",
    message: "payment failed",
    component_id: "c-api",
    environment_id: ZERO_UUID,
    data_source_id: "ds-loki",
    ...overrides,
  };
}

type TraceHref = (id: string, entity: OpsEntity | undefined) => string | undefined;

function renderTable(entries: OpsLogEntry[], traceHref = vi.fn<TraceHref>((id) => `/operations/entity/c-api/traces?trace=${id}`)) {
  render(
    <MemoryRouter>
      <I18nProvider>
        <LogTable entries={entries} environments={new Map()} entities={new Map([["c-api", api]])} showEntity traceHref={traceHref} />
      </I18nProvider>
    </MemoryRouter>,
  );
  return traceHref;
}

describe("LogTable trace link", () => {
  it("opens a line's trace on its entity's Traces", () => {
    const traceHref = renderTable([entry({ trace_id: "4bf92f3577b34da6a3ce929d0e0e4736" })]);
    fireEvent.click(screen.getByRole("button", { name: "Show details" }));
    expect(screen.getByRole("link", { name: "Open trace" })).toHaveAttribute(
      "href",
      "/operations/entity/c-api/traces?trace=4bf92f3577b34da6a3ce929d0e0e4736",
    );
    expect(traceHref).toHaveBeenCalledWith("4bf92f3577b34da6a3ce929d0e0e4736", api);
  });

  it("offers no link where the entity has no Traces, or the line no trace id", () => {
    renderTable([entry({ trace_id: "abc" }), entry({ message: "no trace", timestamp: "2026-10-17T10:00:01Z" })], vi.fn<TraceHref>(() => undefined));
    for (const button of screen.getAllByRole("button", { name: "Show details" })) fireEvent.click(button);
    expect(screen.queryByRole("link", { name: "Open trace" })).not.toBeInTheDocument();
    expect(screen.getByText("abc")).toBeInTheDocument();
  });
});
