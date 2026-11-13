"use client";

import type { NodeTypes } from "@xyflow/react";
import { useMemo } from "react";

import { edge, Flow } from "@/components/docs/diagrams/flow";
import { CardNode } from "@/components/docs/diagrams/nodes";
import { concepts, outboxPath, traceLinks, type Diagram } from "@/components/docs/diagrams/small-data";

const nodeTypes: NodeTypes = { card: CardNode };

function StaticDiagram({ diagram, label, caption, className }: { diagram: Diagram; label: string; caption: string; className?: string }) {
  const nodes = useMemo(
    () =>
      diagram.cards.map((c) => ({
        id: c.id,
        type: "card",
        position: { x: c.x, y: c.y },
        data: { title: c.title, tag: c.tag, body: c.body, mono: c.mono, tone: c.tone },
        ariaLabel: c.title,
      })),
    [diagram],
  );
  const edges = useMemo(() => diagram.links.map((l) => edge(l, l.dashed ? "link" : "normal")), [diagram]);
  return (
    <figure className="docs-figure">
      <Flow nodes={nodes} edges={edges} nodeTypes={nodeTypes} label={label} className={className} padding={0.1} />
      <figcaption>{caption}</figcaption>
    </figure>
  );
}

export function ConceptMap() {
  return (
    <StaticDiagram
      diagram={concepts}
      label="Hookbeam's concepts and how they relate"
      caption="Configuration runs along the top: a workspace holds environments, applications and their endpoints. Traffic runs along the bottom: a message fans out into one delivery per endpoint, each made of attempts."
      className="md:h-[480px]"
    />
  );
}

export function TraceLinks() {
  return (
    <StaticDiagram
      diagram={traceLinks}
      label="One submission's spans, joined by parents and links"
      caption="Solid arrows are parent and child inside one request. Dashed arrows are span links: the queue and the tables break the parent chain, so each consumer links the context stored with the row it read."
    />
  );
}

export function OutboxPath() {
  return (
    <StaticDiagram
      diagram={outboxPath}
      label="The transactional outbox, from commit to handler"
      caption="The row commits with the change; the relay publishes it later; the consumer remembers which events it handled, so a second copy changes nothing."
      className="md:h-[400px]"
    />
  );
}
