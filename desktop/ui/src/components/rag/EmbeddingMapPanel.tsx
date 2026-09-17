import { ChartScatter, Play, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";
import {
  api,
  type EmbeddingMapPoint,
  type EmbeddingMapResponse,
  type EmbeddingMapSource,
  type EmbeddingMapSources,
} from "@/api";
import { EmbeddingMapLegend } from "@/components/rag/EmbeddingMapLegend";
import { EmbeddingScatterCanvas } from "@/components/rag/EmbeddingScatterCanvas";
import { Badge } from "@/components/ui/badge";
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
  otherGroupColor,
  DEFAULT_LIMIT,
  DEFAULT_MIN_DIST,
  DEFAULT_N_NEIGHBORS,
  MAX_LIMIT,
  MIN_LIMIT,
  OTHER_GROUP_ID,
  type EmbeddingMapWorkerRequest,
  type EmbeddingMapWorkerResponse,
} from "@/lib/embeddingMap";

const SNIPPET_MAX = 220;

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
  phase: "neighbors" | "layout";
  ratio: number;
}

function truncateSnippet(snippet: string): string {
  const trimmed = snippet.trim();
  if (trimmed.length <= SNIPPET_MAX) return trimmed;
  return `${trimmed.slice(0, SNIPPET_MAX)}…`;
}

export function EmbeddingMapPanel() {
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

  const [positions, setPositions] = useState<Float32Array | null>(null);
  const [projecting, setProjecting] = useState(false);
  const [progress, setProgress] = useState<ProjectionProgress | null>(null);
  const [projectionError, setProjectionError] = useState<string | null>(null);

  const [hoverGroupId, setHoverGroupId] = useState<string | null>(null);
  const [pinnedGroupId, setPinnedGroupId] = useState<string | null>(null);

  const fetchIdRef = useRef(0);
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
      setPositions(null);
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
    setPositions(null);
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
      setPositions(message.positions);
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

  const scale = useMemo(
    () =>
      buildGroupScale(
        points.map((point) => ({ groupId: point.group_id, label: point.group_label })),
        theme,
      ),
    [points, theme],
  );

  const pointColors = useMemo(
    () => points.map((point) => scale.colorByGroupId.get(point.group_id) ?? "#8a8a94"),
    [points, scale],
  );

  const pointGroupIds = useMemo(() => points.map((point) => point.group_id), [points]);

  const otherPointCount = useMemo(
    () => points.reduce((sum, point) => (scale.otherGroupIds.has(point.group_id) ? sum + 1 : sum), 0),
    [points, scale],
  );

  const activeGroupId = hoverGroupId ?? pinnedGroupId;

  const highlightedGroupIds = useMemo(() => {
    if (!activeGroupId) return null;
    if (activeGroupId === OTHER_GROUP_ID) return scale.otherGroupIds;
    return new Set([activeGroupId]);
  }, [activeGroupId, scale]);

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

  const handleRepositoryChange = useCallback((next: string) => {
    setRepositoryId(next);
    setHoverGroupId(null);
    setPinnedGroupId(null);
  }, []);

  const toggleGroup = useCallback((groupId: string) => {
    setPinnedGroupId((prev) => (prev === groupId ? null : groupId));
  }, []);

  const renderTooltip = useCallback(
    (index: number) => {
      const point = points[index];
      if (!point) return null;
      const meta = [point.language, point.symbol].filter((value) => Boolean(value)).join(" · ");
      return (
        <div className="space-y-1">
          <p className="break-all font-mono text-xs font-medium">{point.group_label}</p>
          <p className="text-[11px] text-muted-foreground">
            {t("content.embeddingMap.tooltipChunk", { index: point.chunk_index })}
            {meta ? ` · ${meta}` : ""}
          </p>
          <pre className="whitespace-pre-wrap break-words font-mono text-[10px] leading-snug text-foreground/80">
            {truncateSnippet(point.snippet)}
          </pre>
        </div>
      );
    },
    [points, t],
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
      : Math.max(4, Math.round(progress.ratio * 100))
    : 0;

  const busy = dataLoading || projecting;

  return (
    <Card className="space-y-5 p-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-heading font-semibold">{t("content.embeddingMap.title")}</h2>
          <p className="text-caption text-muted-foreground">{t("content.embeddingMap.subtitle")}</p>
        </div>
        {mapData && (
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">
              {t("content.embeddingMap.countsLabel", {
                sampled: mapData.sampled,
                total: mapData.total,
              })}
            </Badge>
            <Badge variant="outline">
              {t("content.embeddingMap.groupCount", { count: scale.groups.length })}
            </Badge>
          </div>
        )}
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
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

        {source === "code" && (
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
        <Button
          variant="outline"
          onClick={() => void loadMap()}
          disabled={busy}
          className="gap-2"
        >
          <RefreshCw className="h-4 w-4" />
          {t("content.embeddingMap.refresh")}
        </Button>
        <p className="text-xs text-muted-foreground">{t("content.embeddingMap.paramsHint")}</p>
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

      {mapData?.truncated && !busy && (
        <p className="text-xs text-muted-foreground">
          {t("content.embeddingMap.truncatedNote", {
            sampled: mapData.sampled,
            total: mapData.total,
          })}
        </p>
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

      {!dataError && positions && points.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
          <EmbeddingScatterCanvas
            positions={positions}
            colors={pointColors}
            groupIds={pointGroupIds}
            highlightedGroupIds={highlightedGroupIds}
            surfaceColor={SURFACE_COLOR[theme]}
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
            title={t("content.embeddingMap.legendTitle")}
            hint={t("content.embeddingMap.legendHint")}
          />
        </div>
      )}

      {!dataError && positions && points.length > 0 && (
        <p className="text-xs text-muted-foreground">{t("content.embeddingMap.viewportHint")}</p>
      )}
    </Card>
  );
}
