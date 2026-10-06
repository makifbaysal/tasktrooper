import {
  Background,
  Controls,
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  useNodesState,
  useReactFlow,
  type Connection,
  type EdgeChange,
  type NodeTypes,
  type EdgeTypes,
} from "@xyflow/react";
import { LayoutGrid } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { BoardColumn } from "@/api";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/useI18n";
import { findWorkflowHubs, layoutWorkflow } from "@/lib/workflowGraphLayout";
import { cn } from "@/lib/utils";
import {
  WorkflowStatusNode,
  type WorkflowStatusCategory,
  type WorkflowStatusFlowNode,
} from "./WorkflowStatusNode";
import { WorkflowTransitionEdge, type WorkflowTransitionFlowEdge } from "./WorkflowTransitionEdge";

export type TransitionMap = Record<string, Set<string>>;

const nodeTypes: NodeTypes = { status: WorkflowStatusNode };
const edgeTypes: EdgeTypes = { transition: WorkflowTransitionEdge };

function categoryOf(slug: string): WorkflowStatusCategory {
  if (slug === "backlog" || slug === "todo") return "todo";
  if (slug === "done" || slug === "released") return "done";
  if (slug === "blocked") return "blocked";
  if (slug === "need_revision") return "revision";
  return "active";
}

const edgeId = (from: string, to: string) => `${from}->${to}`;

function flatten(transitions: TransitionMap) {
  return Object.entries(transitions).flatMap(([from, tos]) => [...tos].map((to) => ({ from, to })));
}

interface TransitionGraphProps {
  columns: BoardColumn[];
  transitions: TransitionMap;
  onChange: (next: TransitionMap) => void;
}

/**
 * The board's transition rules as a Jira-style workflow graph: drag from a
 * status's right edge onto another to allow that move, select an arrow to
 * remove it, select a status to edit its targets as a list.
 */
export function TransitionGraph(props: TransitionGraphProps) {
  return (
    <ReactFlowProvider>
      <TransitionGraphInner {...props} />
    </ReactFlowProvider>
  );
}

