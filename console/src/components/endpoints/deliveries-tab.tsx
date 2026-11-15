"use client";

import { ChevronRight } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import type { components } from "@/api/schema";
import { type DeliveryState, useEndpointDeliveries } from "@/api/queries";
import { stateTone } from "@/components/explorer/state";
import { Badge } from "@/components/ui/badge";
import { Pager } from "@/components/ui/pager";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { ago, until, when } from "@/lib/format";
import { cn } from "@/lib/utils";

type Row = components["schemas"]["EndpointDelivery"];

const pageSize = 20;
const filters: { value: DeliveryState | ""; label: string }[] = [
  { value: "", label: "All" },
  { value: "succeeded", label: "Succeeded" },
  { value: "failed", label: "Failed" },
  { value: "retry_scheduled", label: "Retrying" },
  { value: "queued", label: "Queued" },
  { value: "in_flight", label: "In flight" },
  { value: "cancelled", label: "Cancelled" },
];
const terminal = ["succeeded", "failed", "cancelled"];

// DeliveriesTab is everything sent to this endpoint, newest first: the event, where its delivery stands and what
// the receiver last answered. Each row opens the delivery with its attempts.
export function DeliveriesTab({ endpointId }: { endpointId: number }) {
  const [state, setState] = useState<DeliveryState | "">("");
  const [offset, setOffset] = useState(0);
  const list = useEndpointDeliveries(endpointId, { state: state || undefined, limit: pageSize, offset });
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">Events sent to this endpoint, newest first. Refreshes every few seconds.</p>
        <div role="group" aria-label="Filter by state" className="flex flex-wrap gap-1">
          {filters.map((f) => (
            <button
              key={f.label}
              type="button"
              aria-pressed={state === f.value}
              onClick={() => {
                setState(f.value);
                setOffset(0);
              }}
              data-testid={`filter-${f.value || "all"}`}
              className={cn(
                "rounded-full border px-3 py-1 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                state === f.value ? "border-beam-from/60 bg-beam-from/10 font-medium text-foreground" : "border-border text-muted-foreground hover:text-foreground",
              )}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>
      {list.isPending && <LoadingState label="Loading deliveries…" />}
      {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
      {list.data && list.data.items.length === 0 && (
        <EmptyState title={state ? "No delivery in this state" : "Nothing sent here yet"} hint={state ? "Choose another state, or All." : "Send a test delivery from the button above, or submit an event this endpoint subscribes to."} />
      )}
      {list.data && list.data.items.length > 0 && (
        <>
          <div className="overflow-hidden rounded-lg border bg-card">
            <div className="overflow-x-auto">
              <table data-testid="endpoint-deliveries">
                <thead>
                  <tr className="border-b">
                    <th className="px-4 py-2.5">State</th>
                    <th className="px-4 py-2.5">Event</th>
                    <th className="px-4 py-2.5">Last response</th>
                    <th className="px-4 py-2.5">Attempts</th>
                    <th className="px-4 py-2.5">When</th>
                    <th className="px-4 py-2.5">
                      <span className="sr-only">Open</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.items.map((row) => (
                    <DeliveryRow key={row.delivery.id} row={row} />
                  ))}
                </tbody>
              </table>
            </div>
          </div>
          <Pager total={list.data.total} limit={pageSize} offset={offset} onChange={setOffset} noun="deliveries" />
        </>
      )}
    </>
  );
}

function DeliveryRow({ row }: { row: Row }) {
  const { delivery: d, message: m, lastAttempt: a } = row;
  const settled = terminal.includes(d.state);
  const ok = !!a?.statusCode && a.statusCode < 300;
  const href = `/events/${m.id}/deliveries/${d.id}`;
  return (
    <tr className="group border-b last:border-b-0" data-testid="endpoint-delivery-row">
      <td className="px-4 py-3">
        <Badge tone={stateTone(d.state)} dot pulse={!settled}>
          {d.state}
        </Badge>
        {d.failureReason && d.failureReason !== d.state && <span className="mt-1 block text-xs text-muted-foreground">{d.failureReason}</span>}
      </td>
      <td className="px-4 py-3">
        <Link href={href} className="font-medium hover:underline">
          {m.type}
        </Link>
        <span className="block font-mono text-xs text-muted-foreground">{m.externalId ?? m.webhookId}</span>
      </td>
      <td className="px-4 py-3">
        {a ? (
          <span className="flex items-center gap-2">
            <Badge tone={ok ? "good" : "bad"}>{a.statusCode ? `HTTP ${a.statusCode}` : (a.errorClass ?? "unknown")}</Badge>
            <span className="text-xs tabular-nums text-muted-foreground">{a.latencyMs} ms</span>
          </span>
        ) : (
          <span className="text-muted-foreground">not attempted</span>
        )}
      </td>
      <td className="px-4 py-3 tabular-nums">
        {d.attemptCount}
        {d.replayOfDeliveryId && <span className="ml-2 text-xs text-muted-foreground">replay</span>}
      </td>
      <td className="px-4 py-3 text-muted-foreground" title={when(d.completedAt ?? d.dueAt ?? d.createdAt)}>
        {d.completedAt ? ago(d.completedAt) : d.dueAt ? `next ${until(d.dueAt)}` : ago(d.createdAt)}
      </td>
      <td className="px-4 py-3 text-right">
        <Link href={href} aria-label={`Open delivery ${d.id}`} className="inline-flex rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground">
          <ChevronRight className="h-4 w-4 transition-transform group-hover:translate-x-0.5" aria-hidden />
        </Link>
      </td>
    </tr>
  );
}
