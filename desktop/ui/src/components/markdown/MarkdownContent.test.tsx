import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
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
