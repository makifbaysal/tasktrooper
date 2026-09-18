import { describe, expect, it } from "vitest";
import { cn } from "@/lib/utils";

describe("cn", () => {
  it("keeps a text-color utility next to one of our custom text-size utilities", () => {
    // Regression: tailwind-merge doesn't know our globals.css `--text-*` scale
    // (text-caption, text-micro, …), so it used to misclassify these as
    // text-color and silently drop `text-primary-foreground` — every
    // `size="sm"` Button (bg-primary text-primary-foreground ... text-caption)
    // lost its text color this way and rendered unreadable.
    for (const size of ["text-display", "text-title", "text-heading", "text-body", "text-caption", "text-micro"]) {
      const merged = cn("text-primary-foreground", size);
      expect(merged.split(" ")).toEqual(expect.arrayContaining(["text-primary-foreground", size]));
    }
  });

  it("still lets a real text-color utility override an earlier one", () => {
    expect(cn("text-primary-foreground", "text-destructive")).toBe("text-destructive");
  });
});
