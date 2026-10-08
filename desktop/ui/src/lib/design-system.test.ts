import { describe, expect, it } from "vitest";
import type { TaskDocument } from "@/api";
import {
  chosenVariantComment,
  chosenVariantTitles,
  designDocumentKind,
  designDocumentLabel,
  designHtmlDocuments,
  designMarkdownDocuments,
  designVariantGroups,
  designVariantScreen,
} from "@/lib/design-system";

function doc(title: string, format: TaskDocument["format"], position = 0): TaskDocument {
  return {
    id: title,
    task_id: "t",
    title,
    content: "",
    format,
    position,
    created_by_type: "agent",
    created_by_id: "a",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

describe("designDocumentKind", () => {
  it("reads the designer's title convention", () => {
    expect(designDocumentKind(doc("design: export dialog · A", "html"))).toBe("mockup");
    expect(designDocumentKind(doc("handoff: export dialog", "markdown"))).toBe("handoff");
    expect(designDocumentKind(doc("Hand-off: export dialog", undefined))).toBe("handoff");
    expect(designDocumentKind(doc("design system: TaskTrooper v3", "html"))).toBe("system");
    expect(designDocumentKind(doc("design review: export dialog", "html"))).toBe("review");
    expect(designDocumentKind(doc("notes", "markdown"))).toBe("other");
    expect(designDocumentKind(doc("design: markdown by mistake", "markdown"))).toBe("other");
  });

  it("drops the prefix from the label", () => {
    expect(designDocumentLabel(doc("design: export dialog · A", "html"))).toBe("export dialog · A");
    expect(designDocumentLabel(doc("design system: TaskTrooper v3", "html"))).toBe("TaskTrooper v3");
    expect(designDocumentLabel(doc("Plain title", "html"))).toBe("Plain title");
  });
});

describe("design document ordering", () => {
  it("lists mockups first in variant order, then the other HTML documents", () => {
    const docs = [
      doc("design review: export dialog", "html"),
      doc("design: export dialog · B", "html"),
      doc("handoff: export dialog", "markdown"),
      doc("design: export dialog · A", "html"),
    ];
    expect(designHtmlDocuments(docs).map((d) => d.title)).toEqual([
      "design: export dialog · A",
      "design: export dialog · B",
      "design review: export dialog",
    ]);
    expect(designMarkdownDocuments([doc("notes", "markdown", 0), doc("handoff: x", "markdown", 1)]).map((d) => d.title)).toEqual([
      "handoff: x",
      "notes",
    ]);
  });
});

describe("chosen variant", () => {
  it("writes the English marker the designer looks for", () => {
    expect(chosenVariantComment(" design: export dialog · B ")).toBe("Chosen variant: design: export dialog · B");
  });

  it("reads the newest marker, ignoring other comments", () => {
    const comments = [
      { content: "Chosen variant: design: export dialog · A", created_at: "2026-01-01T10:00:00Z" },
      { content: "Looks good", created_at: "2026-01-01T12:00:00Z" },
      { content: "Chosen variant: design: export dialog · B", created_at: "2026-01-01T11:00:00Z" },
    ];
    expect(chosenVariantTitles(comments)).toEqual(["design: export dialog · B"]);
    expect(chosenVariantTitles([{ content: "chosen variant: lower case", created_at: "x" }])).toEqual([]);
    expect(chosenVariantTitles([])).toEqual([]);
  });

  it("keeps one choice per screen", () => {
    const comments = [
      { content: "Chosen variant: design: Home · B", created_at: "2026-01-01T10:00:00Z" },
      { content: "Chosen variant: design: Gallery · A", created_at: "2026-01-01T11:00:00Z" },
      { content: "Chosen variant: design: Gallery · B\n\nKeep A's grid.", created_at: "2026-01-01T12:00:00Z" },
    ];
    expect(chosenVariantTitles(comments)).toEqual(["design: Gallery · B", "design: Home · B"]);
  });
});

describe("design variant groups", () => {
  it("reads the screen off a mockup title", () => {
    expect(designVariantScreen("design: Ana Sayfa · A")).toBe("Ana Sayfa");
    expect(designVariantScreen("design: Invoices — list · B")).toBe("Invoices — list");
    expect(designVariantScreen("design: no variant letter")).toBeNull();
    expect(designVariantScreen("design system: Shop · v2")).toBeNull();
    expect(designVariantScreen("handoff: Home · A")).toBeNull();
  });

  // The bug this guards: seven pages drawn once each were offered as seven
  // "variants" to choose between.
  it("offers nothing when every screen has a single variant", () => {
    const docs = ["Ana Sayfa", "Galeri", "Hakkımda", "İletişim"].map((screen) => doc(`design: ${screen} · A`, "html"));
    expect(designVariantGroups(docs)).toEqual([]);
  });

  it("offers only the screens that have alternatives, each with its variants in order", () => {
    const docs = [
      doc("design: Galeri · B", "html"),
      doc("design: Ana Sayfa · A", "html"),
      doc("design: Galeri · A", "html"),
      doc("design: Hakkımda · A", "html"),
      doc("design review: Galeri", "html"),
      doc("handoff: Galeri", "markdown"),
    ];
    const groups = designVariantGroups(docs);
    expect(groups.map((group) => group.screen)).toEqual(["Galeri"]);
    expect(groups[0]!.documents.map((d) => d.title)).toEqual(["design: Galeri · A", "design: Galeri · B"]);
  });
});
