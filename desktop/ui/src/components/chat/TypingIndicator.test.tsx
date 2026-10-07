import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TYPING_ANIMATION_MS, TypingIndicator } from "@/components/chat/TypingIndicator";
import { I18nProvider } from "@/hooks/useI18n";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("TypingIndicator", () => {
  it("bounces for the first seconds, then holds still", () => {
    const { container } = render(
      <I18nProvider>
        <TypingIndicator />
      </I18nProvider>,
    );
    expect(container.querySelectorAll(".animate-bounce")).toHaveLength(3);

    act(() => vi.advanceTimersByTime(TYPING_ANIMATION_MS - 1));
    expect(container.querySelectorAll(".animate-bounce")).toHaveLength(3);

    act(() => vi.advanceTimersByTime(1));
    expect(container.querySelectorAll(".animate-bounce")).toHaveLength(0);
    expect(container.querySelectorAll(".rounded-full")).toHaveLength(3);
  });
});
