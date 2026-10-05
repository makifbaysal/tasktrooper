import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  EmbeddingMapSearchSummary,
  type EmbeddingMapSearchSummaryProps,
} from "@/components/rag/EmbeddingMapSearchSummary";
import { I18nProvider } from "@/hooks/useI18n";

function renderSummary(overrides: Partial<EmbeddingMapSearchSummaryProps> = {}) {
  const props: EmbeddingMapSearchSummaryProps = {
    query: "auth flow",
    total: 8,
    placed: 8,
    groups: [
      { id: "a", label: "auth · session", color: "#111111", count: 5 },
      { id: "b", label: "router", color: "#222222", count: 3 },
    ],
    locating: false,
    onClear: vi.fn(),
    ...overrides,
  };
  render(
    <I18nProvider>
      <EmbeddingMapSearchSummary {...props} />
    </I18nProvider>,
  );
  return props;
}

describe("EmbeddingMapSearchSummary", () => {
  it("lists the groups that hold hits with their counts", () => {
    renderSummary();
    expect(screen.getByText("Search: “auth flow”")).toBeInTheDocument();
    expect(screen.getByText("auth · session")).toBeInTheDocument();
    expect(screen.getByText("5 hits")).toBeInTheDocument();
    expect(screen.getByText("3 hits")).toBeInTheDocument();
    expect(screen.queryByText(/could not be placed/)).not.toBeInTheDocument();
  });

  it("calls onClear", () => {
    const props = renderSummary();
    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
    expect(props.onClear).toHaveBeenCalledTimes(1);
  });

  it("notes results that could not be placed", () => {
    renderSummary({ placed: 6 });
    expect(screen.getByText("2 results could not be placed.")).toBeInTheDocument();
  });

  it("shows progress instead of groups while locating", () => {
    renderSummary({ locating: true });
    expect(screen.getByText("Placing results on the map…")).toBeInTheDocument();
    expect(screen.queryByText("5 hits")).not.toBeInTheDocument();
  });
});
