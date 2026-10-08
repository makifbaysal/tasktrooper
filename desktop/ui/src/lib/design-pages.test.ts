import { describe, expect, it } from "vitest";
import { matchingPage, normalizePageTitle } from "@/lib/design-pages";

const pages = (...titles: string[]) => titles.map((title, i) => ({ id: `p${i}`, title }));

describe("normalizePageTitle", () => {
  it("drops the variant's own name and punctuation", () => {
    expect(normalizePageTitle("Variant A — table first")).toBe("table first");
    expect(normalizePageTitle("Varyant B: Boş durum")).toBe("boş durum");
    expect(normalizePageTitle("Varsayılan Durum")).toBe("varsayılan durum");
    expect(normalizePageTitle("States · B")).toBe("states");
  });
});

describe("matchingPage", () => {
  const a = pages("Notes", "Variant A — table first", "Boş Durum", "Hata");
  const b = pages("Notes", "Variant B — cards first", "Yükleniyor", "Boş Durum", "Hata");

  it("matches by title once the variant names are gone", () => {
    expect(matchingPage(a, "p2", b)).toBe("p3");
    expect(matchingPage(a, "p3", b)).toBe("p4");
    expect(matchingPage(b, "p3", a)).toBe("p2");
  });

  it("falls back to the same position when no title matches", () => {
    expect(matchingPage(a, "p1", b)).toBe("p1");
    expect(matchingPage(b, "p2", a)).toBe("p2");
  });

  it("keeps the order among pages that share a title", () => {
    expect(matchingPage(pages("375", "1440", "375"), "p2", pages("375", "375", "1440"))).toBe("p1");
  });

  it("is null for an unknown page or an empty target", () => {
    expect(matchingPage(a, "p9", b)).toBeNull();
    expect(matchingPage(a, "p0", [])).toBeNull();
    expect(matchingPage(pages("a", "b", "c"), "p2", pages("x"))).toBeNull();
  });
});
