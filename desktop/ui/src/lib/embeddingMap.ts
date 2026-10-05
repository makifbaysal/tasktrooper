// Pure helpers shared by the embedding-map panel and its UMAP web worker.
// Keep this module free of React and DOM APIs — it is imported inside a worker.

/** Bucket id used for every group that does not fit in the legend. */
export const OTHER_GROUP_ID = "__other__";

/**
 * Categorical hues are never cycled: past this many groups the rest fold into
 * "other" and share one neutral.
 */
export const MAX_COLORED_GROUPS = 8;

export const DEFAULT_N_NEIGHBORS = 15;
export const DEFAULT_MIN_DIST = 0.1;
export const DEFAULT_LIMIT = 2000;
export const MIN_LIMIT = 100;
export const MAX_LIMIT = 5000;

/* -------------------------------------------------------------------------- */
/* Worker protocol                                                            */
/* -------------------------------------------------------------------------- */

export interface EmbeddingMapWorkerRequest {
  requestId: number;
  /** Row-major PCA-reduced vectors, `count` rows of `dims` values. */
  vectors: Float32Array;
  count: number;
  dims: number;
  nNeighbors: number;
  minDist: number;
}

export type EmbeddingMapProgressPhase = "neighbors" | "layout" | "clusters";

export type EmbeddingMapWorkerResponse =
  | { type: "progress"; requestId: number; phase: EmbeddingMapProgressPhase; ratio: number }
  | {
      type: "done";
      requestId: number;
      /** `2 * count` interleaved x/y pairs, already normalized into [0, 1]. */
      positions: Float32Array;
      /** Per-point cluster id over `positions`: 0..k-1 by size, -1 for noise. */
      clusters: Int32Array;
    }
  | { type: "error"; requestId: number; message: string };

/**
 * UMAP needs at least two neighbours and cannot use more than `count - 1`.
 * Callers pass the user-facing value; this keeps tiny datasets from blowing up.
 */
export function clampNeighbors(nNeighbors: number, count: number): number {
  const upper = Math.max(2, count - 1);
  return Math.max(2, Math.min(Math.round(nNeighbors), upper));
}

export function clampLimit(limit: number): number {
  if (!Number.isFinite(limit)) return DEFAULT_LIMIT;
  return Math.max(MIN_LIMIT, Math.min(MAX_LIMIT, Math.round(limit)));
}

export function clampMinDist(minDist: number): number {
  if (!Number.isFinite(minDist)) return DEFAULT_MIN_DIST;
  return Math.max(0.001, Math.min(0.99, minDist));
}

/**
 * Squash an arbitrary 2D layout into [0, 1] on both axes while preserving the
 * aspect ratio, so clusters keep their real shape. UMAP output has no fixed
 * range, so nothing here may assume one.
 */
export function normalizePositions(embedding: number[][]): Float32Array {
  const count = embedding.length;
  const out = new Float32Array(count * 2);
  if (count === 0) return out;
  if (count === 1) {
    out[0] = 0.5;
    out[1] = 0.5;
    return out;
  }

  let minX = Number.POSITIVE_INFINITY;
  let maxX = Number.NEGATIVE_INFINITY;
  let minY = Number.POSITIVE_INFINITY;
  let maxY = Number.NEGATIVE_INFINITY;
  for (const row of embedding) {
    const x = Number.isFinite(row[0]) ? row[0] : 0;
    const y = Number.isFinite(row[1]) ? row[1] : 0;
    if (x < minX) minX = x;
    if (x > maxX) maxX = x;
    if (y < minY) minY = y;
    if (y > maxY) maxY = y;
  }

  const spanX = maxX - minX;
  const spanY = maxY - minY;
  const span = Math.max(spanX, spanY);
  if (!Number.isFinite(span) || span <= 0) {
    // Every point landed on the same spot — spread them on a small ring so the
    // user still sees "n points" rather than one dot.
    for (let i = 0; i < count; i++) {
      const angle = (i / count) * Math.PI * 2;
      out[i * 2] = 0.5 + Math.cos(angle) * 0.02;
      out[i * 2 + 1] = 0.5 + Math.sin(angle) * 0.02;
    }
    return out;
  }

  const offsetX = (span - spanX) / 2;
  const offsetY = (span - spanY) / 2;
  for (let i = 0; i < count; i++) {
    const row = embedding[i];
    const x = Number.isFinite(row[0]) ? row[0] : 0;
    const y = Number.isFinite(row[1]) ? row[1] : 0;
    out[i * 2] = (x - minX + offsetX) / span;
    out[i * 2 + 1] = (y - minY + offsetY) / span;
  }
  return out;
}

