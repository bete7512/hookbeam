"use client";

import { Handle, Position, type Node, type NodeProps } from "@xyflow/react";
import { KeyRound, Link2, type LucideIcon } from "lucide-react";
import { Fragment } from "react";

import { SideHandles } from "@/components/docs/diagrams/flow";
import { cn } from "@/lib/utils";

export type Emphasis = "normal" | "active" | "dim" | "selected";

const emphasis: Record<Emphasis, string> = {
  normal: "",
  active: "ring-2 ring-primary shadow-[0_0_28px_-6px_var(--beam-from)]",
  dim: "opacity-25",
  selected: "ring-2 ring-primary",
};

export type SystemKind = "external" | "edge" | "process" | "database" | "queue";

const kindStyle: Record<SystemKind, { border: string; icon: string }> = {
  process: { border: "border-primary/45", icon: "text-primary" },
  database: { border: "border-cyan-500/50", icon: "text-cyan-600 dark:text-cyan-300" },
  queue: { border: "border-amber-500/50", icon: "text-amber-600 dark:text-amber-300" },
  external: { border: "border-border", icon: "text-muted-foreground" },
  edge: { border: "border-dashed border-foreground/30", icon: "text-foreground" },
};

export type SystemNodeData = { label: string; detail: string; kind: SystemKind; icon: LucideIcon; emphasis: Emphasis };

export function SystemNode({ data }: NodeProps<Node<SystemNodeData>>) {
  const Icon = data.icon;
  const style = kindStyle[data.kind];
  return (
    <div className={cn("w-[180px] cursor-pointer rounded-xl border bg-card px-3 py-2.5 shadow-sm transition-all duration-300", style.border, emphasis[data.emphasis])}>
      <div className="flex items-center gap-2">
        <Icon aria-hidden className={cn("size-4 shrink-0", style.icon)} />
        <span className="text-[14px] font-semibold leading-tight">{data.label}</span>
      </div>
      <p className="mt-1 text-[12px] leading-snug text-muted-foreground">{data.detail}</p>
      <SideHandles />
    </div>
  );
}

export type GroupNodeData = { label: string };

// GroupNode is a plane: a dashed region its components sit in.
export function GroupNode({ data }: NodeProps<Node<GroupNodeData>>) {
  return (
    <div className="relative h-full w-full rounded-2xl border border-dashed border-primary/35 bg-primary/[0.035]">
      <span className="absolute left-4 top-2.5 font-mono text-[11px] font-medium uppercase tracking-wider text-primary/80">{data.label}</span>
    </div>
  );
}

const toneStyle = {
  neutral: "border-foreground/25 text-foreground",
  info: "border-sky-500/60 text-sky-700 dark:text-sky-300",
  warn: "border-amber-500/60 text-amber-700 dark:text-amber-300",
  good: "border-emerald-500/60 text-emerald-700 dark:text-emerald-300",
  bad: "border-red-500/60 text-red-700 dark:text-red-300",
};

export type StateNodeData = { label: string; tone: keyof typeof toneStyle; terminal: boolean; start: boolean; emphasis: Emphasis };

export function StateNode({ data }: NodeProps<Node<StateNodeData>>) {
  if (data.start) {
    return (
      <div className={cn("flex cursor-pointer items-center gap-2 rounded-full border border-dashed bg-card px-3 py-2 text-[12px] text-muted-foreground transition-all", emphasis[data.emphasis])}>
        <span aria-hidden className="size-2.5 rounded-full bg-foreground/70" />
        {data.label}
        <SideHandles />
      </div>
    );
  }
  return (
    <div
      className={cn(
        "min-w-[150px] cursor-pointer rounded-xl bg-card px-4 py-3 text-center font-mono text-[14px] font-semibold shadow-sm transition-all",
        data.terminal ? "border-[3px] border-double" : "border-2",
        toneStyle[data.tone],
        emphasis[data.emphasis],
      )}
    >
      {data.label}
      <SideHandles />
    </div>
  );
}

