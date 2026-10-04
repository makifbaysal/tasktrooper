import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";
import type { Agent } from "@/api";
import { Header } from "@/components/layout/Header";
import { I18nProvider } from "@/hooks/useI18n";
import { SetupProvider } from "@/hooks/useSetup";
import { ThemeProvider } from "@/hooks/useTheme";
import type { TaskTrooperDesktopHost } from "@/lib/desktop-bridge";

function renderHeader(props: Parameters<typeof Header>[0] = {}) {
  return render(
    <I18nProvider>
      <ThemeProvider>
        <SetupProvider>
          <Header {...props} />
        </SetupProvider>
      </ThemeProvider>
    </I18nProvider>,
  );
}

const LOGO_SELECTOR = 'svg[viewBox="0 0 100 130"]';

describe("Header logo alignment", () => {
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

  it("uses the same nav-inset alignment regardless of the desktop shell, since the shell renders the app below its title bar", () => {
    window.__tasktrooperDesktop = {
      runner: { snapshot: () => Promise.resolve(null), subscribe: () => () => {} },
    } as unknown as TaskTrooperDesktopHost;
    const { container } = renderHeader({ sidebarCollapsed: false });
    const logo = container.querySelector(LOGO_SELECTOR);
    const brand = logo?.closest("div")?.parentElement;
    expect(brand?.className).toContain("-ml-1");
    delete window.__tasktrooperDesktop;
  });
});

describe("Header lead quick-ask", () => {
  const lead = { id: "pm", name: "Product Manager", enabled: true } as Agent;
  const ASK = /message product manager|product manager/i;

  function renderAt(path: string, leadAgent: Agent | null) {
    return render(
      <I18nProvider>
        <ThemeProvider>
          <SetupProvider>
            <MemoryRouter initialEntries={[path]}>
              <Header leadAgent={leadAgent} />
            </MemoryRouter>
          </SetupProvider>
        </ThemeProvider>
      </I18nProvider>,
    );
  }

  it("renders the ask field on another route when a lead exists", () => {
    renderAt("/board", lead);
    expect(screen.getByRole("textbox", { name: ASK })).toBeTruthy();
  });

  it("hides it on the lead's own chat route", () => {
    renderAt("/agents/pm/chat/s1", lead);
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("hides it on /home, whose own message box is the ask", () => {
    renderAt("/home", lead);
    expect(screen.queryByRole("textbox")).toBeNull();
  });

  it("hides it when there is no lead", () => {
    renderAt("/board", null);
    expect(screen.queryByRole("textbox")).toBeNull();
  });
});
