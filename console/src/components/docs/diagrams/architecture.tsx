"use client";

import type { Edge, Node, NodeTypes } from "@xyflow/react";
import { Building2, ChevronLeft, ChevronRight, Cog, Database, Inbox, LayoutDashboard, Mail, Monitor, Network, Pause, Play, Radio, Send, Server, Webhook, type LucideIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { components, connections, groups, lifecycle } from "@/components/docs/diagrams/architecture-data";
import { Chips, edge, Flow, type EdgeLook } from "@/components/docs/diagrams/flow";
import { GroupNode, SystemNode, type Emphasis } from "@/components/docs/diagrams/nodes";
import { Source } from "@/components/docs/prose";
import { cn } from "@/lib/utils";

const icons: Record<string, LucideIcon> = {
  sender: Building2,
  browser: Monitor,
  edge: Network,
  console: LayoutDashboard,
  api: Server,
  apiWorker: Cog,
  controlDb: Database,
  delivery: Server,
  deliveryWorker: Send,
  deliveryDb: Database,
  sns: Radio,
  controlQueue: Inbox,
  dataQueue: Inbox,
  mail: Mail,
  receiver: Webhook,
};

const nodeTypes: NodeTypes = { system: SystemNode, plane: GroupNode };

// graph builds the architecture with a highlight: the components and connections of one step stand out, the
// rest fade; with no highlight everything is drawn normally and `selected` rings one component.
function graph(highlight: { nodes: string[]; edges: string[] } | null, selected: string | null): { nodes: Node[]; edges: Edge[] } {
  const planes: Node[] = groups.map((g) => ({
    id: `plane-${g.id}`,
    type: "plane",
    position: { x: g.x, y: g.y },
    data: { label: g.label },
    style: { width: g.width, height: g.height },
    selectable: false,
    focusable: false,
    draggable: false,
  }));
  const parts: Node[] = components.map((c) => {
    let look: Emphasis = "normal";
    if (highlight) look = highlight.nodes.includes(c.id) ? "active" : "dim";
    else if (selected === c.id) look = "selected";
    return {
      id: c.id,
      type: "system",
      position: { x: c.x, y: c.y },
      parentId: c.group ? `plane-${c.group}` : undefined,
      data: { label: c.label, detail: c.detail, kind: c.kind, icon: icons[c.id] ?? Server, emphasis: look },
      ariaLabel: `${c.label}: ${c.detail}`,
    };
  });
  const lines = connections.map((c) => {
    let look: EdgeLook = "normal";
    if (highlight) look = highlight.edges.includes(c.id) ? "active" : "dim";
    return edge(c, look);
  });
  return { nodes: [...planes, ...parts], edges: lines };
}

// ArchitectureExplorer is the system diagram: select a component to read what it does, what it owns and where
// it lives in the repository.
export function ArchitectureExplorer() {
  const [selected, setSelected] = useState<string | null>("delivery");
  const { nodes, edges } = useMemo(() => graph(null, selected), [selected]);
  const part = components.find((c) => c.id === selected);
  const select = (id: string) => setSelected((cur) => (cur === id || !components.some((c) => c.id === id) ? cur : id));
  return (
    <figure className="docs-figure">
      <Flow nodes={nodes} edges={edges} nodeTypes={nodeTypes} label="Hookbeam's architecture" onNodeClick={select} />
      <Chips label="Components" items={components.map((c) => ({ id: c.id, label: c.label }))} selected={selected} onSelect={select} />
      {part && (
        <div className="mt-3 rounded-xl border bg-card p-4" aria-live="polite">
          <p className="text-sm font-semibold">{part.label}</p>
          <p className="mt-1 text-sm text-muted-foreground">{part.role}</p>
          {part.owns && (
            <p className="mt-2 text-sm">
              <span className="text-muted-foreground">Owns: </span>
              {part.owns.join(" · ")}
            </p>
          )}
          {part.code && (
            <p className="mt-3 flex flex-wrap items-center gap-2 text-sm">
              {part.code.map((p) => (
                <Source key={p} path={p} />
              ))}
            </p>
          )}
        </div>
      )}
      <figcaption>Select a component, on the diagram or in the list, to see its role. Arrows point the way a request or an event travels.</figcaption>
    </figure>
  );
}

const stepMillis = 5000;

// LifecyclePlayer steps one message through the same diagram: each step lights the components and connections
// involved. It plays only on request; the moving dashes stop for readers who prefer reduced motion.
export function LifecyclePlayer() {
  const [step, setStep] = useState(0);
  const [playing, setPlaying] = useState(false);
  const current = lifecycle[step];
  const { nodes, edges } = useMemo(() => graph({ nodes: current.nodes, edges: current.edges }, null), [current]);

  useEffect(() => {
    if (!playing) return;
    const timer = window.setTimeout(() => {
      if (step === lifecycle.length - 1) setPlaying(false);
      else setStep(step + 1);
    }, stepMillis);
    return () => window.clearTimeout(timer);
  }, [playing, step]);

  const go = (i: number) => {
    setPlaying(false);
    setStep(Math.max(0, Math.min(lifecycle.length - 1, i)));
  };

  return (
    <figure className="docs-figure">
      <Flow nodes={nodes} edges={edges} nodeTypes={nodeTypes} label={`Step ${step + 1}: ${current.title}`} />
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button type="button" onClick={() => go(step - 1)} disabled={step === 0} className="docs-control" aria-label="Previous step">
          <ChevronLeft aria-hidden className="size-4" />
        </button>
        <button
          type="button"
          onClick={() => {
            if (step === lifecycle.length - 1) setStep(0);
            setPlaying((p) => !p);
          }}
          className="docs-control px-3"
          aria-pressed={playing}
        >
          {playing ? <Pause aria-hidden className="size-4" /> : <Play aria-hidden className="size-4" />}
          <span className="text-xs">{playing ? "Pause" : "Play"}</span>
        </button>
        <button type="button" onClick={() => go(step + 1)} disabled={step === lifecycle.length - 1} className="docs-control" aria-label="Next step">
          <ChevronRight aria-hidden className="size-4" />
        </button>
        <span className="ml-1 text-xs text-muted-foreground">
          Step {step + 1} of {lifecycle.length}
        </span>
      </div>
      <div className="mt-3 rounded-xl border bg-card p-4" aria-live="polite">
        <p className="text-sm font-semibold">
          {step + 1}. {current.title}
        </p>
        <p className="mt-1 text-sm text-muted-foreground">{current.body}</p>
        <p className="mt-3 flex flex-wrap items-center gap-2 text-sm">
          {current.code.map((p) => (
            <Source key={p} path={p} />
          ))}
        </p>
      </div>
      <ol className="mt-3 grid gap-1 text-sm sm:grid-cols-2">
        {lifecycle.map((s, i) => (
          <li key={s.title}>
            <button
              type="button"
              onClick={() => go(i)}
              aria-current={i === step ? "step" : undefined}
              className={cn("w-full rounded-md px-2 py-1 text-left transition-colors", i === step ? "bg-primary/10 text-foreground" : "text-muted-foreground hover:bg-secondary hover:text-foreground")}
            >
              <span className="mr-2 font-mono text-xs">{String(i + 1).padStart(2, "0")}</span>
              {s.title}
            </button>
          </li>
        ))}
      </ol>
      <figcaption>Use the arrows or pick a step; every step names the code that performs it.</figcaption>
    </figure>
  );
}
