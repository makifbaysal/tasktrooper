import type { CanvasPage } from "@/components/board/analysis/srcdoc";

// Variant documents name the same page differently only where they name their
// own variant: "Variant A — table first" / "Variant B — cards first", or a
// trailing "· B". Those words go before two titles are compared.
const VARIANT_WORDS = /\b(?:variant|varyant|variante|variação|variacao)\s+[a-z]\b/giu;
const TRAILING_LETTER = /(?:[·\-–—:(]\s*)[a-z]\)?\s*$/iu;

export function normalizePageTitle(title: string): string {
  return title
    .toLocaleLowerCase("en")
    .replace(VARIANT_WORDS, " ")
    .replace(TRAILING_LETTER, " ")
    .replace(/[^\p{L}\p{N}]+/gu, " ")
    .trim();
}

/**
 * The page of `target` that corresponds to `sourceId` in `source`: the one
 * with the same title once variant names are taken out, else the one at the
 * same position, else null.
 */
export function matchingPage(source: CanvasPage[], sourceId: string, target: CanvasPage[]): string | null {
  const index = source.findIndex((page) => page.id === sourceId);
  if (index < 0 || target.length === 0) return null;
  const title = normalizePageTitle(source[index].title);
  if (title) {
    const same = target.filter((page) => normalizePageTitle(page.title) === title);
    if (same.length === 1) return same[0].id;
    if (same.length > 1) {
      const rank = source.slice(0, index).filter((page) => normalizePageTitle(page.title) === title).length;
      return (same[rank] ?? same[0]).id;
    }
  }
  return target[index]?.id ?? null;
}
