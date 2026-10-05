import { Maximize, ZoomIn, ZoomOut } from "lucide-react";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { Button } from "@/components/ui/button";
import { PointGrid } from "@/lib/embeddingMap";
import { cn } from "@/lib/utils";

const PADDING_PX = 18;
const HIT_RADIUS_PX = 9;
const MIN_SCALE = 0.4;
const MAX_SCALE = 24;
const KEYBOARD_PAN_PX = 48;
const MARKER_FIT_MIN_PX = 40;
const MARKER_FIT_MAX_SCALE = 6;

interface View {
  scale: number;
  tx: number;
  ty: number;
}

interface PlotMetrics {
  spanX: number;
  spanY: number;
}

function plotMetrics(width: number, height: number): PlotMetrics {
  return {
    spanX: Math.max(1, width - PADDING_PX * 2),
    spanY: Math.max(1, height - PADDING_PX * 2),
  };
}

export interface EmbeddingCanvasMarker {
  /** Normalized [0, 1] like `positions`. */
  x: number;
  y: number;
  label: string;
}

const CLICK_SLOP_PX = 4;
const MARKER_FONT = '700 10px "Inter Variable", Inter, system-ui, sans-serif';

export interface EmbeddingCanvasLabel {
  groupId: string;
  text: string;
  /** Normalized [0, 1] like `positions`. */
  x: number;
  y: number;
  weight: number;
}

const LABEL_FONT = '600 12px "Inter Variable", Inter, system-ui, sans-serif';
const LABEL_MAX_CHARS = 28;
const LABEL_MAX_DRAWN = 16;
const LABEL_OFFSET_PX = 12;

export interface EmbeddingScatterCanvasProps {
  /** `2 * n` interleaved x/y pairs normalized into [0, 1]. */
  positions: Float32Array;
  /** Per-point fill color, parallel to `positions`. */
  colors: string[];
  /** Per-point group id, parallel to `positions`. */
  groupIds: string[];
  /** When set, points outside the set are dimmed. */
  highlightedGroupIds: Set<string> | null;
  /** Card surface color, used for the ring drawn around the hovered mark. */
  surfaceColor: string;
  ariaLabel: string;
  ariaDescription: string;
  viewportLabel: string;
  zoomInLabel: string;
  zoomOutLabel: string;
  resetLabel: string;
  /** Tooltip body for the hovered point index. */
  renderTooltip: (index: number) => ReactNode;
  /** Direct labels drawn over the plot, heaviest first. */
  labels?: EmbeddingCanvasLabel[];
  labelColor?: string;
  /** Numbered rings drawn over the points, e.g. code-search hits. */
  markers?: EmbeddingCanvasMarker[];
  /** Fires on a click that did not drag, with the point under the cursor. */
  onPointClick?: (index: number) => void;
  className?: string;
}

