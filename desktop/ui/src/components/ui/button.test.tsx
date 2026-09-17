import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Button } from "@/components/ui/button";

describe("Button active state", () => {
  it.each(["default", "secondary", "destructive"] as const)(
    "darkens and scales down the %s variant on press, distinct from hover",
    (variant) => {
      render(<Button variant={variant}>Save</Button>);
      const button = screen.getByRole("button", { name: "Save" });

      expect(button.className).toMatch(/active:scale-\[0\.98\]/);
      expect(button.className).toMatch(/hover:(bg|brightness)-[\w./-]+/);
      expect(button.className).toMatch(/active:(bg|brightness)-[\w./-]+/);

      const hoverMatch = button.className.match(/hover:(bg|brightness)-[\w./-]+/)![0];
      const activeMatch = button.className
        .split(/\s+/)
        .find((cls) => cls.startsWith("active:") && (cls.includes("bg-") || cls.includes("brightness-")))!;
      expect(activeMatch).not.toBe(hoverMatch);
    },
  );
});
