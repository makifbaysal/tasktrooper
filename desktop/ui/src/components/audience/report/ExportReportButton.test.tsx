import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@/hooks/useI18n";
import { report } from "@/test/audienceFixtures";
import { ExportReportButton } from "./ExportReportButton";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

afterEach(() => vi.restoreAllMocks());

describe("ExportReportButton", () => {
  it("builds one HTML file from the report and downloads it under a safe name", async () => {
    let blob: Blob | undefined;
    URL.createObjectURL = vi.fn((b: Blob) => {
      blob = b;
      return "blob:report";
    });
    URL.revokeObjectURL = vi.fn();
    const names: string[] = [];
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      names.push(this.download);
    });
    render(
      <I18nProvider>
        <ExportReportButton report={report({ kind: "ab_test" })} title="Would they pay?" audienceName="Freelance designers" />
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Export as HTML" }));
    expect(names).toEqual(["Would-they-pay-ab-test.html"]);
    expect(blob?.type).toBe("text/html;charset=utf-8");
    const html = await blob!.text();
    expect(html).toContain("Would they pay?");
    expect(html).toContain("Freelance designers");
  });

  it("opens the file in a window, or downloads it when the window is blocked", () => {
    URL.createObjectURL = vi.fn(() => "blob:report");
    URL.revokeObjectURL = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    render(
      <I18nProvider>
        <ExportReportButton report={report()} title="Would they pay?" />
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Open in browser" }));
    expect(open).toHaveBeenCalledOnce();
    expect(click).toHaveBeenCalledOnce();
  });
});
