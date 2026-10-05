import { ChartScatter, Play, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type EmbeddingMapLocation,
  type EmbeddingMapPoint,
  type EmbeddingMapResponse,
  type EmbeddingMapSource,
  type EmbeddingMapSources,
} from "@/api";
import { EmbeddingGroupDetail } from "@/components/rag/EmbeddingGroupDetail";
import { EmbeddingMapLegend } from "@/components/rag/EmbeddingMapLegend";
import { EmbeddingMapSearchSummary } from "@/components/rag/EmbeddingMapSearchSummary";
import { EmbeddingMapSummary } from "@/components/rag/EmbeddingMapSummary";
import {
  EmbeddingScatterCanvas,
  type EmbeddingCanvasLabel,
  type EmbeddingCanvasMarker,
} from "@/components/rag/EmbeddingScatterCanvas";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useI18n } from "@/hooks/useI18n";
import { useTheme } from "@/hooks/useTheme";
import {
  buildGroupScale,
  clampLimit,
  clampMinDist,
  clampNeighbors,
  groupColorAt,
  otherGroupColor,
  DEFAULT_LIMIT,
  DEFAULT_MIN_DIST,
  DEFAULT_N_NEIGHBORS,
  MAX_LIMIT,
  MIN_LIMIT,
  OTHER_GROUP_ID,
  type EmbeddingMapProgressPhase,
  type EmbeddingMapWorkerRequest,
  type EmbeddingMapWorkerResponse,
} from "@/lib/embeddingMap";
import {
  groupMembers,
  nearestMembers,
  placeMarkers,
  topFiles,
  type EmbeddingMapSearch,
} from "@/lib/embeddingMapSearch";
import {
  CHUNK_KINDS,
  classifyChunkKind,
  describeTopics,
  directoryOf,
  kindComposition,
  pickDirectoryDepth,
  shortenPath,
  type ChunkKind,
} from "@/lib/embeddingMapTopics";

const SNIPPET_MAX = 220;

type ColorBy = "topic" | "directory" | "kind" | "language" | "file";

const CODE_COLOR_MODES: readonly ColorBy[] = ["topic", "directory", "kind", "language", "file"];
const FILES_COLOR_MODES: readonly ColorBy[] = ["topic", "file"];
const MONO_COLOR_MODES: readonly ColorBy[] = ["directory", "file"];
const KIND_ORDER = CHUNK_KINDS.map((kind) => `kind:${kind}`);
const NOISE_GROUP_ID = "topic:noise";
const NEUTRAL_GROUP_IDS: ReadonlySet<string> = new Set([NOISE_GROUP_ID]);

/** Canvas text needs a literal color, like SURFACE_COLOR. */
const LABEL_COLOR = { light: "#1c1c21", dark: "#ececf1" } as const;

/** Card surface per theme — the canvas needs a literal color, not a CSS var. */
const SURFACE_COLOR = { light: "#fbfbfc", dark: "#222228" } as const;

interface UmapParams {
  nNeighbors: number;
  minDist: number;
  limit: number;
}

const DEFAULT_PARAMS: UmapParams = {
  nNeighbors: DEFAULT_N_NEIGHBORS,
  minDist: DEFAULT_MIN_DIST,
  limit: DEFAULT_LIMIT,
};

interface ProjectionProgress {
  phase: EmbeddingMapProgressPhase;
  ratio: number;
}

function truncateSnippet(snippet: string): string {
  const trimmed = snippet.trim();
  if (trimmed.length <= SNIPPET_MAX) return trimmed;
  return `${trimmed.slice(0, SNIPPET_MAX)}…`;
}

const DETAIL_SAMPLES = 3;
const DETAIL_FILES = 5;

export interface EmbeddingMapPanelProps {
  search?: EmbeddingMapSearch | null;
  onClearSearch?: () => void;
}