function TransitionGraphInner({ columns, transitions, onChange }: TransitionGraphProps) {
  const { t } = useI18n();
  const { fitView } = useReactFlow();
  const sorted = useMemo(() => [...columns].sort((a, b) => a.position - b.position), [columns]);
  const [selectedSlug, setSelectedSlug] = useState<string | null>(null);
  const [selectedEdge, setSelectedEdge] = useState<string | null>(null);

  // Laid out once per set of columns, not on every edit: a node that jumped
  // each time an arrow was added would be impossible to keep editing.
  const transitionsRef = useRef(transitions);
  transitionsRef.current = transitions;
  const columnKey = sorted.map((c) => c.slug).join("|");
  const layoutNodes = useCallback(
    (): WorkflowStatusFlowNode[] => {
      const positions = layoutWorkflow(sorted, flatten(transitionsRef.current));
      return sorted.map((c) => ({
        id: c.slug,
        type: "status",
        position: positions.get(c.slug) ?? { x: 0, y: 0 },
        deletable: false,
        data: { label: c.label, category: categoryOf(c.slug), free: false, inbound: null, dimmed: false },
      }));
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [columnKey],
  );
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowStatusFlowNode>(layoutNodes());

  useEffect(() => {
    setNodes(layoutNodes());
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutNodes]);

  const relayout = () => {
    setNodes(layoutNodes());
    window.requestAnimationFrame(() => void fitView({ padding: 0.12, duration: 200 }));
  };

  const pairs = useMemo(() => flatten(transitions), [transitions]);
  const pairSet = useMemo(() => new Set(pairs.map((p) => edgeId(p.from, p.to))), [pairs]);
  const hubs = useMemo(() => findWorkflowHubs(sorted, pairs), [sorted, pairs]);
  const inbound = useMemo(() => {
    const counts = new Map<string, number>();
    for (const p of pairs) if (hubs.has(p.to)) counts.set(p.to, (counts.get(p.to) ?? 0) + 1);
    return counts;
  }, [pairs, hubs]);

  const removeTransition = useCallback(
    (id: string) => {
      const [from, to] = id.split("->");
      const next: TransitionMap = { ...transitions, [from]: new Set(transitions[from] ?? []) };
      next[from].delete(to);
      onChange(next);
      setSelectedEdge(null);
    },
    [transitions, onChange],
  );

  const edges: WorkflowTransitionFlowEdge[] = useMemo(
    () =>
      pairs.map(({ from, to }) => {
        const id = edgeId(from, to);
        const touches = selectedSlug === null || from === selectedSlug || to === selectedSlug;
        // Arrows into a hub are summarised on the hub ("from N statuses") and
        // drawn only for the status in focus.
        const hidden = hubs.has(to) && selectedSlug !== from && selectedSlug !== to && id !== selectedEdge;
        return {
          id,
          source: from,
          target: to,
          type: "transition",
          hidden,
          selected: id === selectedEdge,
          markerEnd: {
            type: MarkerType.ArrowClosed,
            width: 16,
            height: 16,
            color: id === selectedEdge ? "var(--color-primary)" : "var(--color-muted-foreground)",
          },
          data: { paired: pairSet.has(edgeId(to, from)), dimmed: !touches, onRemove: removeTransition },
        };
      }),
    [pairs, pairSet, hubs, selectedSlug, selectedEdge, removeTransition],
  );

  const neighbours = useMemo(() => {
    if (!selectedSlug) return null;
    const out = new Set<string>([selectedSlug]);
    for (const p of pairs) {
      if (p.from === selectedSlug) out.add(p.to);
      if (p.to === selectedSlug) out.add(p.from);
    }
    return out;
  }, [pairs, selectedSlug]);

  const shownNodes = useMemo(
    () =>
      nodes.map((n) => ({
        ...n,
        selected: n.id === selectedSlug,
        data: {
          ...n.data,
          free: (transitions[n.id]?.size ?? 0) === 0,
          inbound: hubs.has(n.id) ? (inbound.get(n.id) ?? 0) : null,
          dimmed: neighbours !== null && !neighbours.has(n.id),
        },
      })),
    [nodes, transitions, neighbours, selectedSlug, hubs, inbound],
  );

  const onConnect = (c: Connection) => {
    if (!c.source || !c.target || c.source === c.target) return;
    const next: TransitionMap = { ...transitions, [c.source]: new Set(transitions[c.source] ?? []) };
    next[c.source].add(c.target);
    onChange(next);
  };

  const onEdgesChange = (changes: EdgeChange<WorkflowTransitionFlowEdge>[]) => {
    for (const change of changes) {
      if (change.type === "select") setSelectedEdge(change.selected ? change.id : null);
      if (change.type === "remove") removeTransition(change.id);
    }
  };

  const toggle = (from: string, to: string) => {
    const next: TransitionMap = { ...transitions, [from]: new Set(transitions[from] ?? []) };
    if (next[from].has(to)) next[from].delete(to);
    else next[from].add(to);
    onChange(next);
  };

  const selected = sorted.find((c) => c.slug === selectedSlug) ?? null;
  const selectedTargets = selected ? (transitions[selected.slug] ?? new Set<string>()) : null;

  return (
    <div className="space-y-3">
      <div className="relative h-[480px] overflow-hidden rounded-lg border border-border bg-muted/10">
        <ReactFlow
          nodes={shownNodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onNodeClick={(_, node) => {
            setSelectedSlug(node.id);
            setSelectedEdge(null);
          }}
          onPaneClick={() => {
            setSelectedSlug(null);
            setSelectedEdge(null);
          }}
          connectionRadius={36}
          deleteKeyCode={["Backspace", "Delete"]}
          fitView
          fitViewOptions={{ padding: 0.12 }}
          minZoom={0.3}
          proOptions={{ hideAttribution: true }}
        >
          <Background gap={24} />
          <Controls showInteractive={false} />
        </ReactFlow>
        <Button variant="outline" size="sm" className="absolute right-3 top-3 z-10 h-8 gap-1.5 bg-background" onClick={relayout}>
          <LayoutGrid className="h-3.5 w-3.5" />
          {t("settingsPages.board.graph.autoLayout")}
        </Button>
      </div>

      {selected && selectedTargets ? (
        <div className="rounded-lg border border-border p-3">
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <p className="text-sm font-medium">{t("settingsPages.board.graph.targetsOf", { column: selected.label })}</p>
            {selectedTargets.size > 0 && (
              <Button
                variant="ghost"
                size="sm"
                className="h-7"
                onClick={() => onChange({ ...transitions, [selected.slug]: new Set() })}
              >
                {t("settingsPages.board.graph.makeFree")}
              </Button>
            )}
          </div>
          <div className="flex flex-wrap gap-1.5">
            {selectedTargets.size === 0 && (
              <span className="rounded-full bg-muted px-2.5 py-1 text-xs text-muted-foreground">
                {t("settingsPages.board.freeToAnywhere")}
              </span>
            )}
            {sorted
              .filter((c) => c.slug !== selected.slug)
              .map((to) => {
                const on = selectedTargets.has(to.slug);
                return (
                  <button
                    key={to.slug}
                    type="button"
                    aria-pressed={on}
                    onClick={() => toggle(selected.slug, to.slug)}
                    className={cn(
                      "rounded-full border px-2.5 py-1 text-xs transition-colors",
                      on
                        ? "border-primary bg-primary/10 font-medium text-primary"
                        : "border-border text-muted-foreground hover:bg-muted",
                    )}
                  >
                    {to.label}
                  </button>
                );
              })}
          </div>
        </div>
      ) : (
        <p className="text-caption text-muted-foreground">
          {t("settingsPages.board.graph.hint")}
          {hubs.size > 0 &&
            ` ${t("settingsPages.board.graph.hubHint", {
              columns: sorted
                .filter((c) => hubs.has(c.slug))
                .map((c) => c.label)
                .join(", "),
            })}`}
        </p>
      )}
    </div>
  );
}
