"use client";

import type { Edge, Node, NodeTypes } from "@xyflow/react";
import { MarkerType } from "@xyflow/react";
import { useMemo, useState } from "react";

import { Chips, Flow } from "@/components/docs/diagrams/flow";
import { RefNode, TableNode } from "@/components/docs/diagrams/nodes";
import { controlTables, deliveryTables, referencedTables, type Plane, type Table } from "@/components/docs/diagrams/schema-data";
import { cn } from "@/lib/utils";

const nodeTypes: NodeTypes = { table: TableNode, ref: RefNode };

function sides(from: { x: number }, to: { x: number }, self: boolean): ["left" | "right", "left" | "right"] {
  if (self) return ["right", "right"];
  if (to.x > from.x) return ["right", "left"];
  if (to.x < from.x) return ["left", "right"];
  return ["left", "left"];
}

function relation(id: string, source: string, sourceCol: string, target: string, targetCol: string, from: { x: number }, to: { x: number }, dashed: boolean, active: boolean): Edge {
  const [out, inn] = sides(from, to, source === target);
  const color = active ? "var(--primary)" : "var(--docs-edge)";
  return {
    id,
    source,
    target,
    sourceHandle: `${sourceCol}-${out}-out`,
    targetHandle: `${targetCol}-${inn}-in`,
    type: "smoothstep",
    className: cn(dashed && "docs-edge-link", active && "docs-edge-active"),
    markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color },
    focusable: false,
  };
}

function graph(plane: Plane, selected: string | null): { nodes: Node[]; edges: Edge[] } {
  const tables = plane === "delivery" ? deliveryTables : controlTables;
  const byName = new Map(tables.map((t) => [t.name, t]));
  const nodes: Node[] = tables.map((t) => ({
    id: `t-${t.name}`,
    type: "table",
    position: { x: t.x, y: t.y },
    data: { name: t.name, columns: t.columns, emphasis: selected === t.name ? "selected" : "normal" },
    ariaLabel: `table ${t.name}`,
  }));
  const edges: Edge[] = [];
  for (const t of tables) {
    for (const c of t.columns) {
      if (c.fk) {
        const [table, column] = c.fk.split(".");
        const target = byName.get(table);
        if (!target) continue;
        edges.push(relation(`${t.name}.${c.name}`, `t-${t.name}`, c.name, `t-${table}`, column, t, target, false, selected === t.name || selected === table));
      }
    }
  }
  if (plane === "control") {
    for (const r of referencedTables) {
      nodes.push({
        id: `ref-${r.owner}`,
        type: "ref",
        position: { x: r.x, y: r.y },
        data: {
          title: r.owner === "goauth" ? "goauth's tables" : "data plane tables",
          note: r.owner === "goauth" ? "same database, owned by goauth" : "another database, reached over gRPC",
          tables: r.tables,
        },
        selectable: false,
        focusable: false,
      });
    }
    for (const t of tables) {
      for (const c of t.columns) {
        if (!c.ref) continue;
        const [owner, table] = c.ref.split(".");
        const ref = referencedTables.find((r) => r.owner === owner);
        if (!ref) continue;
        edges.push(relation(`${t.name}.${c.name}`, `t-${t.name}`, c.name, `ref-${owner}`, table, t, ref, true, selected === t.name));
      }
    }
  }
  return { nodes, edges };
}

const planes: { id: Plane; label: string; database: string; first: string }[] = [
  { id: "delivery", label: "Data plane", database: "hookbeam_delivery", first: "deliveries" },
  { id: "control", label: "Control plane", database: "hookbeam", first: "api_keys" },
];

// DataModel draws both databases table by table: select a table for its purpose and the indexes that matter.
export function DataModel() {
  const [plane, setPlane] = useState<Plane>("delivery");
  const [selected, setSelected] = useState<string | null>("deliveries");
  const tables: Table[] = plane === "delivery" ? deliveryTables : controlTables;
  const { nodes, edges } = useMemo(() => graph(plane, selected), [plane, selected]);
  const table = tables.find((t) => t.name === selected);
  const select = (id: string) => {
    const name = id.replace(/^t-/, "");
    if (tables.some((t) => t.name === name)) setSelected(name);
  };
  return (
    <figure className="docs-figure">
      <div role="group" aria-label="Database" className="mb-3 inline-flex rounded-lg border p-0.5 text-sm">
        {planes.map((p) => (
          <button
            key={p.id}
            type="button"
            aria-pressed={plane === p.id}
            onClick={() => {
              setPlane(p.id);
              setSelected(p.first);
            }}
            className={cn("rounded-md px-3 py-1.5 transition-colors", plane === p.id ? "bg-secondary text-foreground" : "text-muted-foreground hover:text-foreground")}
          >
            {p.label} <span className="font-mono text-xs text-muted-foreground">{p.database}</span>
          </button>
        ))}
      </div>
      <Flow key={plane} nodes={nodes} edges={edges} nodeTypes={nodeTypes} label={`${plane === "delivery" ? "Data plane" : "Control plane"} schema`} onNodeClick={select} className="md:h-[640px]" padding={0.08} />
      <Chips label="Tables" items={tables.map((t) => ({ id: t.name, label: t.name }))} selected={selected} onSelect={select} />
      {table && (
        <div className="mt-3 rounded-xl border bg-card p-4" aria-live="polite">
          <p className="font-mono text-sm font-semibold">{table.name}</p>
          <p className="mt-1 text-sm text-muted-foreground">{table.purpose}</p>
          {table.indexes && (
            <ul className="mt-3 space-y-1.5 text-sm">
              {table.indexes.map((i) => (
                <li key={i.name}>
                  <code className="rounded bg-muted px-1.5 py-0.5 text-[12px]">{i.name}</code> <span className="text-muted-foreground">{i.why}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
      <figcaption>
        A key marks the primary key, a link a reference; a question mark after a type means the column is nullable. Solid arrows are foreign keys; dashed arrows are ids kept by value across an ownership boundary.
      </figcaption>
    </figure>
  );
}
