import type { DesignTokenTree } from "@/api";

export type DesignTokenCategory = "color" | "typography" | "radius" | "dimension" | "shadow" | "other";

export interface DesignToken {
  path: string;
  /** `$type`, inherited from the nearest group that declares one. */
  type?: string;
  /** The `$value` as written: an alias stays `{color.primary}`. */
  value: unknown;
  /** Set when `value` is an alias; the referenced path. */
  alias?: string;
  /** The alias chain followed to a concrete value; undefined when it cannot be resolved. */
  resolved: unknown;
  category: DesignTokenCategory;
}

const ALIAS = /^\{([^{}]+)\}$/;
const HEX_COLOR = /^#(?:[0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})$/i;
// No nested parentheses: a value that could smuggle url(...) into a
// background never matches, so only real colors reach an inline style.
const FUNCTIONAL_COLOR = /^(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\([^()]*\)$/i;
const NAMED_COLOR = /^[a-z]+$/i;
const DIMENSION = /^-?(?:\d+\.?\d*|\.\d+)(?:px|rem|em|%|vh|vw|vmin|vmax|pt|ch|ex)$/i;
const MAX_ALIAS_DEPTH = 16;

const TYPOGRAPHY_TYPES = new Set(["typography", "fontfamily", "fontweight", "fontstyle", "lineheight", "letterspacing", "fontsize"]);
const FONT_PATH = /(?:^|[.\-_])(?:text|type)(?:$|[.\-_])|font|typography|leading|tracking|line-?height|letter-?spacing/i;
const RADIUS_PATH = /radius|radii|rounded|corner/i;
const SHADOW_PATH = /shadow|elevation/i;

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function aliasTarget(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const match = ALIAS.exec(value.trim());
  return match ? match[1].trim() : undefined;
}

/** A string that is safe and meaningful as a CSS color: hex, a color function or a named color. */
export function isColorString(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const v = value.trim();
  return HEX_COLOR.test(v) || FUNCTIONAL_COLOR.test(v);
}

/** The CSS color to paint for a color token's value, or undefined when it is not one. */
export function cssColor(value: unknown): string | undefined {
  if (isPlainObject(value) && typeof value.hex === "string" && HEX_COLOR.test(value.hex.trim())) {
    return value.hex.trim();
  }
  if (typeof value !== "string") return undefined;
  const v = value.trim();
  if (isColorString(v) || NAMED_COLOR.test(v)) return v;
  return undefined;
}

/** DTCG dimensions are either "16px" or `{value: 16, unit: "px"}`. */
export function cssDimension(value: unknown): string | undefined {
  if (typeof value === "string" && DIMENSION.test(value.trim())) return value.trim();
  if (typeof value === "number" && Number.isFinite(value)) return value === 0 ? "0" : `${value}px`;
  if (isPlainObject(value) && typeof value.value === "number" && typeof value.unit === "string") {
    const text = `${value.value}${value.unit}`;
    return DIMENSION.test(text) ? text : undefined;
  }
  return undefined;
}

function shadowLayer(value: unknown): string | undefined {
  if (typeof value === "string") return /url\(/i.test(value) ? undefined : value.trim() || undefined;
  if (!isPlainObject(value)) return undefined;
  const parts = [value.offsetX, value.offsetY, value.blur, value.spread].map((p) => cssDimension(p) ?? "0");
  const color = cssColor(value.color) ?? "currentColor";
  return `${value.inset === true ? "inset " : ""}${parts.join(" ")} ${color}`;
}

/** A CSS box-shadow for a DTCG shadow value (one layer, a list of layers or a CSS string). */
export function cssShadow(value: unknown): string | undefined {
  const layers = Array.isArray(value) ? value : [value];
  const css = layers.map(shadowLayer);
  return css.length > 0 && css.every((c): c is string => !!c) ? css.join(", ") : undefined;
}

export type TypographySample = {
  fontFamily?: string;
  fontSize?: string;
  fontWeight?: string | number;
  lineHeight?: string | number;
  letterSpacing?: string;
  fontStyle?: string;
};

function fontFamily(value: unknown): string | undefined {
  if (Array.isArray(value)) {
    const names = value.filter((v): v is string => typeof v === "string");
    return names.length > 0 ? names.map((n) => (/[\s,]/.test(n) && !/^["']/.test(n) ? `"${n}"` : n)).join(", ") : undefined;
  }
  return typeof value === "string" && value.trim() && !value.includes("(") ? value.trim() : undefined;
}

function keyword(value: unknown): string | undefined {
  return typeof value === "string" && /^[a-z0-9-]+$/i.test(value.trim()) ? value.trim() : undefined;
}

function fontWeight(value: unknown): string | number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  return keyword(value);
}

function lineHeight(value: unknown): string | number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  return cssDimension(value);
}

/** The inline style a type-scale sample uses for this token; empty when nothing applies. */
export function typographySample(token: Pick<DesignToken, "type" | "resolved" | "path">): TypographySample {
  const type = token.type?.toLowerCase();
  const value = token.resolved;
  if (type === "typography" && isPlainObject(value)) {
    return {
      fontFamily: fontFamily(value.fontFamily),
      fontSize: cssDimension(value.fontSize),
      fontWeight: fontWeight(value.fontWeight),
      lineHeight: lineHeight(value.lineHeight),
      letterSpacing: cssDimension(value.letterSpacing),
      fontStyle: keyword(value.fontStyle),
    };
  }
  if (type === "fontfamily" || (!type && Array.isArray(value))) return { fontFamily: fontFamily(value) };
  if (type === "fontweight") return { fontWeight: fontWeight(value) };
  if (type === "fontstyle") return { fontStyle: keyword(value) };
  if (type === "lineheight" || /line-?height|leading/i.test(token.path)) return { lineHeight: lineHeight(value) };
  if (type === "letterspacing" || /letter-?spacing|tracking/i.test(token.path)) return { letterSpacing: cssDimension(value) };
  return { fontSize: cssDimension(value) };
}

function categorize(path: string, type: string | undefined, resolved: unknown): DesignTokenCategory {
  const t = type?.toLowerCase();
  if (t === "color") return "color";
  if (t === "shadow") return "shadow";
  if (t && TYPOGRAPHY_TYPES.has(t)) return "typography";
  if (t === "dimension") {
    if (FONT_PATH.test(path)) return "typography";
    return RADIUS_PATH.test(path) ? "radius" : "dimension";
  }
  if (t) return "other";
  if (isColorString(resolved) || (isPlainObject(resolved) && typeof resolved.hex === "string" && "colorSpace" in resolved)) {
    return "color";
  }
  if (SHADOW_PATH.test(path) && cssShadow(resolved)) return "shadow";
  if (typeof resolved === "string" && DIMENSION.test(resolved.trim())) {
    if (FONT_PATH.test(path)) return "typography";
    return RADIUS_PATH.test(path) ? "radius" : "dimension";
  }
  return "other";
}

function lookup(tree: DesignTokenTree, path: string): Record<string, unknown> | undefined {
  let node: unknown = tree;
  for (const segment of path.split(".")) {
    if (!isPlainObject(node) || !(segment in node)) return undefined;
    node = node[segment];
  }
  return isPlainObject(node) && "$value" in node ? node : undefined;
}

function resolveValue(tree: DesignTokenTree, value: unknown): unknown {
  let current = value;
  const seen = new Set<string>();
  for (let depth = 0; depth < MAX_ALIAS_DEPTH; depth++) {
    const target = aliasTarget(current);
    if (target === undefined) return current;
    if (seen.has(target)) return undefined;
    seen.add(target);
    const token = lookup(tree, target);
    if (!token) return undefined;
    current = token.$value;
  }
  return undefined;
}

function typeOfAlias(tree: DesignTokenTree, path: string): string | undefined {
  let node: unknown = tree;
  let inherited: string | undefined;
  for (const segment of path.split(".")) {
    if (!isPlainObject(node)) return inherited;
    if (typeof node.$type === "string") inherited = node.$type;
    node = node[segment];
  }
  return isPlainObject(node) && typeof node.$type === "string" ? node.$type : inherited;
}

/**
 * Every token in a DTCG tree, depth first in the tree's own order. Groups pass
 * their `$type` down; `$`-prefixed keys other than `$value` are metadata, not
 * children. Anything that is neither a group nor a token is skipped, so a
 * partial or malformed tree still yields what it can.
 */
export function flattenDesignTokens(tree: DesignTokenTree | null | undefined): DesignToken[] {
  if (!isPlainObject(tree)) return [];
  const out: DesignToken[] = [];
  const walk = (node: Record<string, unknown>, prefix: string[], inheritedType: string | undefined) => {
    const groupType = typeof node.$type === "string" ? node.$type : inheritedType;
    for (const [key, child] of Object.entries(node)) {
      if (key.startsWith("$") || !isPlainObject(child)) continue;
      const path = [...prefix, key];
      if ("$value" in child) {
        const value = child.$value;
        const alias = aliasTarget(value);
        const resolved = alias === undefined ? value : resolveValue(tree, value);
        const own = typeof child.$type === "string" ? child.$type : undefined;
        const type = own ?? groupType ?? (alias ? typeOfAlias(tree, alias) : undefined);
        const joined = path.join(".");
        out.push({ path: joined, type, value, alias, resolved, category: categorize(joined, type, resolved) });
      } else {
        walk(child, path, groupType);
      }
    }
  };
  walk(tree, [], undefined);
  return out;
}

export function groupDesignTokens(tokens: DesignToken[]): Record<DesignTokenCategory, DesignToken[]> {
  const groups: Record<DesignTokenCategory, DesignToken[]> = {
    color: [],
    typography: [],
    radius: [],
    dimension: [],
    shadow: [],
    other: [],
  };
  for (const token of tokens) groups[token.category].push(token);
  return groups;
}

/** A one-line rendering of any token value for the path → value table. */
export function formatTokenValue(value: unknown): string {
  if (value === undefined) return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (value === null) return "null";
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

/** The part of a path above the token itself: `color.brand.primary` → `color.brand`. */
export function tokenGroup(path: string): string {
  const i = path.lastIndexOf(".");
  return i === -1 ? "" : path.slice(0, i);
}
