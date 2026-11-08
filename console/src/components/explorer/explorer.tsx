"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMemo } from "react";

import { api } from "@/api/client";
import { unwrap } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { useEnvironment } from "@/lib/environment";
import { ago, when } from "@/lib/format";

import { type Filters, parseFilters, serializeFilters, toQuery } from "./filters";

// Explorer lists the environment's events; every filter, sort and page lives in the URL.
export function Explorer() {
  const { current } = useEnvironment();
  const envId = current?.id ?? null;
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const filters = useMemo(() => parseFilters(new URLSearchParams(params.toString())), [params]);
  const set = (next: Partial<Filters>, resetOffset = true) => {
    const merged = { ...filters, ...next, ...(resetOffset ? { offset: 0 } : {}) };
    router.replace(`${pathname}${serializeFilters(merged)}`);
  };
  const list = useQuery({
    queryKey: ["messages", envId, filters],
    queryFn: () => unwrap(api.GET("/v1/environments/{environmentId}/messages", { params: { path: { environmentId: envId ?? 0 }, query: toQuery(filters) } })),
    enabled: envId !== null,
    refetchInterval: 10_000,
    placeholderData: (prev) => prev,
  });
  const total = list.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / filters.limit));
  const page = Math.floor(filters.offset / filters.limit) + 1;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Events</h1>
        <p className="text-muted-foreground">Everything accepted into {current?.name ?? "the environment"}, newest first by default.</p>
      </header>
      <form
        className="grid gap-3 rounded-md border p-4 sm:grid-cols-2 lg:grid-cols-4"
        onSubmit={(e) => {
          e.preventDefault();
          const data = new FormData(e.currentTarget);
          set({ type: String(data.get("type") ?? ""), externalId: String(data.get("external_id") ?? ""), acceptedAfter: String(data.get("accepted_after") ?? ""), acceptedBefore: String(data.get("accepted_before") ?? "") });
        }}
        data-testid="filters"
      >
        <div className="flex flex-col gap-1">
          <Label htmlFor="f-type">Event type</Label>
          <Input id="f-type" name="type" defaultValue={filters.type} key={`type-${filters.type}`} placeholder="order.shipped" />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="f-external">External id</Label>
          <Input id="f-external" name="external_id" defaultValue={filters.externalId} key={`ext-${filters.externalId}`} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="f-after">Accepted after</Label>
          <Input id="f-after" name="accepted_after" type="datetime-local" defaultValue={filters.acceptedAfter} key={`after-${filters.acceptedAfter}`} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="f-before">Accepted before</Label>
          <Input id="f-before" name="accepted_before" type="datetime-local" defaultValue={filters.acceptedBefore} key={`before-${filters.acceptedBefore}`} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="f-outcome">Outcome</Label>
          <select id="f-outcome" className="h-9 rounded-md border border-input bg-background px-2 text-sm" value={filters.outcome} onChange={(e) => set({ outcome: e.target.value as Filters["outcome"] })} data-testid="outcome-filter">
            <option value="">any</option>
            {(["queued", "in_flight", "retry_scheduled", "succeeded", "failed", "cancelled"] as const).map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="f-sort">Sort</Label>
          <select
            id="f-sort"
            className="h-9 rounded-md border border-input bg-background px-2 text-sm"
            value={`${filters.sortBy}:${filters.sortDir}`}
            onChange={(e) => {
              const [sortBy, sortDir] = e.target.value.split(":") as [Filters["sortBy"], Filters["sortDir"]];
              set({ sortBy, sortDir });
            }}
          >
            <option value="acceptedAt:desc">newest first</option>
            <option value="acceptedAt:asc">oldest first</option>
            <option value="id:asc">id ascending</option>
            <option value="id:desc">id descending</option>
          </select>
        </div>
        <div className="flex items-end gap-2">
          <Button type="submit" variant="outline" data-testid="apply-filters">
            Apply
          </Button>
          <Button type="button" variant="ghost" onClick={() => router.replace(pathname)}>
            Clear
          </Button>
        </div>
      </form>
      {envId === null && <LoadingState label="Waiting for the environment…" />}
      {list.isPending && envId !== null && <LoadingState label="Loading events…" />}
      {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
      {list.data && list.data.items.length === 0 && <EmptyState title="No events match" hint={filters.type || filters.externalId || filters.outcome ? "Loosen a filter, or send a test delivery from an endpoint." : "Submit an event with a bound key, or send a test delivery from an endpoint."} />}
      {list.data && list.data.items.length > 0 && (
        <>
          <table className="w-full text-sm" data-testid="events-table">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-2 pr-4 font-medium">Type</th>
                <th className="py-2 pr-4 font-medium">Webhook id</th>
                <th className="py-2 pr-4 font-medium">Accepted</th>
                <th className="py-2 pr-4 font-medium">Deliveries</th>
                <th className="py-2 pr-4 font-medium">Payload kept until</th>
              </tr>
            </thead>
            <tbody>
              {list.data.items.map((m) => (
                <tr key={m.message.id} className="border-t align-top" data-testid="event-row">
                  <td className="py-2 pr-4">
                    <Link href={`/events/${m.message.id}`} className="underline">
                      {m.message.type}
                    </Link>
                    {m.message.externalId && <span className="block text-xs text-muted-foreground">{m.message.externalId}</span>}
                  </td>
                  <td className="py-2 pr-4 font-mono text-xs">{m.message.webhookId}</td>
                  <td className="py-2 pr-4" title={when(m.message.acceptedAt)}>
                    {ago(m.message.acceptedAt)}
                  </td>
                  <td className="py-2 pr-4">
                    <Counts counts={m.deliveryCounts} />
                  </td>
                  <td className="py-2 pr-4">{when(m.payloadExpiresAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">
              {total} events · page {page} of {pages}
            </span>
            <div className="flex gap-2">
              <Button variant="outline" size="sm" disabled={filters.offset === 0} onClick={() => set({ offset: Math.max(0, filters.offset - filters.limit) }, false)}>
                Previous
              </Button>
              <Button variant="outline" size="sm" disabled={filters.offset + filters.limit >= total} onClick={() => set({ offset: filters.offset + filters.limit }, false)} data-testid="next-page">
                Next
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function Counts({ counts }: { counts: { queued: number; inFlight: number; retryScheduled: number; succeeded: number; failed: number; cancelled: number } }) {
  const parts: { label: string; n: number; tone: "good" | "bad" | "warn" | "neutral" }[] = [
    { label: "succeeded", n: counts.succeeded, tone: "good" },
    { label: "failed", n: counts.failed, tone: "bad" },
    { label: "retrying", n: counts.retryScheduled + counts.queued + counts.inFlight, tone: "warn" },
    { label: "cancelled", n: counts.cancelled, tone: "neutral" },
  ];
  return (
    <span className="flex flex-wrap gap-1">
      {parts.filter((p) => p.n > 0).map((p) => (
        <Badge key={p.label} tone={p.tone} dot pulse={p.tone === "warn"}>
          {p.n} {p.label}
        </Badge>
      ))}
    </span>
  );
}
