"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowUpRight, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import { keys, unwrap, useAttempts, useMessage } from "@/api/queries";
import { FormError } from "@/components/form";
import { stateTone } from "@/components/explorer/state";
import { Badge } from "@/components/ui/badge";
import { until, when } from "@/lib/format";

const terminal = ["succeeded", "failed", "cancelled"];

// useTestDelivery sends a hookbeam.test message to one endpoint and follows its delivery until it settles.
export function useTestDelivery(endpointId: number) {
  const queryClient = useQueryClient();
  const [sent, setSent] = useState<{ messageId: number; deliveryId: number } | null>(null);
  const send = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/test", { params: { path: { endpointId } } })),
    onSuccess: (res) => {
      setSent({ messageId: res.message.id, deliveryId: res.delivery.id });
      queryClient.invalidateQueries({ queryKey: keys.endpointDeliveries(endpointId) });
    },
  });
  const message = useMessage(sent?.messageId ?? null, true);
  const delivery = message.data?.deliveries.find((d) => d.id === sent?.deliveryId) ?? null;
  const settled = delivery ? terminal.includes(delivery.state) : false;
  const attempts = useAttempts(sent?.deliveryId ?? null, !settled);
  // The outcome and its attempt land in one transaction, but the two polls are separate: once the delivery settles,
  // read both once more so the last attempt is never missing.
  useEffect(() => {
    if (!settled) return;
    message.refetch();
    attempts.refetch();
    queryClient.invalidateQueries({ queryKey: keys.endpointDeliveries(endpointId) });
    queryClient.invalidateQueries({ queryKey: keys.endpoint(endpointId) });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settled]);
  return { send, sent, delivery, settled, attempts: attempts.data ?? [], dismiss: () => setSent(null) };
}

// TestDeliveryPanel shows the test that was just sent: its state, each attempt as it lands, and the next retry.
export function TestDeliveryPanel({ test }: { test: ReturnType<typeof useTestDelivery> }) {
  const { send, sent, delivery, settled, attempts } = test;
  if (send.isError) return <FormError message={asApiError(send.error).message} />;
  if (!sent) return null;
  return (
    <section aria-label="Test delivery" className="rounded-lg border border-beam-from/40 bg-beam-from/5 p-4 text-sm" data-testid="test-result">
      <div className="flex flex-wrap items-center gap-3">
        <p className="font-medium">Test delivery</p>
        {delivery ? (
          <Badge tone={stateTone(delivery.state)} dot pulse={!settled} data-testid="test-state">
            {delivery.state}
          </Badge>
        ) : (
          <Badge tone="warn" dot pulse>
            planned
          </Badge>
        )}
        <span className="text-muted-foreground">
          {delivery ? `attempt ${delivery.attemptCount}` : "waiting for the worker"}
          {delivery?.failureReason ? ` · ${delivery.failureReason}` : ""}
          {delivery?.dueAt && !settled ? ` · next attempt ${until(delivery.dueAt)}` : ""}
        </span>
        <span className="ml-auto flex items-center gap-1">
          <Link href={`/events/${sent.messageId}/deliveries/${sent.deliveryId}`} className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground">
            Open delivery <ArrowUpRight className="h-3 w-3" aria-hidden />
          </Link>
          <button type="button" onClick={test.dismiss} aria-label="Dismiss the test delivery" className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground">
            <X className="h-3.5 w-3.5" aria-hidden />
          </button>
        </span>
      </div>
      {attempts.length > 0 && (
        <ol className="mt-3 flex flex-col gap-1.5">
          {attempts.map((a) => {
            const ok = !!a.statusCode && a.statusCode < 300;
            return (
              <li key={a.id} className="flex flex-wrap items-center gap-3 rounded-md border bg-background px-3 py-1.5 text-xs">
                <span className="font-medium tabular-nums">#{a.number}</span>
                <Badge tone={ok ? "good" : "bad"} dot>
                  {a.statusCode ? `HTTP ${a.statusCode}` : (a.errorClass ?? "pending")}
                </Badge>
                <span className="tabular-nums text-muted-foreground">{a.latencyMs} ms</span>
                <span className="text-muted-foreground">{when(a.startedAt)}</span>
                {a.responsePreview ? <code className="truncate text-muted-foreground">{a.responsePreview.slice(0, 80)}</code> : null}
              </li>
            );
          })}
        </ol>
      )}
      {!settled && <p className="mt-2 text-xs text-muted-foreground">Following the delivery…</p>}
    </section>
  );
}
