import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  EmbeddingMapSummary,
  type EmbeddingMapSummaryProps,
} from "@/components/rag/EmbeddingMapSummary";
import { I18nProvider } from "@/hooks/useI18n";
import { CHUNK_KINDS, type ChunkKind, type KindShare } from "@/lib/embeddingMapTopics";

const kindColors = Object.fromEntries(CHUNK_KINDS.map((k, i) => [k, `#00000${i}`])) as Record<
  ChunkKind,
  string
>;

function composition(counts: Partial<Record<ChunkKind, number>>): KindShare[] {
  const total = Object.values(counts).reduce((a, b) => a + b, 0);
  return CHUNK_KINDS.map((kind) => ({
    kind,
    count: counts[kind] ?? 0,
    share: total ? (counts[kind] ?? 0) / total : 0,
  }));
}

function renderSummary(overrides: Partial<EmbeddingMapSummaryProps> = {}) {
  const props: EmbeddingMapSummaryProps = {
    source: "code",
    totalChunks: 1200,
    fileCount: 80,
    topicCount: 6,
    clusteredShare: 0.9,
    indexedAt: "2026-01-01T00:00:00Z",
    branch: "main",
    embeddingModel: "demo-model",
    composition: composition({ source: 80, mock: 10, generated: 10 }),
    kindColors,
    stale: false,
    ...overrides,
  };
  return render(
    <I18nProvider>
      <EmbeddingMapSummary {...props} />
    </I18nProvider>,
  );
}

describe("EmbeddingMapSummary", () => {
  it("warns about mocks and generated code and offers the kind view", () => {
    const onColorByKind = vi.fn();
    renderSummary({ onColorByKind });
    expect(screen.getByText(/Mocks and generated code: 20%/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Color by kind" }));
    expect(onColorByKind).toHaveBeenCalledTimes(1);
  });

  it("offers a re-index action and disables it while one is starting", () => {
    const onReindex = vi.fn();
    const { unmount } = renderSummary({ onReindex });
    fireEvent.click(screen.getByRole("button", { name: "Re-index" }));
    expect(onReindex).toHaveBeenCalledTimes(1);
    unmount();
    renderSummary({ onReindex, reindexing: true });
    expect(screen.getByRole("button", { name: "Re-index" })).toBeDisabled();
  });

  it("stays quiet when mocks and generated code are a small share", () => {
    renderSummary({ composition: composition({ source: 95, mock: 3, generated: 2 }) });
    expect(screen.queryByText(/Mocks and generated code/)).not.toBeInTheDocument();
  });

  it("omits the composition and indexing tile for the files source", () => {
    renderSummary({ source: "files", composition: null });
    expect(screen.queryByText("What the index holds")).not.toBeInTheDocument();
    expect(screen.queryByText("Last indexed")).not.toBeInTheDocument();
    expect(screen.getByText("Indexed chunks")).toBeInTheDocument();
  });

  it("shows the stale-index notice", () => {
    renderSummary({ stale: true, staleDetail: "model changed" });
    expect(
      screen.getByText("This index was built with a different embedding model"),
    ).toBeInTheDocument();
    expect(screen.getByText("model changed")).toBeInTheDocument();
  });
});
