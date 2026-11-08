"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useAttempts, useMe, useMessage } from "@/api/queries";
import { FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";
import { can, deny } from "@/lib/roles";

import { stateTone } from "./state";

type Delivery = components["schemas"]["Delivery"];
type Attempt = components["schemas"]["Attempt"];
type Message = components["schemas"]["Message"];

const terminal = ["succeeded", "failed", "cancelled"];

// DeliveryDetail follows one cycle: its attempts in order, the classified failure, the next retry, and the
// replay and cancel actions with their confirmations.
export function DeliveryDetail({ messageId, deliveryId }: { messageId: number; deliveryId: number }) {
  const me = useMe();
  const msg = useMessage(messageId, true);
  const delivery = msg.data?.deliveries.find((d) => d.id === deliveryId) ?? null;
  const settled = delivery ? terminal.includes(delivery.state) : true;
  const attempts = useAttempts(deliveryId, !settled);
  if (msg.isPending || me.isPending) return <LoadingState />;
  if (msg.isError) return <QueryState error={msg.error} onRetry={() => msg.refetch()} />;
  if (!delivery) return <QueryState error={{ status: 404 }} />;
  const latest = attempts.data?.at(-1) ?? null;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <p className="text-sm text-muted-foreground">
          <Link href="/events" className="underline">
            Events
          </Link>{" "}
          /{" "}
          <Link href={`/events/${messageId}`} className="underline">
            {msg.data.message.webhookId}
          </Link>{" "}
          / delivery {delivery.id}
        </p>
        <h1 className="flex flex-wrap items-center gap-3 text-2xl font-semibold">
          Delivery {delivery.id}
          <Badge tone={stateTone(delivery.state)} data-testid="delivery-state">
            {delivery.state}
          </Badge>
        </h1>
        <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted-foreground">endpoint</dt>
          <dd>
            <Link href={`/endpoints/${delivery.endpointId}`} className="underline">
              endpoint {delivery.endpointId}
            </Link>{" "}
            at revision {delivery.endpointRevision}
          </dd>
          <dt className="text-muted-foreground">attempts</dt>
          <dd>{delivery.attemptCount}</dd>
          <dt className="text-muted-foreground">failure</dt>
          <dd data-testid="failure">{delivery.failureReason ?? "none"}</dd>
          <dt className="text-muted-foreground">latest latency</dt>
          <dd>{latest ? `${latest.latencyMs} ms` : "—"}</dd>
          <dt className="text-muted-foreground">next retry</dt>
          <dd data-testid="next-retry">{!settled && delivery.dueAt ? `${when(delivery.dueAt)} (${ago(delivery.dueAt)})` : "none"}</dd>
          <dt className="text-muted-foreground">deadline</dt>
          <dd>{when(delivery.deadlineAt)}</dd>
          <dt className="text-muted-foreground">finished</dt>
          <dd>{delivery.completedAt ? when(delivery.completedAt) : "not yet"}</dd>
        </dl>
      </header>
      <Actions message={msg.data.message} delivery={delivery} role={me.data?.role ?? null} />
      <Card>
        <CardHeader>
          <CardTitle>Attempts</CardTitle>
          <CardDescription>Oldest first. Each row records the endpoint revision and secret version it used.</CardDescription>
        </CardHeader>
        <CardContent>
          {attempts.isPending && <LoadingState />}
          {attempts.isError && <QueryState error={attempts.error} onRetry={() => attempts.refetch()} />}
          {attempts.data && attempts.data.length === 0 && <p className="text-sm text-muted-foreground">No attempt yet; the worker picks it up next.</p>}
          {attempts.data && attempts.data.length > 0 && (
            <ol className="flex flex-col gap-2" data-testid="attempts">
              {attempts.data.map((a) => (
                <AttemptRow key={a.id} attempt={a} />
              ))}
            </ol>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export function AttemptRow({ attempt }: { attempt: Attempt }) {
  const [open, setOpen] = useState(false);
  const outcome = attempt.statusCode ? `HTTP ${attempt.statusCode}` : (attempt.errorClass ?? "pending");
  return (
    <li className="rounded-md border p-3 text-sm" data-testid="attempt">
      <button className="flex w-full flex-wrap items-center gap-3 text-left" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <span className="font-medium">#{attempt.number}</span>
        <Badge tone={attempt.statusCode && attempt.statusCode < 300 ? "good" : "bad"}>{outcome}</Badge>
        <span className="text-muted-foreground">{attempt.latencyMs} ms</span>
        <span className="text-muted-foreground">{when(attempt.startedAt)}</span>
        <span className="ml-auto text-xs text-muted-foreground">{open ? "hide" : "details"}</span>
      </button>
      {open && (
        <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
          <dt className="text-muted-foreground">request</dt>
          <dd className="break-all">{attempt.requestUrl}</dd>
          <dt className="text-muted-foreground">endpoint revision</dt>
          <dd>{attempt.endpointRevision}</dd>
          <dt className="text-muted-foreground">secret version</dt>
          <dd>{attempt.secretVersion}</dd>
          <dt className="text-muted-foreground">finished</dt>
          <dd>{attempt.completedAt ? when(attempt.completedAt) : "—"}</dd>
          <dt className="text-muted-foreground">response headers</dt>
          <dd>
            {attempt.responseHeaders && Object.keys(attempt.responseHeaders).length > 0 ? (
              <ul className="font-mono">
                {Object.entries(attempt.responseHeaders).map(([k, v]) => (
                  <li key={k}>
                    {k}: {v}
                  </li>
                ))}
              </ul>
            ) : (
              <span className="text-muted-foreground">not shown</span>
            )}
          </dd>
          <dt className="text-muted-foreground">response preview</dt>
          <dd>
            {attempt.responsePreview ? (
              // Rendered as text on purpose: whatever the receiver sent back is data, never markup here.
              <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded bg-secondary p-2 font-mono" data-testid="preview">
                {attempt.responsePreview}
              </pre>
            ) : (
              <span className="text-muted-foreground">not shown</span>
            )}
          </dd>
        </dl>
      )}
    </li>
  );
}

function Actions({ message, delivery, role }: { message: Message; delivery: Delivery; role: string | null }) {
  const queryClient = useQueryClient();
  const [dialog, setDialog] = useState<"replay" | "cancel" | null>(null);
  const [idempotencyKey] = useState(() => (typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : String(Date.now())));
  const expired = message.payloadExpiresAt ? new Date(message.payloadExpiresAt).getTime() < Date.now() : false;
  const settled = terminal.includes(delivery.state);
  const replayable = (delivery.state === "failed" || delivery.state === "cancelled") && !expired;
  const mayReplay = can(role, "replay");
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: keys.message(message.id) });
    queryClient.invalidateQueries({ queryKey: keys.attempts(delivery.id) });
  };
  const replay = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/deliveries/{deliveryId}/replay", { params: { path: { deliveryId: delivery.id }, header: { "Idempotency-Key": idempotencyKey } } })),
    onSuccess: () => {
      setDialog(null);
      refresh();
    },
  });
  const cancel = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/deliveries/{deliveryId}/cancel", { params: { path: { deliveryId: delivery.id } } })),
    onSuccess: () => {
      setDialog(null);
      refresh();
    },
  });
  const replayReason = !mayReplay ? deny(role, "replay") : expired ? "the payload has expired" : !replayable ? `a ${delivery.state} delivery cannot be replayed` : null;
  const cancelReason = !mayReplay ? deny(role, "replay") : settled ? `a ${delivery.state} delivery is already finished` : null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Actions</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" disabled={!!replayReason} onClick={() => setDialog("replay")} data-testid="replay">
            Replay
          </Button>
          {replayReason && (
            <span className="text-muted-foreground" data-testid="replay-reason">
              {replayReason}
            </span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" disabled={!!cancelReason} onClick={() => setDialog("cancel")} data-testid="cancel">
            Cancel delivery
          </Button>
          {cancelReason && <span className="text-muted-foreground">{cancelReason}</span>}
        </div>
        {dialog && (
          <div role="dialog" aria-modal="true" aria-labelledby="confirm-title" className="rounded-md border bg-card p-4" data-testid="confirm-dialog">
            <h2 id="confirm-title" className="font-medium">
              {dialog === "replay" ? "Replay this delivery?" : "Cancel this delivery?"}
            </h2>
            <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
              <dt className="text-muted-foreground">destination</dt>
              <dd>
                endpoint {delivery.endpointId}, planned at revision {delivery.endpointRevision}
                {dialog === "replay" ? "; the replay uses the endpoint's current revision" : ""}
              </dd>
              <dt className="text-muted-foreground">original outcome</dt>
              <dd>
                {delivery.state}
                {delivery.failureReason ? ` (${delivery.failureReason})` : ""}
              </dd>
              <dt className="text-muted-foreground">payload kept until</dt>
              <dd>{when(message.payloadExpiresAt)}</dd>
            </dl>
            {dialog === "replay" && (
              <p role="note" className="mt-2 text-xs">
                The receiver gets the same <code>webhook-id</code> again, so a receiver that does not deduplicate processes the event twice.
              </p>
            )}
            {dialog === "cancel" && <p className="mt-2 text-xs">An attempt already claimed may still finish and record its outcome.</p>}
            <FormError message={replay.isError ? asApiError(replay.error).message : cancel.isError ? asApiError(cancel.error).message : null} />
            <div className="mt-3 flex gap-2">
              <Button onClick={() => (dialog === "replay" ? replay.mutate() : cancel.mutate())} disabled={replay.isPending || cancel.isPending} data-testid="confirm">
                {dialog === "replay" ? "Replay" : "Cancel delivery"}
              </Button>
              <Button variant="ghost" onClick={() => setDialog(null)}>
                Keep
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
