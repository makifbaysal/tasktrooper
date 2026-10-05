// What the embedding map says about an index: chunk kinds, directory groups
// and topic labels for the clusters. Pure and DOM-free.

export type ChunkKind = "source" | "test" | "mock" | "generated" | "docs" | "config";

/** Fixed order: a kind's color slot is its index here. */
export const CHUNK_KINDS: readonly ChunkKind[] = [
  "source",
  "test",
  "mock",
  "generated",
  "docs",
  "config",
];

const GENERATED_SUFFIXES = [
  ".pb.go",
  "_pb2.py",
  "_pb2_grpc.py",
  ".pb.ts",
  "_gen.go",
  ".gen.ts",
  ".gen.go",
  ".min.js",
  ".min.css",
];
const LOCK_FILES = new Set([
  "package-lock.json",
  "yarn.lock",
  "pnpm-lock.yaml",
  "go.sum",
  "cargo.lock",
  "poetry.lock",
  "gemfile.lock",
  "composer.lock",
]);
const GENERATED_DIRS = new Set(["generated", "__generated__"]);
const MOCK_DIRS = new Set(["mock", "mocks", "__mocks__"]);
const TEST_DIRS = new Set(["test", "tests", "__tests__", "testdata", "e2e", "spec"]);
const DOC_DIRS = new Set(["docs", "doc"]);
const DOC_EXTENSIONS = new Set(["md", "mdx", "rst", "adoc", "txt"]);
const CONFIG_EXTENSIONS = new Set([
  "json",
  "yaml",
  "yml",
  "toml",
  "ini",
  "xml",
  "properties",
  "gradle",
  "cfg",
  "conf",
  "env",
]);
const CONFIG_BASENAMES = new Set(["dockerfile", "makefile", "procfile"]);

function pathSegments(path: string): string[] {
  return path.split("/").filter((part) => part !== "");
}

function extensionOf(basename: string): string {
  const dot = basename.lastIndexOf(".");
  return dot > 0 ? basename.slice(dot + 1) : "";
}

export function classifyChunkKind(path: string): ChunkKind {
  const segments = pathSegments(path);
  const original = segments[segments.length - 1] ?? "";
  const base = original.toLowerCase();
  const dirs = segments.slice(0, -1).map((segment) => segment.toLowerCase());
  const inDir = (set: Set<string>) => dirs.some((dir) => set.has(dir));

  if (
    GENERATED_SUFFIXES.some((suffix) => base.endsWith(suffix)) ||
    base.includes(".generated.") ||
    base.startsWith("zz_generated") ||
    LOCK_FILES.has(base) ||
    inDir(GENERATED_DIRS)
  ) {
    return "generated";
  }
  if (
    inDir(MOCK_DIRS) ||
    base.startsWith("mock_") ||
    base.startsWith("mock-") ||
    base.startsWith("mock.") ||
    /[._-]mocks?\.[a-z0-9]+$/.test(base) ||
    /^Mock[A-Z]/.test(original)
  ) {
    return "mock";
  }
  if (
    base.endsWith("_test.go") ||
    /\.(test|spec)\.[a-z0-9]+$/.test(base) ||
    base.startsWith("test_") ||
    base.endsWith("_test.py") ||
    /(Test|Tests|IT)\.(java|kt|cs|scala)$/.test(original) ||
    inDir(TEST_DIRS)
  ) {
    return "test";
  }
  const extension = extensionOf(base);
  if (DOC_EXTENSIONS.has(extension) || inDir(DOC_DIRS)) return "docs";
  if (
    CONFIG_EXTENSIONS.has(extension) ||
    base.startsWith(".env") ||
    CONFIG_BASENAMES.has(base) ||
    base.startsWith("dockerfile.")
  ) {
    return "config";
  }
  return "source";
}

/** The first `depth` directory segments of `path`; "" for a root-level file. */
export function directoryOf(path: string, depth: number): string {
  return pathSegments(path).slice(0, -1).slice(0, depth).join("/");
}

