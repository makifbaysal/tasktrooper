import { BaseEdge, EdgeLabelRenderer, getBezierPath, type Edge, type EdgeProps } from "@xyflow/react";
import { useState } from "react";
import { useI18n } from "@/hooks/useI18n";
import type { MapEdgeData } from "@/lib/architectureMapLayout";
import { cn } from "@/lib/utils";

/**
 * Confirmed edges are solid; a suggestion (awaiting a human's Confirm/
 * Dismiss) is dashed in the warning color; a cross-project edge is thicker
 * and info-colored so it reads as leaving the project even before its
 * protocol label is visible. The protocol label itself only renders on
 * hover or selection — with the review queue and dashed style already
 * saying "unconfirmed", a label on every edge at once would be noise.
 */
export function MapEdgeLine({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  selected,
  markerEnd,
}: EdgeProps<Edge<MapEdgeData>>) {
  const { t } = useI18n();
  const [hovered, setHovered] = useState(false);
  const edge = data?.edge;
  const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition });
  if (!edge) return null;

  const suggested = edge.status === "suggested";
  const highlighted = Boolean(selected || data?.highlighted);
  const dimmed = Boolean(data?.dimmed) && !highlighted;
  const showLabel = highlighted || hovered;

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        interactionWidth={20}
        className={cn("transition-opacity", dimmed && "opacity-20")}
        style={{
          strokeDasharray: suggested ? "6 4" : undefined,
          stroke: suggested
            ? "var(--color-warning)"
            : edge.cross_project
              ? "var(--color-info)"
              : highlighted
                ? "var(--color-ring)"
                : undefined,
          strokeWidth: highlighted ? 2.5 : edge.cross_project ? 2 : undefined,
        }}
      />
      {/* BaseEdge's own interactionWidth widens the click/select target but
          emits no DOM events of its own — this transparent path is what the
          hover-to-reveal-the-label affordance listens on. */}
      <path
        d={path}
        fill="none"
        stroke="transparent"
        strokeWidth={16}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      />
      {showLabel && (
        <EdgeLabelRenderer>
          <div
            className="pointer-events-none absolute rounded-md border border-border bg-popover px-1.5 py-0.5 text-micro text-popover-foreground shadow-[var(--shadow-raised)]"
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
          >
            {t(`projectModel.linkProtocols.${edge.protocol}`)}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
