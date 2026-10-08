import "@testing-library/jest-dom/vitest";
import { act, render, waitFor } from "@testing-library/react";
import { createRef } from "react";
import { describe, expect, it, vi } from "vitest";
import type { TaskDocument } from "@/api";
import {
  AnalysisFrame,
  type AnalysisFrameHandle,
  type FrameAnnotation,
} from "@/components/board/analysis/AnalysisFrame";
import { I18nProvider } from "@/hooks/useI18n";

const htmlDoc: Pick<TaskDocument, "id" | "content" | "format"> = {
  id: "doc-1",
  format: "html",
  content: "<html><head><script>steal()</script></head><body><h1>analiz: plan</h1><p>Use the cache.</p></body></html>",
};

const annotation: FrameAnnotation = { id: "a1", quote: "the cache", prefix: "Use ", suffix: ".", status: "open" };

function renderFrame(overrides: Partial<Parameters<typeof AnalysisFrame>[0]> = {}) {
  const props = {
    document: htmlDoc,
    annotations: [annotation],
    activeId: null,
    theme: "light" as const,
    onSelection: vi.fn(),
    onFocusAnnotation: vi.fn(),
    onAnchored: vi.fn(),
    ...overrides,
  };
  const ref = createRef<AnalysisFrameHandle>();
  const utils = render(
    <I18nProvider>
      <AnalysisFrame ref={ref} {...props} />
    </I18nProvider>,
  );
  return { ...utils, props, ref };
}

async function frameOf(container: HTMLElement): Promise<HTMLIFrameElement> {
  return waitFor(() => {
    const iframe = container.querySelector("iframe");
    if (!iframe) throw new Error("no iframe yet");
    return iframe;
  });
}

function fromFrame(data: unknown, source: MessageEventSource | null) {
  act(() => {
    window.dispatchEvent(new MessageEvent("message", { data, source }));
  });
}

describe("AnalysisFrame", () => {
  it("renders the document in a script-only sandbox with the CSP injected", async () => {
    const { container } = renderFrame();
    const iframe = await frameOf(container);

    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts");
    expect(iframe.getAttribute("sandbox")).not.toContain("allow-same-origin");
    const srcdoc = iframe.getAttribute("srcdoc")!;
    const nonce = /script-src 'nonce-([0-9a-f]{32})'/.exec(srcdoc)?.[1];
    expect(nonce).toBeDefined();
    expect(srcdoc).toContain(`<script nonce="${nonce}">`);
    expect(srcdoc).toContain("<script>steal()</script>");
    expect(srcdoc).toContain("<h1>analiz: plan</h1>");
  });

  it("acts on messages from its own frame and ignores every other source", async () => {
    const { container, props } = renderFrame();
    const iframe = await frameOf(container);
    const selection = { type: "tt:selection", quote: "the cache", prefix: "Use ", suffix: "." };

    fromFrame(selection, window);
    fromFrame(selection, null);
    expect(props.onSelection).not.toHaveBeenCalled();

    fromFrame(selection, iframe.contentWindow);
    expect(props.onSelection).toHaveBeenCalledWith({ quote: "the cache", prefix: "Use ", suffix: "." });

    fromFrame({ type: "tt:focus", id: "a1" }, iframe.contentWindow);
    expect(props.onFocusAnnotation).toHaveBeenCalledWith("a1");

    fromFrame({ type: "tt:anchored", results: [{ id: "a1", found: false }] }, iframe.contentWindow);
    expect(props.onAnchored).toHaveBeenCalledWith({ a1: false });

    fromFrame({ type: "tt:selection", quote: 42 }, iframe.contentWindow);
    expect(props.onSelection).toHaveBeenCalledTimes(1);
  });

  it("sends only the annotations to draw when the frame is ready, and scroll requests", async () => {
    const { container, ref } = renderFrame({ activeId: "a1" });
    const iframe = await frameOf(container);
    const post = vi.spyOn(iframe.contentWindow!, "postMessage");

    fromFrame({ type: "tt:ready" }, iframe.contentWindow);
    expect(post).toHaveBeenCalledWith({ type: "tt:annotations", items: [{ ...annotation, active: true }] }, "*");

    act(() => ref.current!.scrollTo("a1"));
    expect(post).toHaveBeenLastCalledWith({ type: "tt:scrollTo", id: "a1" }, "*");
  });

  it("posts the Open questions section's translated labels and items, and reports an answer typed in the frame", async () => {
    const onAnswer = vi.fn();
    const question = {
      id: "q1",
      key: "Q1",
      kind: "technical" as const,
      blocking: true,
      prompt: "Which cache?",
      recommendedAnswer: "",
      answer: "",
      status: "open" as const,
      editable: true,
    };
    const { container } = renderFrame({ questions: [question], onAnswer });
    const iframe = await frameOf(container);
    const post = vi.spyOn(iframe.contentWindow!, "postMessage");

    fromFrame({ type: "tt:ready" }, iframe.contentWindow);
    expect(post).toHaveBeenCalledWith(
      expect.objectContaining({ type: "tt:questions", items: [question] }),
      "*",
    );
    const [[sent]] = post.mock.calls.filter(([m]) => (m as { type: string }).type === "tt:questions");
    expect((sent as { labels: { heading: string } }).labels.heading).toBe("Open questions");

    fromFrame({ type: "tt:answer", id: "q1", text: "Redis" }, iframe.contentWindow);
    expect(onAnswer).toHaveBeenCalledWith("q1", "Redis");
  });

  it("renders a markdown document to HTML inside the same sandbox", async () => {
    const { container } = renderFrame({
      document: { id: "doc-2", content: "# spec: old\n\nPlain **markdown**.", format: undefined },
    });
    const iframe = await frameOf(container);
    const srcdoc = iframe.getAttribute("srcdoc")!;

    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts");
    expect(srcdoc).toContain("<h1>spec: old</h1>");
    expect(srcdoc).toContain("<strong>markdown</strong>");
    expect(srcdoc).toContain("Content-Security-Policy");
  });

  it("shows an HTML canvas with its zoom bar, which drives and mirrors the frame's zoom", async () => {
    const { container, getByRole } = renderFrame({ canvas: true });
    const iframe = await frameOf(container);
    expect(iframe.getAttribute("srcdoc")).toMatch(/<html[^>]*\sdata-tt-canvas/);
    const post = vi.spyOn(iframe.contentWindow!, "postMessage");

    act(() => getByRole("button", { name: "Zoom in" }).click());
    expect(post).toHaveBeenLastCalledWith({ type: "tt:zoom", action: "in" }, "*");
    act(() => getByRole("button", { name: "Fit to width" }).click());
    expect(post).toHaveBeenLastCalledWith({ type: "tt:zoom", action: "fit" }, "*");

    fromFrame({ type: "tt:zoom", zoom: 0.5 }, iframe.contentWindow);
    expect(getByRole("button", { name: "Zoom to 100%" })).toHaveTextContent("50%");
  });

  it("keeps a markdown document out of canvas mode", async () => {
    const { container, queryByRole } = renderFrame({
      canvas: true,
      document: { id: "doc-3", content: "# design review: list", format: "markdown" },
    });
    const iframe = await frameOf(container);
    expect(iframe.getAttribute("srcdoc")).not.toMatch(/<html[^>]*\sdata-tt-canvas/);
    expect(queryByRole("toolbar")).toBeNull();
  });
});
