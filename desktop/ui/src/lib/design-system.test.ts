import { describe, expect, it } from "vitest";
import type { TaskDocument } from "@/api";
import {
  chosenVariantComment,
  chosenVariantTitle,
  designDocumentKind,
  designDocumentLabel,
  designHtmlDocuments,
  designMarkdownDocuments,
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
    expect(chosenVariantTitle(comments)).toBe("design: export dialog · B");
    expect(chosenVariantTitle([{ content: "chosen variant: lower case", created_at: "x" }])).toBeNull();
    expect(chosenVariantTitle([])).toBeNull();
  });
});