/** Deepest directory depth (1..4) that still yields at most `maxGroups` groups. */
export function pickDirectoryDepth(paths: string[], maxGroups = 12): number {
  let best = 1;
  for (let depth = 1; depth <= 4; depth++) {
    const distinct = new Set(paths.map((path) => directoryOf(path, depth)));
    if (distinct.size <= maxGroups) best = depth;
  }
  return best;
}

/** Keeps the informative tail of a long path: "…/ports/mock_store.go". */
export function shortenPath(path: string, maxChars = 40): string {
  if (path.length <= maxChars) return path;
  const segments = pathSegments(path);
  const basename = segments[segments.length - 1] ?? path;
  if (segments.length > 2) {
    const tail = `…/${segments.slice(-2).join("/")}`;
    if (tail.length <= maxChars) return tail;
  }
  if (basename.length > maxChars) return `${basename.slice(0, maxChars - 1)}…`;
  return segments.length > 1 ? `…/${basename}` : basename;
}

const STOPWORDS = new Set(
  (
    "src lib libs internal pkg app apps main index mod util utils common cmd core base impl " +
    "go ts tsx js jsx mjs py java kt rb rs cs cpp json yaml yml md txt html css " +
    "func function return const let var import export package type interface struct class def " +
    "self this nil null undefined true false err new string int bool void public private " +
    "protected static async await else case break default value values item items " +
    "the and for with from that are was were has have not but you your can will all any its " +
    "into our out use used using when which who how what then than there these those also " +
    "such may should would could been being each more most other some only own same very " +
    "just about over under between both few why where here " +
    "get set expect expecter call calls run returns " +
    "bir bu ve ile için olan gibi daha çok olarak veya ama her şu ise kadar sonra önce göre " +
    "değil var yok"
  ).split(" "),
);

export function tokenize(text: string): string[] {
  const out: string[] = [];
  const spaced = text
    .replace(/([\p{Ll}\p{N}])(\p{Lu})/gu, "$1 $2")
    .replace(/(\p{Lu})(\p{Lu}\p{Ll})/gu, "$1 $2");
  for (const part of spaced.split(/[^\p{L}\p{N}]+/u)) {
    if (part === "") continue;
    const token = part.toLowerCase();
    if (token.length < 3 || /^\p{N}+$/u.test(token) || STOPWORDS.has(token)) continue;
    out.push(token);
  }
  return out;
}

export interface TopicTokenSource {
  path: string;
  symbol?: string;
  snippet?: string;
}

export interface Topic {
  cluster: number;
  /** Up to `maxTerms` distinctive terms, most distinctive first; may be empty. */
  terms: string[];
  size: number;
  /** Most common directory among the members; "" when none dominates or no dirs. */
  topDirectory: string;
  /** Share of members under `topDirectory`, in [0, 1]. */
  topDirectoryShare: number;
  /** Index of the member point nearest the cluster's mean position. */
  anchor: number;
}

function basenameStem(path: string): string {
  const segments = pathSegments(path);
  const base = segments[segments.length - 1] ?? "";
  const dot = base.lastIndexOf(".");
  return dot > 0 ? base.slice(0, dot) : base;
}

function topDirectoryAt(paths: string[], depth: number): { dir: string; count: number } {
  const counts = new Map<string, number>();
  for (const path of paths) {
    const dir = directoryOf(path, depth);
    if (dir !== "") counts.set(dir, (counts.get(dir) ?? 0) + 1);
  }
  let dir = "";
  let count = 0;
  for (const [candidate, value] of counts) {
    if (value > count || (value === count && candidate < dir)) {
      dir = candidate;
      count = value;
    }
  }
  return { dir, count };
}

function twinsOf(term: string): string[] {
  return term.endsWith("s") ? [term.slice(0, -1)] : [`${term}s`];
}

