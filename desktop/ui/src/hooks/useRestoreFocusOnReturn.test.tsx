import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useRestoreFocusOnReturn } from "@/hooks/useRestoreFocusOnReturn";

function TestApp() {
  useRestoreFocusOnReturn();
  return (
    <div>
      <input data-testid="a" aria-label="a" />
      <input data-testid="b" aria-label="b" />
      <button data-testid="button" type="button">
        run
      </button>
    </div>
  );
}

function focusOutOfWindow(el: HTMLElement): void {
  // Chromium fires `focusout` with a null relatedTarget when focus leaves the
  // document (alt-tab, window switch), then clears document focus.
  el.dispatchEvent(new FocusEvent("focusout", { bubbles: true, relatedTarget: null }));
  el.blur();
}

describe("useRestoreFocusOnReturn", () => {
  it("hands focus back to the field that lost it when the window returns", () => {
    render(<TestApp />);
    const input = screen.getByTestId("a") as HTMLInputElement;
    input.focus();
    focusOutOfWindow(input);
    expect(document.activeElement).toBe(document.body);

    fireEvent(window, new Event("focus"));

    expect(document.activeElement).toBe(input);
  });

  it("captures the field from the window blur even when no focusout was delivered", () => {
    render(<TestApp />);
    const input = screen.getByTestId("a") as HTMLInputElement;
    input.focus();
    // Electron can deactivate the webContents without firing the element's own
    // blur/focusout; the window's blur alone must still remember the field.
    fireEvent(window, new Event("blur"));
    input.blur();
    fireEvent(window, new Event("focus"));

    expect(document.activeElement).toBe(input);
  });

  it("restores the field when the tab becomes visible again", () => {
    render(<TestApp />);
    const input = screen.getByTestId("a") as HTMLInputElement;
    input.focus();
    focusOutOfWindow(input);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(document.activeElement).toBe(input);
    delete (document as { visibilityState?: string }).visibilityState;
  });

  it("leaves an in-app focus change alone", () => {
    render(<TestApp />);
    const input = screen.getByTestId("a") as HTMLInputElement;
    const button = screen.getByTestId("button");
    // Focus moved within the document (relatedTarget is the new owner), which
    // is the app's own doing and must not be remembered as a window switch.
    input.dispatchEvent(new FocusEvent("focusout", { bubbles: true, relatedTarget: button }));
    input.blur();
    button.focus();

    fireEvent(window, new Event("focus"));

    expect(document.activeElement).toBe(button);
  });

  it("does not steal focus from a control that already owns it in the document", () => {
    render(<TestApp />);
    const input = screen.getByTestId("a") as HTMLInputElement;
    const button = screen.getByTestId("button");
    input.focus();
    focusOutOfWindow(input);
    // By the time the window is back, the user clicked something in-app.
    button.focus();

    fireEvent(window, new Event("focus"));

    expect(document.activeElement).toBe(button);
  });

  it("forgets an element that unmounted while the window was away", () => {
    const { rerender } = render(<TestApp />);
    const input = screen.getByTestId("a") as HTMLInputElement;
    input.focus();
    focusOutOfWindow(input);

    // Replace the tree: the remembered field is gone by the time focus returns.
    rerender(<div />);
    fireEvent(window, new Event("focus"));

    expect(document.activeElement).toBe(document.body);
  });
});