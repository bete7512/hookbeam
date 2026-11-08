"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { asApiError, errorFromResponse } from "@/api/errors";
import { useMe, useMessage } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";
import { can, deny } from "@/lib/roles";

import { stateTone } from "./state";

// EventDetail shows one accepted message: its metadata and retention, one row per recipient's delivery cycle,
// and the payload for those allowed to read it.
export function EventDetail({ id }: { id: number }) {
  const me = useMe();
  const msg = useMessage(id, true);
  const [tab, setTab] = useState<"deliveries" | "payload">("deliveries");
  if (msg.isPending) return <LoadingState />;
  if (msg.isError) return <QueryState error={msg.error} onRetry={() => msg.refetch()} />;
  const m = msg.data.message;
  const role = me.data?.role ?? null;
  const mayReadPayload = can(role, "readPayloads");
  const expired = m.payloadExpiresAt ? new Date(m.payloadExpiresAt).getTime() < Date.now() : false;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <p className="text-sm text-muted-foreground">
          <Link href="/events" className="underline">
            Events
          </Link>{" "}
          / {m.webhookId}
        </p>
        <h1 className="text-2xl font-semibold" data-testid="event-type">
          {m.type}
        </h1>
        <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted-foreground">webhook id</dt>
          <dd className="font-mono text-xs">{m.webhookId}</dd>
          <dt className="text-muted-foreground">external id</dt>
          <dd>{m.externalId ?? "—"}</dd>
          <dt className="text-muted-foreground">accepted</dt>
          <dd>
            {when(m.acceptedAt)} ({ago(m.acceptedAt)})
          </dd>
          <dt className="text-muted-foreground">payload kept until</dt>
          <dd data-testid="retention">{m.payloadExpiresAt ? `${when(m.payloadExpiresAt)}${expired ? " · expired" : ""}` : "unknown"}</dd>
          <dt className="text-muted-foreground">application</dt>
          <dd>
            <Link href={`/applications/${m.applicationId}`} className="underline">
              application {m.applicationId}
            </Link>{" "}
            · {m.environmentKind}
          </dd>
        </dl>
      </header>
      <div role="tablist" aria-label="Event sections" className="flex gap-2 border-b text-sm">
        <button role="tab" aria-selected={tab === "deliveries"} className={`px-3 py-2 ${tab === "deliveries" ? "border-b-2 border-foreground font-medium" : "text-muted-foreground"}`} onClick={() => setTab("deliveries")}>
          Deliveries ({msg.data.deliveries.length})
        </button>
        {mayReadPayload && (
          <button role="tab" aria-selected={tab === "payload"} className={`px-3 py-2 ${tab === "payload" ? "border-b-2 border-foreground font-medium" : "text-muted-foreground"}`} onClick={() => setTab("payload")} data-testid="payload-tab">
            Payload
          </button>
        )}
        {!mayReadPayload && (
          <span className="px-3 py-2 text-muted-foreground" data-testid="payload-denied">
            Payload hidden: {deny(role, "readPayloads")}
          </span>
        )}
      </div>
      {tab === "deliveries" && (
        <Card>
          <CardHeader>
            <CardTitle>Recipients</CardTitle>
            <CardDescription>One cycle per endpoint; a replay adds a cycle.</CardDescription>
          </CardHeader>
          <CardContent>
            <table className="w-full text-sm">
              <thead className="text-left text-muted-foreground">
                <tr>
                  <th className="py-2 pr-4 font-medium">Delivery</th>
                  <th className="py-2 pr-4 font-medium">Endpoint</th>
                  <th className="py-2 pr-4 font-medium">State</th>
                  <th className="py-2 pr-4 font-medium">Attempts</th>
                  <th className="py-2 pr-4 font-medium">Next / finished</th>
                </tr>
              </thead>
              <tbody>
                {msg.data.deliveries.map((d) => (
                  <tr key={d.id} className="border-t" data-testid="delivery-row">
                    <td className="py-2 pr-4">
                      <Link href={`/events/${m.id}/deliveries/${d.id}`} className="underline">
                        {d.id}
                      </Link>
                      {d.replayOfDeliveryId && <span className="block text-xs text-muted-foreground">replay of {d.replayOfDeliveryId}</span>}
                    </td>
                    <td className="py-2 pr-4">
                      <Link href={`/endpoints/${d.endpointId}`} className="underline">
                        endpoint {d.endpointId}
                      </Link>
                      <span className="block text-xs text-muted-foreground">revision {d.endpointRevision}</span>
                    </td>
                    <td className="py-2 pr-4">
                      <Badge tone={stateTone(d.state)} dot pulse={!["succeeded", "failed", "cancelled"].includes(d.state)}>
                        {d.state}
                      </Badge>
                      {d.failureReason && <span className="block text-xs text-muted-foreground">{d.failureReason}</span>}
                    </td>
                    <td className="py-2 pr-4">{d.attemptCount}</td>
                    <td className="py-2 pr-4">{d.completedAt ? when(d.completedAt) : d.dueAt ? `due ${when(d.dueAt)}` : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      )}
      {tab === "payload" && mayReadPayload && <Payload id={m.id} />}
    </div>
  );
}

function Payload({ id }: { id: number }) {
  const payload = useQuery({
    queryKey: ["payload", id],
    queryFn: async () => {
      const res = await fetch(`/v1/messages/${id}/payload`, { credentials: "include" });
      if (!res.ok) throw await errorFromResponse(res);
      return res.text();
    },
    retry: false,
  });
  if (payload.isPending) return <LoadingState label="Loading the payload…" />;
  if (payload.isError) {
    const err = asApiError(payload.error);
    if (err.status === 410) {
      return (
        <p role="status" className="text-sm text-muted-foreground" data-testid="payload-expired">
          The payload has expired and was dropped; the metadata above stays.
        </p>
      );
    }
    return <QueryState error={payload.error} onRetry={() => payload.refetch()} />;
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>Payload</CardTitle>
        <CardDescription>The exact bytes the sender submitted; reading them is audited.</CardDescription>
      </CardHeader>
      <CardContent>
        <pre className="overflow-x-auto rounded-md bg-secondary p-3 text-xs" data-testid="payload-text">
          {pretty(payload.data)}
        </pre>
        <Button variant="outline" size="sm" className="mt-3" onClick={() => navigator.clipboard.writeText(payload.data).catch(() => undefined)}>
          Copy
        </Button>
      </CardContent>
    </Card>
  );
}

function pretty(text: string) {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}