export function describeTopics(args: {
  clusters: Int32Array;
  positions: Float32Array;
  points: TopicTokenSource[];
  mode: "code" | "files";
  maxTerms?: number;
}): Topic[] {
  const { clusters, positions, points, mode, maxTerms = 3 } = args;
  const total = points.length;

  // Directory names are left out of code labels: they repeat across clusters
  // and the dominant directory is reported separately as topDirectory.
  const tokenSets = points.map((point) =>
    mode === "code"
      ? new Set([...tokenize(basenameStem(point.path)), ...tokenize(point.symbol ?? "")])
      : new Set([...tokenize(point.snippet ?? ""), ...tokenize(basenameStem(point.path))]),
  );

  const frequency = new Map<string, number>();
  for (const set of tokenSets) {
    for (const token of set) frequency.set(token, (frequency.get(token) ?? 0) + 1);
  }

  const members = new Map<number, number[]>();
  for (let i = 0; i < total; i++) {
    const cluster = clusters[i];
    if (cluster < 0) continue;
    const list = members.get(cluster);
    if (list) list.push(i);
    else members.set(cluster, [i]);
  }

  const topics: Topic[] = [];
  for (const cluster of [...members.keys()].sort((a, b) => a - b)) {
    const ids = members.get(cluster) as number[];
    const size = ids.length;

    const docFrequency = new Map<string, number>();
    for (const id of ids) {
      for (const token of tokenSets[id]) {
        docFrequency.set(token, (docFrequency.get(token) ?? 0) + 1);
      }
    }
    const minDf = size < 2 ? 1 : Math.max(2, Math.ceil(0.2 * size));
    const scored: { term: string; score: number }[] = [];
    for (const [term, df] of docFrequency) {
      if (df < minDf) continue;
      const global = frequency.get(term) as number;
      const score = (df / size) * Math.log(total / global);
      if (score > 0) scored.push({ term, score });
    }
    scored.sort((a, b) => b.score - a.score || (a.term < b.term ? -1 : a.term > b.term ? 1 : 0));
    const terms: string[] = [];
    for (const { term } of scored) {
      if (terms.length >= maxTerms) break;
      if (twinsOf(term).some((twin) => terms.includes(twin))) continue;
      terms.push(term);
    }

    const paths = ids.map((id) => points[id].path);
    let topDirectory = "";
    let topDirectoryShare = 0;
    let chosen = false;
    for (let depth = 4; depth >= 1; depth--) {
      const { dir, count } = topDirectoryAt(paths, depth);
      const share = count / size;
      if (share >= 0.5) {
        topDirectory = dir;
        topDirectoryShare = share;
        chosen = true;
        break;
      }
      if (depth === 1) {
        topDirectory = dir;
        topDirectoryShare = share;
      }
    }
    if (!chosen && topDirectory === "") topDirectoryShare = 0;

    let meanX = 0;
    let meanY = 0;
    for (const id of ids) {
      meanX += positions[id * 2];
      meanY += positions[id * 2 + 1];
    }
    meanX /= size;
    meanY /= size;
    let anchor = ids[0];
    let anchorDistance = Number.POSITIVE_INFINITY;
    for (const id of ids) {
      const dx = positions[id * 2] - meanX;
      const dy = positions[id * 2 + 1] - meanY;
      const d = dx * dx + dy * dy;
      if (d < anchorDistance) {
        anchorDistance = d;
        anchor = id;
      }
    }

    topics.push({ cluster, terms, size, topDirectory, topDirectoryShare, anchor });
  }
  return topics;
}

export interface KindShare {
  kind: ChunkKind;
  count: number;
  share: number;
}

/** One entry per kind in CHUNK_KINDS order. */
export function kindComposition(kinds: ChunkKind[]): KindShare[] {
  const counts = new Map<ChunkKind, number>();
  for (const kind of kinds) counts.set(kind, (counts.get(kind) ?? 0) + 1);
  return CHUNK_KINDS.map((kind) => {
    const count = counts.get(kind) ?? 0;
    return { kind, count, share: kinds.length === 0 ? 0 : count / kinds.length };
  });
}
