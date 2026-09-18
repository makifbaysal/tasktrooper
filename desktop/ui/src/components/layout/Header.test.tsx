import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Header } from "@/components/layout/Header";
import { I18nProvider } from "@/hooks/useI18n";
import { ThemeProvider } from "@/hooks/useTheme";
import type { TaskTrooperDesktopHost } from "@/lib/desktop-bridge";

function withDesktopHost() {
  window.__tasktrooperDesktop = { runner: {} } as unknown as TaskTrooperDesktopHost;
}

function renderHeader(props: Parameters<typeof Header>[0] = {}) {
  return render(
    <I18nProvider>
      <ThemeProvider>
        <Header {...props} />
      </ThemeProvider>
    </I18nProvider>,
  );
}

const LOGO_SELECTOR = 'svg[viewBox="0 0 100 130"]';

describe("Header logo alignment", () => {
  afterEach(() => {
    delete window.__tasktrooperDesktop;
  });

  it("shifts the logo left by the sidebar's expanded nav inset when the sidebar is expanded", () => {
    const { container } = renderHeader({ sidebarCollapsed: false });
    const logo = container.querySelector(LOGO_SELECTOR);
    const brand = logo?.closest("div")?.parentElement;
    expect(brand?.className).toContain("-ml-1");
  });

  it("shifts the logo by the sidebar's collapsed nav inset when the sidebar is collapsed", () => {
    const { container } = renderHeader({ sidebarCollapsed: true });
    const logo = container.querySelector(LOGO_SELECTOR);
    const brandBadge = logo?.closest("div");
    expect(brandBadge?.className).toContain("ml-0.5");
  });

  it("keeps the logo inside the header element, not moved elsewhere", () => {
    const { container } = renderHeader();
    const header = container.querySelector("header");
    expect(header?.querySelector(LOGO_SELECTOR)).not.toBeNull();
  });

  it("keeps the mobile menu button visible and clickable at narrow viewports", () => {
    const onMenuClick = vi.fn();
    renderHeader({ onMenuClick });
    const button = screen.getByRole("button", { name: "" });
    expect(button.className).toContain("lg:hidden");
    button.click();
    expect(onMenuClick).toHaveBeenCalledTimes(1);
  });

  it("cancels the shell's pl-16 traffic-light clearance so the logo lines up expanded, at lg and up", () => {
    withDesktopHost();
    const { container } = renderHeader({ sidebarCollapsed: false });
    const logo = container.querySelector(LOGO_SELECTOR);
    const brand = logo?.closest("div")?.parentElement;
    expect(brand?.className).toContain("lg:-ml-[68px]");
  });

  it("cancels the shell's pl-16 traffic-light clearance so the logo lines up collapsed, at lg and up", () => {
    withDesktopHost();
    const { container } = renderHeader({ sidebarCollapsed: true });
    const logo = container.querySelector(LOGO_SELECTOR);
    const brandBadge = logo?.closest("div");
    expect(brandBadge?.className).toContain("lg:-ml-[62px]");
  });

  it("does not cancel the traffic-light clearance below lg, so the logo stays clear of the mobile menu button", () => {
    withDesktopHost();
    const { container } = renderHeader({ sidebarCollapsed: false });
    const logo = container.querySelector(LOGO_SELECTOR);
    const brand = logo?.closest("div")?.parentElement;
    const classes = brand?.className?.split(/\s+/) ?? [];
    expect(classes).not.toContain("-ml-[68px]");
  });
});
