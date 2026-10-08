import type { TaskComment, TaskDocument } from "@/api";
import { documentFormat } from "@/lib/analysis-review";

/** The `?tab=` value of the Design System tab on both the project and the repository page. */
export const DESIGN_SYSTEM_TAB = "design";

export function projectDesignSystemPath(projectId: string): string {
  return `/projects/${encodeURIComponent(projectId)}?tab=${DESIGN_SYSTEM_TAB}`;
}

export function repositoryDesignSystemPath(repositoryId: string, projectId?: string): string {
  const params = new URLSearchParams({ tab: DESIGN_SYSTEM_TAB });
  if (projectId) params.set("project", projectId);
  return `/repositories/${encodeURIComponent(repositoryId)}?${params.toString()}`;
}

/** The task type the designer agent works on; it is reviewed on the analysis review page like `analiz`. */
export const DESIGN_TASK_TYPE = "design";

export function isDesignTask(task: { task_type?: string } | null | undefined): boolean {
  return task?.task_type === DESIGN_TASK_TYPE;
}

/**
 * What a design task's document is, read off the designer's title convention:
 * `design: <screen> · <variant>` (an HTML mockup, one per variant),
 * `handoff: <screen>` (the markdown hand-off spec), `design system: <name> v<N>`
 * (the report) and `design review: <screen>`.
 */
export type DesignDocumentKind = "mockup" | "handoff" | "system" | "review" | "other";

const TITLE_KINDS: [RegExp, DesignDocumentKind][] = [
  [/^design\s+system\s*:/i, "system"],
  [/^design\s+review\s*:/i, "review"],
  [/^design\s*:/i, "mockup"],
  [/^hand-?off\s*:/i, "handoff"],
];

export function designDocumentKind(doc: Pick<TaskDocument, "title" | "format">): DesignDocumentKind {
  const title = doc.title.trim();
  for (const [pattern, kind] of TITLE_KINDS) {
    if (!pattern.test(title)) continue;
    if (kind === "handoff") return documentFormat(doc) === "markdown" ? kind : "other";
    return documentFormat(doc) === "html" ? kind : "other";
  }
  return "other";
}

/** The title without its `design:`/`handoff:`/… prefix — "export dialog · A". */
export function designDocumentLabel(doc: Pick<TaskDocument, "title">): string {
  const title = doc.title.trim();
  const match = /^(?:design\s+system|design\s+review|design|hand-?off)\s*:\s*(.+)$/i.exec(title);
  return match ? match[1].trim() : title;
}

const KIND_ORDER: Record<DesignDocumentKind, number> = { mockup: 0, review: 1, system: 2, handoff: 3, other: 4 };

/** A design task's HTML documents, mockups first, each group in title order (variant A before B). */
export function designHtmlDocuments(documents: TaskDocument[]): TaskDocument[] {
  return documents
    .filter((doc) => documentFormat(doc) === "html")
    .sort(
      (a, b) =>
        KIND_ORDER[designDocumentKind(a)] - KIND_ORDER[designDocumentKind(b)] ||
        a.title.localeCompare(b.title, undefined, { numeric: true }),
    );
}

/** Its markdown documents, hand-off specs first. */
export function designMarkdownDocuments(documents: TaskDocument[]): TaskDocument[] {
  return documents
    .filter((doc) => documentFormat(doc) === "markdown")
    .sort(
      (a, b) =>
        Number(designDocumentKind(b) === "handoff") - Number(designDocumentKind(a) === "handoff") ||
        a.position - b.position,
    );
}

/**
 * The marker the designer agent looks for in the task's comments. It stays
 * English whatever the UI language — only the button that writes it is
 * translated.
 */
export const CHOSEN_VARIANT_PREFIX = "Chosen variant: ";

export function chosenVariantComment(documentTitle: string): string {
  return `${CHOSEN_VARIANT_PREFIX}${documentTitle.trim()}`;
}

/** The document title the newest `Chosen variant: ` comment names, or null. */
export function chosenVariantTitle(comments: Pick<TaskComment, "content" | "created_at">[]): string | null {
  let latest: { title: string; at: string } | null = null;
  for (const comment of comments) {
    const content = comment.content.trim();
    if (!content.startsWith(CHOSEN_VARIANT_PREFIX)) continue;
    const title = content.slice(CHOSEN_VARIANT_PREFIX.length).split("\n")[0].trim();
    if (!title) continue;
    if (!latest || comment.created_at >= latest.at) latest = { title, at: comment.created_at };
  }
  return latest?.title ?? null;
}