export type TableColumn = { name: string; type: string; nullable?: boolean; pk?: boolean; fk?: string; ref?: string };
export type TableNodeData = { name: string; columns: TableColumn[]; emphasis: Emphasis };

// TableNode draws one table; every column row carries handles, so a foreign key leaves from its own row.
export function TableNode({ data }: NodeProps<Node<TableNodeData>>) {
  return (
    <div className={cn("w-[270px] cursor-pointer overflow-hidden rounded-lg border bg-card text-[11px] leading-[1.35] shadow-sm transition-all", emphasis[data.emphasis])}>
      <div className="border-b bg-primary/10 px-3 py-1.5 font-mono text-[12px] font-semibold">{data.name}</div>
      <ul>
        {data.columns.map((c) => (
          <li key={c.name} className="relative flex items-center gap-2 border-b border-border/60 px-3 py-[2px] last:border-b-0">
            <span className={cn("w-3 shrink-0", c.pk ? "text-amber-500" : "text-sky-500")} aria-hidden>
              {c.pk ? <KeyRound className="size-3" /> : c.fk || c.ref ? <Link2 className="size-3" /> : null}
            </span>
            <span className={cn("font-mono", c.pk && "font-semibold")}>{c.name}</span>
            <span className="ml-auto font-mono text-muted-foreground">
              {c.type}
              {c.nullable ? "?" : ""}
            </span>
            <RowHandles id={c.name} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function RowHandles({ id }: { id: string }) {
  return (
    <>
      {(["left", "right"] as const).map((side) => (
        <Fragment key={side}>
          <Handle type="source" id={`${id}-${side}-out`} position={side === "left" ? Position.Left : Position.Right} className="docs-handle" isConnectable={false} />
          <Handle type="target" id={`${id}-${side}-in`} position={side === "left" ? Position.Left : Position.Right} className="docs-handle" isConnectable={false} />
        </Fragment>
      ))}
    </>
  );
}

export type RefNodeData = { title: string; note: string; tables: string[] };

// RefNode lists tables owned elsewhere that a schema refers to by id.
export function RefNode({ data }: NodeProps<Node<RefNodeData>>) {
  return (
    <div className="w-[230px] overflow-hidden rounded-lg border border-dashed bg-card/70 text-[11px]">
      <div className="border-b border-dashed px-3 py-1.5">
        <span className="font-mono text-[12px] font-semibold">{data.title}</span>
        <p className="text-muted-foreground">{data.note}</p>
      </div>
      <ul>
        {data.tables.map((t) => (
          <li key={t} className="relative px-3 py-[3px] font-mono text-muted-foreground">
            {t}
            <RowHandles id={t} />
          </li>
        ))}
      </ul>
    </div>
  );
}

export type CardNodeData = { title: string; body?: string; tag?: string; mono?: boolean; tone?: "plain" | "accent" | "store" | "queue" };

const cardTone = {
  plain: "border-border",
  accent: "border-primary/50",
  store: "border-cyan-500/50",
  queue: "border-amber-500/50",
};

// CardNode is the small diagrams' box: a title, an optional tag and a sentence.
export function CardNode({ data }: NodeProps<Node<CardNodeData>>) {
  return (
    <div className={cn("w-[220px] rounded-xl border bg-card px-3 py-2.5 shadow-sm", cardTone[data.tone ?? "plain"])}>
      {/* A code name (a span, a table) takes the whole width and its tag goes underneath, so the name never breaks
          mid-word for the tag's sake. */}
      <div className={cn("flex gap-2", data.mono ? "flex-col items-start gap-1" : "items-start justify-between")}>
        <span className={cn("min-w-0 text-[13px] font-semibold leading-tight [overflow-wrap:anywhere]", data.mono && "font-mono text-[12px]")}>{data.title}</span>
        {data.tag && <span className="shrink-0 rounded-full border px-1.5 py-px text-[10px] text-muted-foreground">{data.tag}</span>}
      </div>
      {data.body && <p className="mt-1 text-[11px] leading-snug text-muted-foreground">{data.body}</p>}
      <SideHandles />
    </div>
  );
}
