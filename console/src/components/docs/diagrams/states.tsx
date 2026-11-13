"use client";

import type { Node, NodeTypes } from "@xyflow/react";
import { useMemo, useState } from "react";

import { Chips, edge, Flow } from "@/components/docs/diagrams/flow";
import { StateNode } from "@/components/docs/diagrams/nodes";
import { states, transitions } from "@/components/docs/diagrams/states-data";

const nodeTypes: NodeTypes = { state: StateNode };

// DeliveryStates is the delivery state machine: select a state to read what it means and what moves it on.
export function DeliveryStates() {
  const [selected, setSelected] = useState<string | null>("in_flight");
  const nodes: Node[] = useMemo(
    () =>
      states.map((s) => ({
        id: s.id,
        type: "state",
        position: { x: s.x, y: s.y },
        data: { label: s.label, tone: s.tone, terminal: s.terminal, start: s.id === "start", emphasis: selected === s.id ? "selected" : "normal" },
        ariaLabel: `${s.label}${s.terminal ? ", terminal" : ""}`,
      })),
    [selected],
  );
  const edges = useMemo(() => transitions.map((t) => edge(t, selected && (t.source === selected || t.target === selected) ? "active" : "normal")), [selected]);
  const state = states.find((s) => s.id === selected);
  const select = (id: string) => setSelected(id);
  return (
    <figure className="docs-figure">
      <Flow nodes={nodes} edges={edges} nodeTypes={nodeTypes} label="The delivery state machine" onNodeClick={select} padding={0.12} />
      <Chips label="States" items={states.map((s) => ({ id: s.id, label: s.label }))} selected={selected} onSelect={select} />
      {state && (
        <div className="mt-3 rounded-xl border bg-card p-4" aria-live="polite">
          <p className="font-mono text-sm font-semibold">
            {state.label}
            {state.terminal && <span className="ml-2 font-sans text-xs font-normal text-muted-foreground">terminal</span>}
          </p>
          <p className="mt-1 text-sm text-muted-foreground">{state.meaning}</p>
          <p className="mt-2 text-sm">
            <span className="text-muted-foreground">Leaves by: </span>
            {state.leaves}
          </p>
        </div>
      )}
      <figcaption>
        Selecting a state lights the transitions into and out of it; double borders mark terminal states. Not drawn: a worker that fails before sending anything releases the delivery back to where it came from, without counting an attempt.
      </figcaption>
    </figure>
  );
}