/* -------------------------------------------------------------------------- */
/* Categorical color scale                                                     */
/* -------------------------------------------------------------------------- */

// Validated categorical hues (see .ai design guidance / data-viz palette):
// fixed slot order, stepped separately for the light and dark surfaces.
const CATEGORICAL_LIGHT = [
  "#2a78d6", // blue
  "#eb6834", // orange
  "#1baf7a", // aqua
  "#eda100", // yellow
  "#e87ba4", // magenta
  "#008300", // green
  "#4a3aa7", // violet
  "#e34948", // red
];

const CATEGORICAL_DARK = [
  "#3987e5",
  "#d95926",
  "#199e70",
  "#c98500",
  "#d55181",
  "#008300",
  "#9085e9",
  "#e66767",
];

export type EmbeddingMapTheme = "light" | "dark";

/** Neutral used for the folded "other" bucket. */
export function otherGroupColor(theme: EmbeddingMapTheme): string {
  return theme === "dark" ? "#7c7c86" : "#9a9aa4";
}

/**
 * The Nth categorical slot, or the neutral once the validated hues run out —
 * a generated ninth hue would be indistinguishable from its neighbours.
 */
export function groupColorAt(index: number, theme: EmbeddingMapTheme): string {
  const palette = theme === "dark" ? CATEGORICAL_DARK : CATEGORICAL_LIGHT;
  if (index < 0 || index >= palette.length) return otherGroupColor(theme);
  return palette[index];
}

export interface EmbeddingGroup {
  id: string;
  label: string;
  /** Secondary line under the label, e.g. a topic's dominant directory. */
  sublabel?: string;
  count: number;
  color: string;
}

export interface EmbeddingGroupScale {
  /** Every distinct group, biggest first. */
  groups: EmbeddingGroup[];
  /** Legend rows: the colored groups in slot order, then the neutral ones. */
  legend: EmbeddingGroup[];
  /** Groups folded into the "other" legend row (empty when nothing folded). */
  otherGroupIds: Set<string>;
  colorByGroupId: Map<string, string>;
}

export interface EmbeddingGroupScaleOptions {
  /**
   * Slot assignment. "count" (default) colors the biggest groups; an explicit
   * id list pins each id to its index so an entity keeps its hue whether or
   * not its neighbours are present.
   */
  order?: "count" | readonly string[];
  /** Groups drawn in the neutral without taking a slot (e.g. unclustered noise). */
  neutralGroupIds?: ReadonlySet<string>;
  /**
   * Fold groups past the colored slots into one "other" row (default). Off,
   * they keep their own neutral legend rows — for groups the plot labels
   * directly, where one row per group is still worth hovering.
   */
  foldOverflow?: boolean;
}

