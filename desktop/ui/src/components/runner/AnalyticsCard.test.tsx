import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AnalyticsCard } from "@/components/runner/AnalyticsCard";
import { I18nProvider } from "@/hooks/useI18n";
import type { DesktopAnalyticsState } from "@/lib/desktop-bridge";

function stubShell(initial: DesktopAnalyticsState) {
  const analytics = {
    get: vi.fn().mockResolvedValue(initial),
    set: vi.fn((on: boolean) => Promise.resolve({ ...initial, enabled: on })),
  };
  window.__tasktrooperDesktop = { analytics };
  return analytics;
}

function renderCard() {
  return render(
    <I18nProvider>
      <AnalyticsCard />
    </I18nProvider>,
  );
}

afterEach(() => {
  delete window.__tasktrooperDesktop;
});

describe("AnalyticsCard", () => {
  it("renders nothing without a shell", () => {
    const { container } = renderCard();
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when the build carries no analytics", async () => {
    const analytics = stubShell({ available: false, enabled: false, forcedOff: false });
    const { container } = renderCard();
    await waitFor(() => expect(analytics.get).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the switch on, and sends the new value when it is turned off", async () => {
    const analytics = stubShell({ available: true, enabled: true, forcedOff: false });
    renderCard();

    const toggle = await screen.findByRole("switch", { name: "Anonymous usage statistics" });
    expect(toggle).toBeChecked();

    fireEvent.click(toggle);

    await waitFor(() => expect(analytics.set).toHaveBeenCalledWith(false));
    await waitFor(() => expect(toggle).not.toBeChecked());
  });

  it("locks the switch off when the environment forces it", async () => {
    stubShell({ available: true, enabled: false, forcedOff: true });
    renderCard();

    const toggle = await screen.findByRole("switch", { name: "Anonymous usage statistics" });
    expect(toggle).toBeDisabled();
    expect(toggle).not.toBeChecked();
  });
});
