import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Logo } from "@/assets/logo";

describe("Logo", () => {
  it("matches the tasktrooper-site mark's exact path data", () => {
    const markup = renderToStaticMarkup(<Logo className="h-4 w-4" />);

    expect(markup).toContain('d="M6 30 L50 2 L94 30 V56 L50 30 L6 56 Z"');
    expect(markup).toContain('d="M6 66 L50 40 L94 66 V92 L50 66 L6 92 Z"');
    expect(markup).toContain('d="M6 102 L50 76 L94 102 V128 H6 Z"');
  });
});
