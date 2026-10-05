import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { EmbeddingGroupDetail } from "@/components/rag/EmbeddingGroupDetail";

function renderDetail(onClose = vi.fn()) {
  render(
    <EmbeddingGroupDetail
      title="auth · session"
      subtitle="internal/auth"
      color="#123456"
      count={42}
      share="12%"
      files={[
        { path: "internal/auth/session.go", count: 9 },
        { path: "internal/auth/token.go", count: 4 },
      ]}
      samples={[{ path: "internal/auth/session.go", symbol: "NewSession", snippet: "func NewSession() {}" }]}
      onClose={onClose}
      labels={{
        files: "Files with the most chunks",
        samples: "Sample chunks",
        close: "Close details",
        chunks: (n) => `${n} chunks`,
      }}
    />,
  );
  return onClose;
}

describe("EmbeddingGroupDetail", () => {
  it("renders the group, its files and its samples", () => {
    renderDetail();
    expect(screen.getByText("auth · session")).toBeInTheDocument();
    expect(screen.getByText("12%")).toBeInTheDocument();
    expect(screen.getByText("42 chunks")).toBeInTheDocument();
    expect(screen.getByText("internal/auth/token.go")).toBeInTheDocument();
    expect(screen.getByText("9")).toBeInTheDocument();
    expect(screen.getByText("NewSession")).toBeInTheDocument();
    expect(screen.getByText("func NewSession() {}")).toBeInTheDocument();
  });

  it("closes", () => {
    const onClose = renderDetail();
    fireEvent.click(screen.getByRole("button", { name: "Close details" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