export function buildGroupScale(
  entries: { groupId: string; label: string; sublabel?: string }[],
  theme: EmbeddingMapTheme,
  options: EmbeddingGroupScaleOptions = {},
): EmbeddingGroupScale {
  const byId = new Map<string, EmbeddingGroup>();
  for (const entry of entries) {
    const existing = byId.get(entry.groupId);
    if (existing) {
      existing.count += 1;
    } else {
      byId.set(entry.groupId, {
        id: entry.groupId,
        label: entry.label,
        sublabel: entry.sublabel,
        count: 1,
        color: "",
      });
    }
  }

  const groups = [...byId.values()].sort(
    (a, b) => b.count - a.count || a.label.localeCompare(b.label) || a.id.localeCompare(b.id),
  );
  const neutralIds = options.neutralGroupIds ?? new Set<string>();
  const neutral = otherGroupColor(theme);
  const colorByGroupId = new Map<string, string>();
  const colored: { group: EmbeddingGroup; slot: number }[] = [];
  const otherGroupIds = new Set<string>();

  const order = options.order ?? "count";
  let nextSlot = 0;
  for (const group of groups) {
    if (neutralIds.has(group.id)) continue;
    const slot = order === "count" ? nextSlot++ : order.indexOf(group.id);
    if (slot >= 0 && slot < MAX_COLORED_GROUPS) {
      colored.push({ group, slot });
    } else {
      otherGroupIds.add(group.id);
    }
  }

  for (const group of groups) group.color = neutral;
  for (const { group, slot } of colored) group.color = groupColorAt(slot, theme);
  for (const group of groups) colorByGroupId.set(group.id, group.color);

  const overflow = options.foldOverflow === false ? groups.filter((group) => otherGroupIds.has(group.id)) : [];
  if (options.foldOverflow === false) otherGroupIds.clear();

  const legend = [
    ...colored.sort((a, b) => a.slot - b.slot).map(({ group }) => group),
    ...overflow,
    ...groups.filter((group) => neutralIds.has(group.id)),
  ];

  return { groups, legend, otherGroupIds, colorByGroupId };
}

/* -------------------------------------------------------------------------- */
/* Spatial index for hover hit-testing                                         */
/* -------------------------------------------------------------------------- */

/**
 * Uniform grid over the normalized [0, 1] layout. Hover lookups only scan the
 * cells within the query radius, so mousemove stays cheap at 5000 points.
 */
export class PointGrid {
  private readonly cells: number[][];
  private readonly resolution: number;
  private readonly positions: Float32Array;

  constructor(positions: Float32Array, resolution?: number) {
    this.positions = positions;
    const count = positions.length / 2;
    this.resolution = Math.max(1, resolution ?? Math.ceil(Math.sqrt(Math.max(count, 1)) / 2));
    this.cells = Array.from({ length: this.resolution * this.resolution }, () => []);
    for (let i = 0; i < count; i++) {
      const cell = this.cellIndex(positions[i * 2], positions[i * 2 + 1]);
      this.cells[cell].push(i);
    }
  }

  private cellIndex(x: number, y: number): number {
    const cx = Math.max(0, Math.min(this.resolution - 1, Math.floor(x * this.resolution)));
    const cy = Math.max(0, Math.min(this.resolution - 1, Math.floor(y * this.resolution)));
    return cy * this.resolution + cx;
  }

  /** Index of the closest point within `radius`, or -1. Coordinates are [0, 1]. */
  nearest(x: number, y: number, radius: number): number {
    const span = Math.max(1, Math.ceil(radius * this.resolution));
    const cx = Math.max(0, Math.min(this.resolution - 1, Math.floor(x * this.resolution)));
    const cy = Math.max(0, Math.min(this.resolution - 1, Math.floor(y * this.resolution)));
    const radiusSq = radius * radius;

    let best = -1;
    let bestDistSq = radiusSq;
    for (let gy = cy - span; gy <= cy + span; gy++) {
      if (gy < 0 || gy >= this.resolution) continue;
      for (let gx = cx - span; gx <= cx + span; gx++) {
        if (gx < 0 || gx >= this.resolution) continue;
        for (const index of this.cells[gy * this.resolution + gx]) {
          const dx = this.positions[index * 2] - x;
          const dy = this.positions[index * 2 + 1] - y;
          const distSq = dx * dx + dy * dy;
          if (distSq <= bestDistSq) {
            bestDistSq = distSq;
            best = index;
          }
        }
      }
    }
    return best;
  }
}
