import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { visualSrcdoc } from "@/components/markdown/VisualFrame";
import { I18nProvider } from "@/hooks/useI18n";

function renderMarkdown(content: string) {
  return render(
    <I18nProvider>
      <MarkdownContent content={content} />
    </I18nProvider>,
  );
}

describe("MarkdownContent", () => {
  it("does not print HTML comments as text", () => {
    const { container } = renderMarkdown(
      "<!-- tt:order -->\n**Release order**\n\n- Built after: D-2\n<!-- /tt:order -->",
    );
    expect(container.textContent).not.toContain("tt:order");
    expect(container.textContent).not.toContain("<!--");
    expect(screen.getByText("Release order")).toBeInTheDocument();
  });
});

describe("MarkdownContent visual blocks", () => {
  it("renders a ```visual block as a sandboxed frame, not as source", () => {
    const md = "Ekranda çizdim.\n\n```visual\n<svg viewBox=\"0 0 10 10\"><circle r=\"4\"/></svg>\n```\n\n```ts\nconst x = 1;\n```";
    const { container } = render(
      <I18nProvider>
        <MarkdownContent content={md} />
      </I18nProvider>,
    );
    const frames = container.querySelectorAll("iframe");
    expect(frames).toHaveLength(1);
    expect(frames[0].getAttribute("sandbox")).toBe("allow-scripts");
    const doc = frames[0].getAttribute("srcdoc") ?? "";
    expect(doc).toContain("<circle");
    expect(doc).toContain("default-src 'none'");
    expect(container.textContent).not.toContain("<circle");
    // An ordinary code block is still code.
    expect(container.querySelector("pre code")?.textContent).toContain("const x = 1;");
  });
});

describe("visualSrcdoc", () => {
  it("blocks the agent's scripts and network loads with its CSP", () => {
    const doc = visualSrcdoc("<div>hi</div><script>alert(1)</script>", "n0nce", "f1");
    expect(doc).toContain("script-src 'nonce-n0nce'");
    expect(doc).toContain("img-src data:");
    expect(doc).not.toContain("img-src data: https:");
  });
});
