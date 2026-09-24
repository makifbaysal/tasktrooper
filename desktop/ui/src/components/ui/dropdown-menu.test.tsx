import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";

describe("DropdownMenu", () => {
  it("leaves the page clickable while open, so a dialog opened from an item cannot freeze it", async () => {
    render(
      <DropdownMenu open>
        <DropdownMenuTrigger>Open</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem>Item</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>,
    );
    await screen.findByRole("menuitem", { name: "Item" });
    expect(document.body.style.pointerEvents).not.toBe("none");
  });
});