export function EmbeddingMapPanel({ search = null, onClearSearch }: EmbeddingMapPanelProps) {
  const { t } = useI18n();
  const { theme } = useTheme();

  const [sources, setSources] = useState<EmbeddingMapSources | null>(null);
  const [sourcesLoading, setSourcesLoading] = useState(true);
  const [sourcesError, setSourcesError] = useState<string | null>(null);

  const [source, setSource] = useState<EmbeddingMapSource>("files");
  const [repositoryId, setRepositoryId] = useState("");

  const [draft, setDraft] = useState<UmapParams>(DEFAULT_PARAMS);
  const [applied, setApplied] = useState<UmapParams>(DEFAULT_PARAMS);

  const [mapData, setMapData] = useState<EmbeddingMapResponse | null>(null);
  const [dataLoading, setDataLoading] = useState(false);
  const [dataError, setDataError] = useState<string | null>(null);

  const [layout, setLayout] = useState<{ positions: Float32Array; clusters: Int32Array } | null>(null);
  const [colorBy, setColorBy] = useState<ColorBy>("topic");
  const [projecting, setProjecting] = useState(false);
  const [progress, setProgress] = useState<ProjectionProgress | null>(null);
  const [projectionError, setProjectionError] = useState<string | null>(null);

  const [hoverGroupId, setHoverGroupId] = useState<string | null>(null);
  const [pinnedGroupId, setPinnedGroupId] = useState<string | null>(null);

  const [locations, setLocations] = useState<EmbeddingMapLocation[] | null>(null);
  const [locating, setLocating] = useState(false);
  const [reindexing, setReindexing] = useState(false);

  const fetchIdRef = useRef(0);
  const locateIdRef = useRef(0);
  const switchedForSearchRef = useRef<EmbeddingMapSearch | null>(null);
  const projectionIdRef = useRef(0);
  const workerRef = useRef<Worker | null>(null);
  const nNeighborsId = useId();
  const minDistId = useId();
  const limitId = useId();

  const repositories = useMemo(() => sources?.repositories ?? [], [sources]);
  const filesAvailable = sources?.files.available === true;
  const hasAnySource = filesAvailable || repositories.length > 0;

  /* ---------------------------------------------------------------- sources */

  const loadSources = useCallback(async () => {
    setSourcesLoading(true);
    setSourcesError(null);
    try {
      const data = await api.getEmbeddingMapSources();
      setSources(data);
    } catch (e) {
      const message = e instanceof Error ? e.message : t("content.embeddingMap.sourcesFailed");
      setSourcesError(message);
      toast.error(message);
    } finally {
      setSourcesLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadSources();
  }, [loadSources]);

  // Pick a sensible default source once we know what exists, and keep the
  // repository selection valid if the list changes underneath us.
  useEffect(() => {
    if (!sources) return;
    const repos = sources.repositories ?? [];
    if (!sources.files.available && repos.length > 0 && source === "files") {
      setSource("code");
    }
    if (repos.length === 0) {
      setRepositoryId("");
      return;
    }
    if (!repositoryId || !repos.some((repo) => repo.id === repositoryId)) {
      setRepositoryId(repos[0].id);
    }
  }, [sources, source, repositoryId]);

  /* ------------------------------------------------------------------ fetch */

  const loadMap = useCallback(async () => {
    if (source === "code" && !repositoryId) {
      setMapData(null);
      return;
    }
    if (source === "files" && !filesAvailable) {
      setMapData(null);
      return;
    }
    const fetchId = ++fetchIdRef.current;
    setDataLoading(true);
    setDataError(null);
    try {
      const data = await api.getEmbeddingMap({
        source,
        repositoryId: source === "code" ? repositoryId : undefined,
        limit: applied.limit,
      });
      if (fetchId !== fetchIdRef.current) return; // a newer request won
      setMapData(data);
    } catch (e) {
      if (fetchId !== fetchIdRef.current) return;
      const message = e instanceof Error ? e.message : t("content.embeddingMap.loadFailed");
      setDataError(message);
      setMapData(null);
      setLayout(null);
      toast.error(message);
    } finally {
      if (fetchId === fetchIdRef.current) setDataLoading(false);
    }
  }, [source, repositoryId, applied.limit, filesAvailable, t]);

  useEffect(() => {
    if (!sources) return;
    void loadMap();
  }, [sources, loadMap]);

  // A pinned group from the previous dataset would dim every point in the new
  // one, so drop the highlight whenever the chunks change.
  useEffect(() => {
    setHoverGroupId(null);
    setPinnedGroupId(null);
  }, [mapData]);

  /* ------------------------------------------------------------- projection */

  useEffect(() => {
    const points = mapData?.points ?? [];
    // Any change replaces the running projection: kill the old worker first so
    // nothing leaks and no stale layout can land.
    workerRef.current?.terminate();
    workerRef.current = null;
    setLayout(null);
    setProgress(null);
    setProjectionError(null);

    if (points.length === 0) {
      setProjecting(false);
      return;
    }

    const projectionId = ++projectionIdRef.current;
    const dims = points[0].vector?.length ?? 0;
    if (dims === 0) {
      setProjecting(false);
      setProjectionError(t("content.embeddingMap.missingVectors"));
      return;
    }

    const vectors = new Float32Array(points.length * dims);
    for (let i = 0; i < points.length; i++) {
      const vector = points[i].vector ?? [];
      for (let d = 0; d < dims; d++) {
        const value = vector[d];
        vectors[i * dims + d] = typeof value === "number" && Number.isFinite(value) ? value : 0;
      }
    }

    const worker = new Worker(new URL("./embeddingMap.worker.ts", import.meta.url), {
      type: "module",
    });
    workerRef.current = worker;
    setProjecting(true);
    setProgress({ phase: "neighbors", ratio: 0 });

    worker.onmessage = (event: MessageEvent<EmbeddingMapWorkerResponse>) => {
      const message = event.data;
      if (message.requestId !== projectionIdRef.current) return; // stale result
      if (message.type === "progress") {
        setProgress({ phase: message.phase, ratio: message.ratio });
        return;
      }
      if (message.type === "error") {
        setProjecting(false);
        setProgress(null);
        setProjectionError(message.message);
        toast.error(t("content.embeddingMap.projectionFailed"));
        return;
      }
      setLayout({ positions: message.positions, clusters: message.clusters });
      setProjecting(false);
      setProgress(null);
    };

    worker.onerror = (event) => {
      if (projectionId !== projectionIdRef.current) return;
      setProjecting(false);
      setProgress(null);
      setProjectionError(event.message || t("content.embeddingMap.projectionFailed"));
      toast.error(t("content.embeddingMap.projectionFailed"));
    };

    const request: EmbeddingMapWorkerRequest = {
      requestId: projectionId,
      vectors,
      count: points.length,
      dims,
      nNeighbors: clampNeighbors(applied.nNeighbors, points.length),
      minDist: clampMinDist(applied.minDist),
    };
    worker.postMessage(request, [vectors.buffer]);

    return () => {
      worker.terminate();
      if (workerRef.current === worker) workerRef.current = null;
    };
  }, [mapData, applied.nNeighbors, applied.minDist, t]);

  useEffect(
    () => () => {
      workerRef.current?.terminate();
      workerRef.current = null;
    },
    [],
  );

  /* ----------------------------------------------------------------- derived */

  const points: EmbeddingMapPoint[] = useMemo(() => mapData?.points ?? [], [mapData]);

  const readyLayout = layout && layout.clusters.length === points.length ? layout : null;
  const code = source === "code";
  const allowedModes = code ? CODE_COLOR_MODES : FILES_COLOR_MODES;
  const activeColorBy: ColorBy = allowedModes.includes(colorBy) ? colorBy : "topic";

  const kinds = useMemo<ChunkKind[]>(
    () => (code ? points.map((point) => classifyChunkKind(point.group_label)) : []),
    [code, points],
  );

  const topics = useMemo(
    () =>
      readyLayout
        ? describeTopics({
            clusters: readyLayout.clusters,
            positions: readyLayout.positions,
            points: points.map((point) => ({
              path: point.group_label,
              symbol: point.symbol,
              snippet: point.snippet,
            })),
            mode: source,
          })
        : [],
    [readyLayout, points, source],
  );

  // Clusters that earn the same label are one topic split across islands (a
  // big generated file, typically), so they share a group, a color and a label.
  const topicLabelByCluster = useMemo(() => {
    const labels = new Map<number, string>();
    for (const topic of topics) {
      labels.set(
        topic.cluster,
        topic.terms.join(" · ") ||
          (topic.topDirectory
            ? shortenPath(topic.topDirectory, 28)
            : t("content.embeddingMap.topicFallback", { n: topic.cluster + 1 })),
      );
    }
    return labels;
  }, [topics, t]);

  const topicCount = useMemo(() => new Set(topicLabelByCluster.values()).size, [topicLabelByCluster]);

  const topicByCluster = useMemo(() => new Map(topics.map((topic) => [topic.cluster, topic])), [topics]);

  const entries = useMemo(() => {
    const shareLabel = (share: number) =>
      t("content.embeddingMap.legendShare", { pct: Math.round(share * 100) });
    const paths = points.map((point) => point.group_label);
    const depth = activeColorBy === "directory" ? pickDirectoryDepth(paths) : 1;
    return points.map((point, index) => {
      switch (activeColorBy) {
        case "topic": {
          const cluster = readyLayout?.clusters[index] ?? -1;
          if (cluster < 0) {
            return { groupId: NOISE_GROUP_ID, label: t("content.embeddingMap.noiseLabel") };
          }
          const topic = topicByCluster.get(cluster);
          const sublabel =
            code && topic && topic.topDirectory && topic.topDirectoryShare >= 0.5
              ? `${shortenPath(topic.topDirectory, 22)} · ${shareLabel(topic.topDirectoryShare)}`
              : undefined;
          const label = topicLabelByCluster.get(cluster) ?? "";
          return { groupId: `topic:${label}`, label, sublabel };
        }
        case "directory": {
          const dir = directoryOf(point.group_label, depth);
          return { groupId: `dir:${dir}`, label: dir || t("content.embeddingMap.rootDirectory") };
        }
        case "kind":
          return {
            groupId: `kind:${kinds[index]}`,
            label: t(`content.embeddingMap.kinds.${kinds[index]}`),
          };
        case "language":
          return {
            groupId: `lang:${point.language || "unknown"}`,
            label: point.language || t("content.embeddingMap.unknownLanguage"),
          };
        case "file":
          return { groupId: point.group_id, label: point.group_label };
      }
    });
  }, [points, activeColorBy, readyLayout, topicByCluster, topicLabelByCluster, kinds, code, t]);

  const scale = useMemo(
    () =>
      buildGroupScale(entries, theme, {
        neutralGroupIds: activeColorBy === "topic" ? NEUTRAL_GROUP_IDS : undefined,
        order: activeColorBy === "kind" ? KIND_ORDER : undefined,
        foldOverflow: activeColorBy !== "topic",
      }),
    [entries, activeColorBy, theme],
  );

  const pointGroupIds = useMemo(() => entries.map((entry) => entry.groupId), [entries]);

  const pointColors = useMemo(
    () => pointGroupIds.map((groupId) => scale.colorByGroupId.get(groupId) ?? "#8a8a94"),
    [pointGroupIds, scale],
  );

  const otherPointCount = useMemo(
    () => pointGroupIds.reduce((sum, id) => (scale.otherGroupIds.has(id) ? sum + 1 : sum), 0),
    [pointGroupIds, scale],
  );

  const selectedRepo = useMemo(
    () => (code ? repositories.find((repo) => repo.id === repositoryId) : undefined),
    [code, repositories, repositoryId],
  );

  const kindColors = useMemo(
    () =>
      Object.fromEntries(CHUNK_KINDS.map((kind, i) => [kind, groupColorAt(i, theme)])) as Record<
        ChunkKind,
        string
      >,
    [theme],
  );

  const canvasLabels = useMemo<EmbeddingCanvasLabel[] | undefined>(() => {
    if (!readyLayout || activeColorBy !== "topic") return undefined;
    const byLabel = new Map<string, EmbeddingCanvasLabel & { anchorSize: number }>();
    for (const topic of topics) {
      const text = topicLabelByCluster.get(topic.cluster) ?? "";
      const existing = byLabel.get(text);
      const anchor = {
        x: readyLayout.positions[topic.anchor * 2],
        y: readyLayout.positions[topic.anchor * 2 + 1],
      };
      if (!existing) {
        byLabel.set(text, { groupId: `topic:${text}`, text, ...anchor, weight: topic.size, anchorSize: topic.size });
        continue;
      }
      existing.weight += topic.size;
      if (topic.size > existing.anchorSize) Object.assign(existing, anchor, { anchorSize: topic.size });
    }
    return [...byLabel.values()].map(({ anchorSize: _, ...label }) => label);
  }, [readyLayout, activeColorBy, topics, topicLabelByCluster]);

  const composition = useMemo(() => (code ? kindComposition(kinds) : null), [code, kinds]);

  const clusteredShare = useMemo(() => {
    if (!readyLayout || points.length === 0) return 0;
    let inTopic = 0;
    for (const cluster of readyLayout.clusters) if (cluster >= 0) inTopic++;
    return inTopic / points.length;
  }, [readyLayout, points.length]);

  const activeGroupId = hoverGroupId ?? pinnedGroupId;

  const paramsDirty =
    draft.nNeighbors !== applied.nNeighbors ||
    draft.minDist !== applied.minDist ||
    draft.limit !== applied.limit;

  const applyParams = useCallback(() => {
    setApplied({
      nNeighbors: Math.max(2, Math.min(200, Math.round(draft.nNeighbors))),
      minDist: clampMinDist(draft.minDist),
      limit: clampLimit(draft.limit),
    });
  }, [draft]);

  const handleSourceChange = useCallback((next: string) => {
    setSource(next === "code" ? "code" : "files");
    setHoverGroupId(null);
    setPinnedGroupId(null);
  }, []);

  const handleColorByChange = useCallback((next: string) => {
    setColorBy(CODE_COLOR_MODES.find((mode) => mode === next) ?? "topic");
    setHoverGroupId(null);
    setPinnedGroupId(null);
  }, []);

  const handleRepositoryChange = useCallback((next: string) => {
    setRepositoryId(next);
    setHoverGroupId(null);
    setPinnedGroupId(null);
  }, []);

  const toggleGroup = useCallback((groupId: string) => {
    setPinnedGroupId((prev) => (prev === groupId ? null : groupId));
  }, []);

  // Only the search itself moves the map; afterwards the user may pick another
  // repository without being pulled back.
  useEffect(() => {
    if (!search || switchedForSearchRef.current === search) return;
    if (!repositories.some((repo) => repo.id === search.repositoryId)) return;
    switchedForSearchRef.current = search;
    if (source !== "code") handleSourceChange("code");
    if (repositoryId !== search.repositoryId) handleRepositoryChange(search.repositoryId);
  }, [search, repositories, source, repositoryId, handleSourceChange, handleRepositoryChange]);

  const searchShown = Boolean(search && code && repositoryId === search.repositoryId);
  const pointIndexById = useMemo(() => new Map(points.map((point, i) => [point.id, i])), [points]);

  useEffect(() => {
    const locateId = ++locateIdRef.current;
    setLocations(null);
    if (!search || !searchShown || !readyLayout || points.length === 0) {
      setLocating(false);
      return;
    }
    setLocating(true);
    api
      .locateEmbeddingMapChunks(
        search.repositoryId,
        points.map((point) => point.id),
        search.hits.map((hit) => hit.id),
      )
      .then((res) => {
        if (locateId !== locateIdRef.current) return;
        setLocations(res.locations ?? []);
      })
      .catch(() => {
        if (locateId !== locateIdRef.current) return;
        setLocations([]);
        toast.error(t("content.embeddingMap.search.failed"));
      })
      .finally(() => {
        if (locateId === locateIdRef.current) setLocating(false);
      });
  }, [search, searchShown, readyLayout, points, t]);

  const anchorIndexByHitId = useMemo(() => {
    const map = new Map<string, number>();
    for (const location of locations ?? []) {
      const index = pointIndexById.get(location.anchor_id);
      if (index !== undefined) map.set(location.chunk_id, index);
    }
    return map;
  }, [locations, pointIndexById]);

  const placedMarkers = useMemo(
    () =>
      search && searchShown && readyLayout
        ? placeMarkers(search.hits, anchorIndexByHitId, readyLayout.positions)
        : [],
    [search, searchShown, readyLayout, anchorIndexByHitId],
  );

  const canvasMarkers = useMemo<EmbeddingCanvasMarker[] | undefined>(
    () =>
      placedMarkers.length > 0
        ? placedMarkers.map((marker) => ({ x: marker.x, y: marker.y, label: String(marker.rank) }))
        : undefined,
    [placedMarkers],
  );

  const searchGroups = useMemo(() => {
    const counts = new Map<string, { label: string; count: number }>();
    for (const marker of placedMarkers) {
      const index = anchorIndexByHitId.get(marker.hitId);
      if (index === undefined) continue;
      const id = pointGroupIds[index];
      const existing = counts.get(id);
      if (existing) existing.count++;
      else counts.set(id, { label: entries[index].label, count: 1 });
    }
    return [...counts]
      .map(([id, group]) => ({
        id,
        label: group.label,
        count: group.count,
        color: scale.colorByGroupId.get(id) ?? "#8a8a94",
      }))
      .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
  }, [placedMarkers, anchorIndexByHitId, pointGroupIds, entries, scale]);

  const searchGroupIds = useMemo(
    () => (searchGroups.length > 0 ? new Set(searchGroups.map((group) => group.id)) : null),
    [searchGroups],
  );

  const highlightedGroupIds = useMemo(() => {
    if (!activeGroupId) return searchGroupIds;
    if (activeGroupId === OTHER_GROUP_ID) return scale.otherGroupIds;
    return new Set([activeGroupId]);
  }, [activeGroupId, scale, searchGroupIds]);

  const handlePointClick = useCallback(
    (index: number) => {
      const groupId = pointGroupIds[index];
      if (groupId !== undefined) setPinnedGroupId((prev) => (prev === groupId ? null : groupId));
    },
    [pointGroupIds],
  );

  const groupDetail = useMemo(() => {
    if (!readyLayout || !pinnedGroupId || pinnedGroupId === OTHER_GROUP_ID) return null;
    const members = groupMembers(pointGroupIds, pinnedGroupId);
    if (members.length === 0) return null;
    const cluster = readyLayout.clusters[members[0]] ?? -1;
    const topic = activeColorBy === "topic" && cluster >= 0 ? topicByCluster.get(cluster) : undefined;
    return {
      title: entries[members[0]].label,
      subtitle: topic?.topDirectory || undefined,
      color: scale.colorByGroupId.get(pinnedGroupId) ?? "#8a8a94",
      count: members.length,
      share: t("content.embeddingMap.legendShare", {
        pct: Math.round((members.length / points.length) * 100),
      }),
      files: topFiles(
        points.map((point) => point.group_label),
        members,
        DETAIL_FILES,
      ),
      samples: nearestMembers(readyLayout.positions, members, DETAIL_SAMPLES).map((i) => ({
        path: points[i].group_label,
        symbol: points[i].symbol || undefined,
        snippet: points[i].snippet,
      })),
    };
  }, [readyLayout, pinnedGroupId, pointGroupIds, entries, points, scale, topicByCluster, activeColorBy, t]);

  const handleReindex = useCallback(async () => {
    if (!repositoryId) return;
    setReindexing(true);
    try {
      await api.reindexRepository(repositoryId);
      toast.success(t("content.embeddingMap.summary.reindexStarted"));
    } catch (e) {
      toast.error(t("content.embeddingMap.summary.reindexFailed"), {
        description: e instanceof Error ? e.message : undefined,
      });
    } finally {
      setReindexing(false);
    }
  }, [repositoryId, t]);

  const renderTooltip = useCallback(
    (index: number) => {
      const point = points[index];
      if (!point) return null;
      const cluster = readyLayout?.clusters[index] ?? -1;
      const meta = [
        cluster >= 0 ? topicLabelByCluster.get(cluster) : undefined,
        code ? t(`content.embeddingMap.kinds.${kinds[index]}`) : undefined,
        point.language,
        point.symbol,
      ]
        .filter((value) => Boolean(value))
        .join(" · ");
      return (
        <div className="space-y-1">
          <p className="break-all font-mono text-xs font-medium">{point.group_label}</p>
          {meta && <p className="text-micro text-muted-foreground">{meta}</p>}
          <pre className="whitespace-pre-wrap break-words font-mono text-micro leading-snug text-foreground/80">
            {truncateSnippet(point.snippet)}
          </pre>
        </div>
      );
    },
    [points, readyLayout, topicLabelByCluster, code, kinds, t],
  );

  /* ------------------------------------------------------------------ render */

  if (sourcesLoading) {
    return (
      <Card className="space-y-4 p-6">
        <Skeleton className="h-6 w-56" />
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-64 w-full" />
      </Card>
    );
  }

  if (sourcesError) {
    return (
      <Card className="p-6">
        <EmptyState
          icon={ChartScatter}
          title={t("content.embeddingMap.sourcesFailed")}
          description={sourcesError}
          action={
            <Button onClick={() => void loadSources()} className="gap-2">
              <RefreshCw className="h-4 w-4" />
              {t("content.embeddingMap.retry")}
            </Button>
          }
        />
      </Card>
    );
  }

  if (!hasAnySource) {
    return (
      <Card className="p-6">
        <EmptyState
          icon={ChartScatter}
          title={t("content.embeddingMap.emptyTitle")}
          description={t("content.embeddingMap.emptyDescription")}
          action={
            <Button asChild variant="outline">
              <Link to="/repositories">{t("content.embeddingMap.goToRepositories")}</Link>
            </Button>
          }
        />
      </Card>
    );
  }

  const progressPercent = progress
    ? progress.phase === "neighbors"
      ? 4
      : progress.phase === "clusters"
        ? 100
        : Math.max(4, Math.round(progress.ratio * 100))
    : 0;

  const busy = dataLoading || projecting;

  const mapReady = !dataError && !busy && readyLayout !== null && points.length > 0 && mapData !== null;

  return (
    <Card className="space-y-5 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-heading font-semibold">{t("content.embeddingMap.title")}</h2>
          <p className="text-caption text-muted-foreground">{t("content.embeddingMap.subtitle")}</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void loadMap()} disabled={busy} className="gap-2">
          <RefreshCw className="h-4 w-4" />
          {t("content.embeddingMap.refresh")}
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <div className="space-y-2">
          <Label>{t("content.embeddingMap.sourceLabel")}</Label>
          <Select value={source} onValueChange={handleSourceChange}>
            <SelectTrigger aria-label={t("content.embeddingMap.sourceLabel")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="files" disabled={!filesAvailable}>
                {t("content.embeddingMap.sourceFiles")}
              </SelectItem>
              <SelectItem value="code" disabled={repositories.length === 0}>
                {t("content.embeddingMap.sourceCode")}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        {code && (
          <div className="space-y-2">
            <Label>{t("content.embeddingMap.repositoryLabel")}</Label>
            <Select value={repositoryId} onValueChange={handleRepositoryChange}>
              <SelectTrigger aria-label={t("content.embeddingMap.repositoryLabel")}>
                <SelectValue placeholder={t("content.embeddingMap.selectRepository")} />
              </SelectTrigger>
              <SelectContent>
                {repositories.map((repo) => (
                  <SelectItem key={repo.id} value={repo.id}>
                    {repo.branch ? `${repo.name} · ${repo.branch}` : repo.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}

        <div className="space-y-2">
          <Label>{t("content.embeddingMap.colorByLabel")}</Label>
          <Select value={activeColorBy} onValueChange={handleColorByChange}>
            <SelectTrigger aria-label={t("content.embeddingMap.colorByLabel")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {allowedModes.map((mode) => (
                <SelectItem key={mode} value={mode}>
                  {t(`content.embeddingMap.colorBy.${mode}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      {busy && (
        <div className="space-y-2" aria-live="polite">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Spinner size="sm" />
            <span>
              {dataLoading
                ? t("content.embeddingMap.loading")
                : progress?.phase === "neighbors"
                  ? t("content.embeddingMap.progressNeighbors")
                  : progress?.phase === "clusters"
                    ? t("content.embeddingMap.progressClusters")
                    : t("content.embeddingMap.progressLayout", { percent: progressPercent })}
            </span>
          </div>
          {!dataLoading && (
            <div className="h-2 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary transition-all duration-200"
                style={{ width: `${progressPercent}%` }}
              />
            </div>
          )}
        </div>
      )}

      {dataError && !busy && (
        <EmptyState
          icon={ChartScatter}
          title={t("content.embeddingMap.loadFailed")}
          description={dataError}
          action={
            <Button onClick={() => void loadMap()} className="gap-2">
              <RefreshCw className="h-4 w-4" />
              {t("content.embeddingMap.retry")}
            </Button>
          }
        />
      )}

      {!dataError && !busy && points.length === 0 && (
        <EmptyState
          icon={ChartScatter}
          title={t("content.embeddingMap.emptyPointsTitle")}
          description={t("content.embeddingMap.emptyPointsDescription")}
        />
      )}

      {!dataError && !busy && projectionError && points.length > 0 && (
        <EmptyState
          icon={ChartScatter}
          title={t("content.embeddingMap.projectionFailed")}
          description={projectionError}
          action={
            <Button onClick={() => void loadMap()} className="gap-2">
              <RefreshCw className="h-4 w-4" />
              {t("content.embeddingMap.retry")}
            </Button>
          }
        />
      )}

      {mapReady && readyLayout && mapData && (
        <>
          <EmbeddingMapSummary
            source={source}
            totalChunks={mapData.total}
            fileCount={code ? (selectedRepo?.file_count ?? null) : (sources?.files.document_count ?? null)}
            topicCount={topicCount}
            clusteredShare={clusteredShare}
            indexedAt={selectedRepo?.indexed_at}
            branch={selectedRepo?.branch || mapData.branch}
            embeddingModel={selectedRepo?.embedding_model}
            composition={composition}
            kindColors={kindColors}
            stale={Boolean(mapData.embedding_stale || selectedRepo?.embedding_stale)}
            staleDetail={mapData.embedding_warning || selectedRepo?.embedding_warning}
            onColorByKind={activeColorBy === "kind" ? undefined : () => handleColorByChange("kind")}
            onReindex={code && repositoryId ? () => void handleReindex() : undefined}
            reindexing={reindexing}
          />

          {search && searchShown && (
            <EmbeddingMapSearchSummary
              query={search.query}
              total={search.hits.length}
              placed={placedMarkers.length}
              groups={searchGroups}
              locating={locating}
              onClear={onClearSearch}
            />
          )}

          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
            <EmbeddingScatterCanvas
              positions={readyLayout.positions}
              colors={pointColors}
              groupIds={pointGroupIds}
              highlightedGroupIds={highlightedGroupIds}
              surfaceColor={SURFACE_COLOR[theme]}
              labels={canvasLabels}
              labelColor={LABEL_COLOR[theme]}
              ariaLabel={t("content.embeddingMap.canvasLabel", {
                count: points.length,
                groups: scale.groups.length,
              })}
              ariaDescription={t("content.embeddingMap.canvasDescription")}
              viewportLabel={t("content.embeddingMap.viewportLabel")}
              zoomInLabel={t("content.embeddingMap.zoomIn")}
              zoomOutLabel={t("content.embeddingMap.zoomOut")}
              resetLabel={t("content.embeddingMap.resetView")}
              renderTooltip={renderTooltip}
              markers={canvasMarkers}
              onPointClick={handlePointClick}
            />
            <EmbeddingMapLegend
              legend={scale.legend}
              otherPointCount={otherPointCount}
              otherGroupCount={scale.otherGroupIds.size}
              otherColor={otherGroupColor(theme)}
              otherLabel={t("content.embeddingMap.legendOther", { count: scale.otherGroupIds.size })}
              activeGroupId={activeGroupId}
              pinnedGroupId={pinnedGroupId}
              onHoverGroup={setHoverGroupId}
              onToggleGroup={toggleGroup}
              title={t(`content.embeddingMap.legendTitles.${activeColorBy}`)}
              hint={t("content.embeddingMap.legendHint")}
              total={points.length}
              formatShare={(pct) => t("content.embeddingMap.legendShare", { pct })}
              mono={MONO_COLOR_MODES.includes(activeColorBy)}
            />
          </div>

          {groupDetail && (
            <EmbeddingGroupDetail
              {...groupDetail}
              onClose={() => setPinnedGroupId(null)}
              labels={{
                files: t("content.embeddingMap.detail.files"),
                samples: t("content.embeddingMap.detail.samples"),
                close: t("content.embeddingMap.detail.close"),
                chunks: (count) => t("content.embeddingMap.detail.chunks", { count }),
              }}
            />
          )}

          <p className="text-xs text-muted-foreground">
            {mapData.truncated &&
              `${t("content.embeddingMap.truncatedNote", {
                sampled: mapData.sampled,
                total: mapData.total,
              })} `}
            {t("content.embeddingMap.viewportHint")}
          </p>
        </>
      )}

      <Accordion type="single" collapsible>
        <AccordionItem value="advanced">
          <AccordionTrigger>{t("content.embeddingMap.advancedTitle")}</AccordionTrigger>
          <AccordionContent className="space-y-4">
            <div className="grid gap-4 md:grid-cols-3">
              <div className="space-y-2">
                <Label htmlFor={nNeighborsId}>{t("content.embeddingMap.nNeighborsLabel")}</Label>
                <Input
                  id={nNeighborsId}
                  type="number"
                  min={2}
                  max={200}
                  step={1}
                  value={draft.nNeighbors}
                  onChange={(e) =>
                    setDraft((prev) => ({ ...prev, nNeighbors: Number(e.target.value) || 0 }))
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor={minDistId}>{t("content.embeddingMap.minDistLabel")}</Label>
                <Input
                  id={minDistId}
                  type="number"
                  min={0.001}
                  max={0.99}
                  step={0.01}
                  value={draft.minDist}
                  onChange={(e) => setDraft((prev) => ({ ...prev, minDist: Number(e.target.value) || 0 }))}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor={limitId}>{t("content.embeddingMap.limitLabel")}</Label>
                <Input
                  id={limitId}
                  type="number"
                  min={MIN_LIMIT}
                  max={MAX_LIMIT}
                  step={100}
                  value={draft.limit}
                  onChange={(e) => setDraft((prev) => ({ ...prev, limit: Number(e.target.value) || 0 }))}
                />
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <Button onClick={applyParams} disabled={!paramsDirty || busy} className="gap-2">
                <Play className="h-4 w-4" />
                {t("content.embeddingMap.reproject")}
              </Button>
              <p className="text-xs text-muted-foreground">{t("content.embeddingMap.paramsHint")}</p>
            </div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </Card>
  );
}
