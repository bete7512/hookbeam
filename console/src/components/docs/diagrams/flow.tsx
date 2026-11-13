"use client";

import { Background, BackgroundVariant, ControlButton, Controls, Handle, MarkerType, Position, ReactFlow, useReactFlow, useStore, type Edge, type Node, type BuiltInEdge, type NodeTypes } from "@xyflow/react";
import { Maximize2, Minimize2 } from "lucide-react";
import { Fragment, useEffect, useState } from "react";

import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

export type Side = "top" | "right" | "bottom" | "left";

// A port is a point on a node's side: its middle, or a third of the way along (`-1`) or two thirds (`-2`), so two
// edges between the same pair of nodes can run side by side.
export type Port = Side | `${Side}-1` | `${Side}-2`;

const positions: Record<Side, Position> = { top: Position.Top, right: Position.Right, bottom: Position.Bottom, left: Position.Left };
const offsets = [
  { suffix: "", at: "50%" },
  { suffix: "-1", at: "30%" },
  { suffix: "-2", at: "70%" },
];

// SideHandles gives a node an outgoing and an incoming handle at every port; edges name them `<port>-out` and
// `<port>-in`, so a hand-placed layout also chooses where each line leaves and arrives.
export function SideHandles() {
  return (
    <>
      {(Object.keys(positions) as Side[]).flatMap((side) =>
        offsets.map(({ suffix, at }) => {
          const style = side === "top" || side === "bottom" ? { left: at } : { top: at };
          return (
            <Fragment key={side + suffix}>
              <Handle type="source" id={`${side}${suffix}-out`} position={positions[side]} style={style} className="docs-handle" isConnectable={false} />
              <Handle type="target" id={`${side}${suffix}-in`} position={positions[side]} style={style} className="docs-handle" isConnectable={false} />
            </Fragment>
          );
        }),
      )}
    </>
  );
}

export interface Link {
  id: string;
  source: string;
  target: string;
  label?: string;
  sourceSide?: Port;
  targetSide?: Port;
  offset?: number; // how far the path stands off its nodes before turning; the default is 20
}

export type EdgeLook = "normal" | "active" | "dim" | "link";

// edge turns a hand-written connection into a React Flow edge with the docs' look.
export function edge(l: Link, look: EdgeLook = "normal"): Edge {
  const color = look === "active" ? "var(--primary)" : "var(--docs-edge)";
  const e: BuiltInEdge = {
    id: l.id,
    source: l.source,
    target: l.target,
    sourceHandle: `${l.sourceSide ?? "right"}-out`,
    targetHandle: `${l.targetSide ?? "left"}-in`,
    type: "smoothstep",
    pathOptions: l.offset ? { offset: l.offset } : undefined,
    label: l.label,
    animated: look === "active",
    className: look === "normal" ? undefined : `docs-edge-${look}`,
    markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16, color },
    labelBgPadding: [6, 3],
    labelBgBorderRadius: 4,
    focusable: false,
  };
  return e;
}

// AutoFit keeps the whole diagram in view when its container changes width (a resized window, the enlarged view).
function AutoFit({ padding }: { padding: number }) {
  const { fitView } = useReactFlow();
  const width = useStore((s) => s.width);
  useEffect(() => {
    void fitView({ padding });
  }, [width, fitView, padding]);
  return null;
}

export function Flow({
  nodes,
  edges,
  nodeTypes,
  label,
  className,
  padding = 0.06,
  onNodeClick,
}: {
  nodes: Node[];
  edges: Edge[];
  nodeTypes: NodeTypes;
  label: string;
  className?: string;
  padding?: number;
  onNodeClick?: (id: string) => void;
}) {
  const theme = useTheme();
  const [enlarged, setEnlarged] = useState(false);

  useEffect(() => {
    if (!enlarged) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setEnlarged(false);
    };
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = overflow;
      document.removeEventListener("keydown", onKey);
    };
  }, [enlarged]);

  return (
    <>
      {enlarged && <div aria-hidden className="fixed inset-0 z-40 bg-background/80 backdrop-blur-sm" onClick={() => setEnlarged(false)} />}
      <div
        className={cn("docs-flow overflow-hidden rounded-xl border", enlarged ? "fixed inset-3 z-50 shadow-2xl md:inset-6" : cn("relative h-[440px] md:h-[560px]", className))}
        aria-label={label}
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          colorMode={theme}
          fitView
          fitViewOptions={{ padding }}
          minZoom={0.2}
          maxZoom={2}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={Boolean(onNodeClick)}
          nodesFocusable={Boolean(onNodeClick)}
          edgesFocusable={false}
          zoomOnScroll={false}
          zoomOnDoubleClick={false}
          preventScrolling={false}
          defaultMarkerColor="var(--docs-edge)"
          onNodeClick={onNodeClick ? (_, node) => onNodeClick(node.id) : undefined}
          ariaLabelConfig={{ "node.a11yDescription.default": "Press Enter to read about this part of the diagram." }}
        >
          <Background variant={BackgroundVariant.Dots} gap={22} size={1} />
          <Controls showInteractive={false} position="bottom-left">
            <ControlButton
              onClick={() => setEnlarged((e) => !e)}
              aria-label={enlarged ? "Close the enlarged diagram" : "Enlarge the diagram"}
              title={enlarged ? "Close (Esc)" : "Enlarge"}
            >
              {/* React Flow fills control icons; Lucide's are drawn with strokes. */}
              {enlarged ? <Minimize2 aria-hidden style={{ fill: "none" }} /> : <Maximize2 aria-hidden style={{ fill: "none" }} />}
            </ControlButton>
          </Controls>
          <AutoFit padding={padding} />
        </ReactFlow>
      </div>
    </>
  );
}

// Chips are the keyboard and screen-reader way into a diagram: one button per part, same selection as a click.
export function Chips({ items, selected, onSelect, label }: { items: { id: string; label: string }[]; selected: string | null; onSelect: (id: string) => void; label: string }) {
  return (
    <div role="group" aria-label={label} className="mt-3 flex flex-wrap gap-1.5">
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          aria-pressed={selected === item.id}
          onClick={() => onSelect(item.id)}
          className={cn(
            "rounded-full border px-2.5 py-1 font-mono text-[11px] transition-colors",
            selected === item.id ? "border-primary bg-primary/10 text-foreground" : "text-muted-foreground hover:border-primary/50 hover:text-foreground",
          )}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
