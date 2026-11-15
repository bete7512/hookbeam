"use client";

import Link from "next/link";
import { useState } from "react";

import { useApplicationMessages } from "@/api/queries";
import { DeliveryCountBadges } from "@/components/explorer/delivery-counts";
import { Pager } from "@/components/ui/pager";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";

export const eventsPageSize = 15;

// EventsTab lists what this application accepted, newest first, with where each event's deliveries stand.
export function EventsTab({ applicationId }: { applicationId: number }) {
  const [offset, setOffset] = useState(0);
  const list = useApplicationMessages(applicationId, { limit: eventsPageSize, offset });
  return (
    <>
      <p className="text-sm text-muted-foreground">
        Events accepted for this application, newest first. Filters across the whole environment are in the{" "}
        <Link href="/events" className="underline underline-offset-2 hover:text-foreground">
          explorer
        </Link>
        .
      </p>
      {list.isPending && <LoadingState label="Loading events…" />}
      {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
      {list.data && list.data.total === 0 && <EmptyState title="No events yet" hint="Submit one with a bound key, or send a test delivery from an endpoint." />}
      {list.data && list.data.items.length > 0 && (
        <>
          <div className="overflow-hidden rounded-lg border bg-card">
            <div className="overflow-x-auto">
              <table data-testid="application-events">
                <thead>
                  <tr className="border-b">
                    <th className="px-4 py-2.5">Type</th>
                    <th className="px-4 py-2.5">Webhook id</th>
                    <th className="px-4 py-2.5">Accepted</th>
                    <th className="px-4 py-2.5">Deliveries</th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.items.map((m) => (
                    <tr key={m.message.id} className="border-b last:border-b-0">
                      <td className="px-4 py-3">
                        <Link href={`/events/${m.message.id}`} className="font-medium hover:underline">
                          {m.message.type}
                        </Link>
                        {m.message.externalId && <span className="block text-xs text-muted-foreground">{m.message.externalId}</span>}
                      </td>
                      <td className="px-4 py-3 font-mono text-xs text-muted-foreground">{m.message.webhookId}</td>
                      <td className="px-4 py-3 text-muted-foreground" title={when(m.message.acceptedAt)}>
                        {ago(m.message.acceptedAt)}
                      </td>
                      <td className="px-4 py-3">
                        <DeliveryCountBadges counts={m.deliveryCounts} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
          <Pager total={list.data.total} limit={eventsPageSize} offset={offset} onChange={setOffset} noun="events" />
        </>
      )}
    </>
  );
}
