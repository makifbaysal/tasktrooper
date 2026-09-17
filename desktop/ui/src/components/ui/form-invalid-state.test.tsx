import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

describe("aria-invalid destructive styling", () => {
  it("Input renders its border and ring in the destructive color when aria-invalid", () => {
    render(<Input aria-invalid placeholder="email" />);
    const el = screen.getByPlaceholderText("email");
    expect(el.className).toContain("aria-invalid:border-destructive");
    expect(el.className).toMatch(/aria-invalid:ring-destructive/);
  });

  it("Textarea renders its border and ring in the destructive color when aria-invalid", () => {
    render(<Textarea aria-invalid placeholder="notes" />);
    const el = screen.getByPlaceholderText("notes");
    expect(el.className).toContain("aria-invalid:border-destructive");
    expect(el.className).toMatch(/aria-invalid:ring-destructive/);
  });

  it("SelectTrigger renders its border and ring in the destructive color when aria-invalid", () => {
    render(
      <Select value="" onValueChange={() => {}}>
        <SelectTrigger aria-invalid data-testid="trigger">
          <SelectValue placeholder="pick one" />
        </SelectTrigger>
      </Select>,
    );
    const el = screen.getByTestId("trigger");
    expect(el.className).toContain("aria-invalid:border-destructive");
    expect(el.className).toMatch(/aria-invalid:ring-destructive/);
  });

  it("Checkbox renders its border and ring in the destructive color when aria-invalid", () => {
    render(<Checkbox aria-invalid data-testid="checkbox" />);
    const el = screen.getByTestId("checkbox");
    expect(el.className).toContain("aria-invalid:border-destructive");
    expect(el.className).toMatch(/aria-invalid:ring-destructive/);
  });

  it("Switch renders its border and ring in the destructive color when aria-invalid", () => {
    render(<Switch aria-invalid data-testid="switch" />);
    const el = screen.getByTestId("switch");
    expect(el.className).toContain("aria-invalid:border-destructive");
    expect(el.className).toMatch(/aria-invalid:ring-destructive/);
  });
});