export function EmbeddingScatterCanvas({
  positions,
  colors,
  groupIds,
  highlightedGroupIds,
  surfaceColor,
  ariaLabel,
  ariaDescription,
  viewportLabel,
  zoomInLabel,
  zoomOutLabel,
  resetLabel,
  renderTooltip,
  labels,
  labelColor = "#1c1c21",
  markers,
  onPointClick,
  className,
}: EmbeddingScatterCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<View>({ scale: 1, tx: 0, ty: 0 });
  const frameRef = useRef<number | null>(null);
  const drawRef = useRef<(() => void) | null>(null);
  const draggingRef = useRef(false);
  const dragOriginRef = useRef({ x: 0, y: 0 });
  const downRef = useRef({ x: 0, y: 0 });
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);
  const descriptionId = useId();

  const grid = useMemo(() => new PointGrid(positions), [positions]);
  const pointCount = positions.length / 2;

  const requestDraw = useCallback(() => {
    if (frameRef.current !== null) return;
    frameRef.current = requestAnimationFrame(() => {
      frameRef.current = null;
      drawRef.current?.();
    });
  }, []);

  const resetView = useCallback(() => {
    viewRef.current = { scale: 1, tx: 0, ty: 0 };
    requestDraw();
  }, [requestDraw]);

  // A fresh projection always starts from the default viewport.
  useEffect(() => {
    resetView();
  }, [positions, resetView]);

  // Search hits tend to land in one region, where their rings and numbers
  // overlap at the default zoom, so new markers bring that region into view.
  useEffect(() => {
    const rect = containerRef.current?.getBoundingClientRect();
    if (!markers || markers.length === 0 || !rect || rect.width === 0) return;
    const { spanX, spanY } = plotMetrics(rect.width, rect.height);
    let minX = Number.POSITIVE_INFINITY;
    let maxX = Number.NEGATIVE_INFINITY;
    let minY = Number.POSITIVE_INFINITY;
    let maxY = Number.NEGATIVE_INFINITY;
    for (const marker of markers) {
      const x = PADDING_PX + marker.x * spanX;
      const y = PADDING_PX + marker.y * spanY;
      minX = Math.min(minX, x);
      maxX = Math.max(maxX, x);
      minY = Math.min(minY, y);
      maxY = Math.max(maxY, y);
    }
    const width = Math.max(maxX - minX, MARKER_FIT_MIN_PX);
    const height = Math.max(maxY - minY, MARKER_FIT_MIN_PX);
    const scale = Math.max(
      1,
      Math.min(MARKER_FIT_MAX_SCALE, rect.width / (width * 2.5), rect.height / (height * 2.5)),
    );
    viewRef.current = {
      scale,
      tx: rect.width / 2 - ((minX + maxX) / 2) * scale,
      ty: rect.height / 2 - ((minY + maxY) / 2) * scale,
    };
    requestDraw();
  }, [markers, requestDraw]);

  const zoomAt = useCallback(
    (factor: number, anchorX: number, anchorY: number) => {
      const view = viewRef.current;
      const next = Math.max(MIN_SCALE, Math.min(MAX_SCALE, view.scale * factor));
      const applied = next / view.scale;
      viewRef.current = {
        scale: next,
        tx: anchorX - (anchorX - view.tx) * applied,
        ty: anchorY - (anchorY - view.ty) * applied,
      };
      requestDraw();
    },
    [requestDraw],
  );

  const zoomFromCenter = useCallback(
    (factor: number) => {
      const rect = containerRef.current?.getBoundingClientRect();
      zoomAt(factor, (rect?.width ?? 0) / 2, (rect?.height ?? 0) / 2);
    },
    [zoomAt],
  );

  // Keep the drawing closure fresh without re-subscribing listeners.
  useEffect(() => {
    drawRef.current = () => {
      const canvas = canvasRef.current;
      const ctx = canvas?.getContext("2d");
      if (!canvas || !ctx) return;

      const dpr = window.devicePixelRatio || 1;
      const width = canvas.width / dpr;
      const height = canvas.height / dpr;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, width, height);
      if (pointCount === 0) return;

      const { spanX, spanY } = plotMetrics(width, height);
      const { scale, tx, ty } = viewRef.current;
      const radius = Math.max(1.7, Math.min(5.5, 2.6 * Math.sqrt(scale)));

      // Batch by color so 5000 points cost a handful of fills, not 5000.
      const dimmed = new Map<string, number[]>();
      const bright = new Map<string, number[]>();
      for (let i = 0; i < pointCount; i++) {
        const isDim = highlightedGroupIds !== null && !highlightedGroupIds.has(groupIds[i]);
        const bucket = isDim ? dimmed : bright;
        const color = colors[i] ?? "#888888";
        const list = bucket.get(color);
        if (list) list.push(i);
        else bucket.set(color, [i]);
      }

      const paint = (batches: Map<string, number[]>, alpha: number, r: number) => {
        ctx.globalAlpha = alpha;
        for (const [color, indices] of batches) {
          ctx.fillStyle = color;
          ctx.beginPath();
          for (const i of indices) {
            const x = (PADDING_PX + positions[i * 2] * spanX) * scale + tx;
            const y = (PADDING_PX + positions[i * 2 + 1] * spanY) * scale + ty;
            if (x < -r || y < -r || x > width + r || y > height + r) continue;
            ctx.moveTo(x + r, y);
            ctx.arc(x, y, r, 0, Math.PI * 2);
          }
          ctx.fill();
        }
      };

      paint(dimmed, 0.1, radius);
      paint(bright, highlightedGroupIds ? 0.95 : 0.82, radius);
      ctx.globalAlpha = 1;

      if (hoverIndex !== null && hoverIndex < pointCount) {
        const x = (PADDING_PX + positions[hoverIndex * 2] * spanX) * scale + tx;
        const y = (PADDING_PX + positions[hoverIndex * 2 + 1] * spanY) * scale + ty;
        // 2px surface ring keeps the hovered mark readable over a dense cluster.
        ctx.lineWidth = 2;
        ctx.strokeStyle = surfaceColor;
        ctx.beginPath();
        ctx.arc(x, y, radius + 3, 0, Math.PI * 2);
        ctx.stroke();
        ctx.lineWidth = 1.5;
        ctx.strokeStyle = colors[hoverIndex] ?? "#888888";
        ctx.beginPath();
        ctx.arc(x, y, radius + 4.5, 0, Math.PI * 2);
        ctx.stroke();
      }

      if (markers && markers.length > 0) {
        ctx.font = MARKER_FONT;
        ctx.textAlign = "left";
        ctx.textBaseline = "middle";
        ctx.lineJoin = "round";
        for (const marker of markers) {
          const x = (PADDING_PX + marker.x * spanX) * scale + tx;
          const y = (PADDING_PX + marker.y * spanY) * scale + ty;
          if (x < -20 || y < -20 || x > width + 20 || y > height + 20) continue;
          ctx.lineWidth = 2;
          ctx.strokeStyle = surfaceColor;
          ctx.beginPath();
          ctx.arc(x, y, radius + 5, 0, Math.PI * 2);
          ctx.stroke();
          ctx.strokeStyle = labelColor;
          ctx.beginPath();
          ctx.arc(x, y, radius + 6.5, 0, Math.PI * 2);
          ctx.stroke();
          ctx.lineWidth = 3;
          ctx.strokeStyle = surfaceColor;
          ctx.strokeText(marker.label, x + 9, y - 9);
          ctx.fillStyle = labelColor;
          ctx.fillText(marker.label, x + 9, y - 9);
        }
      }

      if (labels && labels.length > 0) {
        ctx.font = LABEL_FONT;
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.lineJoin = "round";
        ctx.lineWidth = 4;
        const placed: { left: number; right: number; top: number; bottom: number }[] = [];
        const ordered = [...labels].sort((a, b) => b.weight - a.weight);
        for (const label of ordered) {
          if (placed.length >= LABEL_MAX_DRAWN) break;
          if (highlightedGroupIds !== null && !highlightedGroupIds.has(label.groupId)) continue;
          const text =
            label.text.length > LABEL_MAX_CHARS ? `${label.text.slice(0, LABEL_MAX_CHARS - 1)}…` : label.text;
          const anchorX = (PADDING_PX + label.x * spanX) * scale + tx;
          const anchorY = (PADDING_PX + label.y * spanY) * scale + ty;
          if (anchorX < 0 || anchorX > width || anchorY < 0 || anchorY > height) continue;
          const halfW = (ctx.measureText(text).width + 8) / 2;
          // Sit just above the anchor so a tight cluster stays visible under its
          // label, and slide inward rather than clip at the plot's edges.
          const x = Math.min(Math.max(anchorX, halfW + 2), width - halfW - 2);
          const y = Math.min(Math.max(anchorY - LABEL_OFFSET_PX, 10), height - 10);
          const rect = { left: x - halfW, right: x + halfW, top: y - 8, bottom: y + 8 };
          if (placed.some((p) => rect.left < p.right && rect.right > p.left && rect.top < p.bottom && rect.bottom > p.top)) {
            continue;
          }
          placed.push(rect);
          ctx.strokeStyle = surfaceColor;
          ctx.strokeText(text, x, y);
          ctx.fillStyle = labelColor;
          ctx.fillText(text, x, y);
        }
      }
    };
    requestDraw();
  });

  // Resize with the container and stay sharp on retina.
  useEffect(() => {
    const container = containerRef.current;
    const canvas = canvasRef.current;
    if (!container || !canvas) return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      const dpr = window.devicePixelRatio || 1;
      const width = Math.max(1, Math.round(entry.contentRect.width));
      const height = Math.max(1, Math.round(entry.contentRect.height));
      canvas.width = Math.round(width * dpr);
      canvas.height = Math.round(height * dpr);
      canvas.style.width = `${width}px`;
      canvas.style.height = `${height}px`;
      requestDraw();
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, [requestDraw]);

  useEffect(
    () => () => {
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
      // requestDraw skips while a frame is pending; a cancelled frame left here
      // would silence every draw after a remount (StrictMode does exactly that).
      frameRef.current = null;
    },
    [],
  );

  // React marks onWheel passive, so preventDefault needs a native listener.
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = container.getBoundingClientRect();
      const factor = Math.pow(0.999, event.deltaY);
      zoomAt(factor, event.clientX - rect.left, event.clientY - rect.top);
    };
    container.addEventListener("wheel", onWheel, { passive: false });
    return () => container.removeEventListener("wheel", onWheel);
  }, [zoomAt]);

  const pointAt = useCallback(
    (clientX: number, clientY: number) => {
      const container = containerRef.current;
      if (!container) return null;
      const rect = container.getBoundingClientRect();
      const sx = clientX - rect.left;
      const sy = clientY - rect.top;
      const { spanX, spanY } = plotMetrics(rect.width, rect.height);
      const { scale, tx, ty } = viewRef.current;
      const dataX = ((sx - tx) / scale - PADDING_PX) / spanX;
      const dataY = ((sy - ty) / scale - PADDING_PX) / spanY;
      const radius = HIT_RADIUS_PX / (scale * Math.min(spanX, spanY));
      const found = grid.nearest(dataX, dataY, radius);
      return { index: found >= 0 ? found : null, sx, sy, rect };
    },
    [grid],
  );

  const updateHover = useCallback(
    (clientX: number, clientY: number) => {
      const hit = pointAt(clientX, clientY);
      if (!hit) return;
      const { index: next, sx, sy, rect } = hit;

      // Position imperatively — a tooltip that follows the cursor must not
      // re-render React on every mousemove.
      const tooltip = tooltipRef.current;
      if (tooltip && next !== null) {
        const offsetX = sx > rect.width - 260 ? sx - 250 : sx + 14;
        const offsetY = sy > rect.height - 140 ? sy - 130 : sy + 14;
        tooltip.style.transform = `translate(${Math.max(4, offsetX)}px, ${Math.max(4, offsetY)}px)`;
      }
      setHoverIndex((prev) => (prev === next ? prev : next));
    },
    [pointAt],
  );

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    draggingRef.current = true;
    dragOriginRef.current = { x: event.clientX, y: event.clientY };
    downRef.current = { x: event.clientX, y: event.clientY };
    event.currentTarget.setPointerCapture(event.pointerId);
    setHoverIndex(null);
  };

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (draggingRef.current) {
      const origin = dragOriginRef.current;
      const view = viewRef.current;
      viewRef.current = {
        scale: view.scale,
        tx: view.tx + (event.clientX - origin.x),
        ty: view.ty + (event.clientY - origin.y),
      };
      dragOriginRef.current = { x: event.clientX, y: event.clientY };
      requestDraw();
      return;
    }
    updateHover(event.clientX, event.clientY);
  };

  const endDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!draggingRef.current) return;
    draggingRef.current = false;
    if (
      onPointClick &&
      event.type === "pointerup" &&
      Math.hypot(event.clientX - downRef.current.x, event.clientY - downRef.current.y) < CLICK_SLOP_PX
    ) {
      const hit = pointAt(event.clientX, event.clientY);
      if (hit && hit.index !== null) onPointClick(hit.index);
    }
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const view = viewRef.current;
    const pan = (dx: number, dy: number) => {
      viewRef.current = { scale: view.scale, tx: view.tx + dx, ty: view.ty + dy };
      requestDraw();
    };
    switch (event.key) {
      case "ArrowLeft":
        pan(KEYBOARD_PAN_PX, 0);
        break;
      case "ArrowRight":
        pan(-KEYBOARD_PAN_PX, 0);
        break;
      case "ArrowUp":
        pan(0, KEYBOARD_PAN_PX);
        break;
      case "ArrowDown":
        pan(0, -KEYBOARD_PAN_PX);
        break;
      case "+":
      case "=":
        zoomFromCenter(1.25);
        break;
      case "-":
      case "_":
        zoomFromCenter(0.8);
        break;
      case "0":
        resetView();
        break;
      default:
        return;
    }
    event.preventDefault();
  };

  return (
    <div
      ref={containerRef}
      role="group"
      aria-label={viewportLabel}
      aria-describedby={descriptionId}
      tabIndex={0}
      className={cn(
        "relative h-[420px] w-full touch-none overflow-hidden rounded-lg border border-border bg-muted/20",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        className,
      )}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onPointerLeave={() => setHoverIndex(null)}
      onKeyDown={handleKeyDown}
    >
      <canvas
        ref={canvasRef}
        role="img"
        aria-label={ariaLabel}
        className="block h-full w-full cursor-crosshair"
      />
      <p id={descriptionId} className="sr-only">
        {ariaDescription}
      </p>

      <div
        className="absolute right-2 top-2 flex flex-col gap-1"
        onPointerDown={(event) => event.stopPropagation()}
      >
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="h-8 w-8 bg-background/80 backdrop-blur"
          aria-label={zoomInLabel}
          title={zoomInLabel}
          onClick={() => zoomFromCenter(1.25)}
        >
          <ZoomIn className="h-4 w-4" />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="h-8 w-8 bg-background/80 backdrop-blur"
          aria-label={zoomOutLabel}
          title={zoomOutLabel}
          onClick={() => zoomFromCenter(0.8)}
        >
          <ZoomOut className="h-4 w-4" />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon"
          className="h-8 w-8 bg-background/80 backdrop-blur"
          aria-label={resetLabel}
          title={resetLabel}
          onClick={resetView}
        >
          <Maximize className="h-4 w-4" />
        </Button>
      </div>

      <div
        ref={tooltipRef}
        aria-hidden={hoverIndex === null}
        className={cn(
          "pointer-events-none absolute left-0 top-0 z-10 max-w-[16rem] rounded-md border border-border bg-popover p-2.5 text-popover-foreground shadow-md",
          hoverIndex === null && "hidden",
        )}
      >
        {hoverIndex !== null && renderTooltip(hoverIndex)}
      </div>
    </div>
  );
}
