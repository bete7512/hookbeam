"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect, useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useAttempts, useEndpoint, useMessage } from "@/api/queries";
import { Field, FormError, FormNotice } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { SecretOnce } from "@/components/ui/secret-once";
import { LoadingState, QueryState } from "@/components/states";
import { HealthBadge } from "@/components/endpoints/health-badge";
import { when } from "@/lib/format";

type Endpoint = components["schemas"]["Endpoint"];

export function EndpointDetail({ id }: { id: number }) {
  const ep = useEndpoint(id);
  if (ep.isPending) return <LoadingState />;
  if (ep.isError) return <QueryState error={ep.error} onRetry={() => ep.refetch()} />;
  const e = ep.data;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <p className="text-sm text-muted-foreground">
          <Link href={`/applications/${e.applicationId}`} className="underline">
            Application
          </Link>{" "}
          / endpoint {e.id}
        </p>
        <h1 className="break-all text-2xl font-semibold" data-testid="endpoint-url">
          {e.url}
        </h1>
        <div className="mt-2 flex flex-wrap items-center gap-3 text-sm">
          <HealthBadge health={e.health} />
          {e.archivedAt ? <Badge>archived</Badge> : e.pausedAt ? <Badge tone="warn">paused</Badge> : <Badge tone="good">active</Badge>}
          <span className="text-muted-foreground">revision {e.revision}</span>
          <span className="text-muted-foreground" data-testid="waiting">
            {e.waiting ?? 0} waiting
          </span>
        </div>
      </header>
      <TestDelivery endpoint={e} />
      <Lifecycle endpoint={e} />
      <Edit endpoint={e} />
      <Secrets endpoint={e} />
    </div>
  );
}

function TestDelivery({ endpoint }: { endpoint: Endpoint }) {
  const [deliveryId, setDeliveryId] = useState<number | null>(null);
  const [messageId, setMessageId] = useState<number | null>(null);
  const send = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/test", { params: { path: { endpointId: endpoint.id } } })),
    onSuccess: (res) => {
      setMessageId(res.message.id);
      setDeliveryId(res.delivery.id);
    },
  });
  const message = useMessage(messageId, true);
  const delivery = message.data?.deliveries.find((d) => d.id === deliveryId) ?? null;
  const settled = delivery ? ["succeeded", "failed", "cancelled"].includes(delivery.state) : false;
  const attempts = useAttempts(deliveryId, !settled);
  // The outcome and its attempt land in one transaction, but the two polls are separate: once the delivery settles,
  // read both once more so the last attempt is never missing.
  useEffect(() => {
    if (settled) {
      message.refetch();
      attempts.refetch();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settled]);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Test delivery</CardTitle>
        <CardDescription>Plans one hookbeam.test message for this endpoint alone; the worker attempts it next and the attempt appears below.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-center gap-3">
          <Button onClick={() => send.mutate()} disabled={send.isPending || !!endpoint.archivedAt} data-testid="send-test">
            Send test delivery
          </Button>
          <FormError message={send.isError ? asApiError(send.error).message : null} />
        </div>
        {delivery && (
          <div className="rounded-md border p-3 text-sm" data-testid="test-result">
            <p>
              Delivery {delivery.id}: <Badge tone={delivery.state === "succeeded" ? "good" : delivery.state === "failed" ? "bad" : "warn"} data-testid="test-state">{delivery.state}</Badge>
              {delivery.failureReason ? ` · ${delivery.failureReason}` : ""} · attempt {delivery.attemptCount}
              {delivery.dueAt && !settled ? ` · next ${when(delivery.dueAt)}` : ""}
            </p>
            {attempts.data && attempts.data.length > 0 && (
              <ul className="mt-2 text-xs">
                {attempts.data.map((a) => (
                  <li key={a.id} className="border-t py-1">
                    #{a.number} {a.statusCode ? `HTTP ${a.statusCode}` : (a.errorClass ?? "pending")} · {a.latencyMs} ms · {when(a.startedAt)}
                    {a.responsePreview ? <code className="ml-2 text-muted-foreground">{a.responsePreview.slice(0, 80)}</code> : null}
                  </li>
                ))}
              </ul>
            )}
            {!settled && <p className="mt-2 text-xs text-muted-foreground">Following the delivery…</p>}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Lifecycle({ endpoint }: { endpoint: Endpoint }) {
  const queryClient = useQueryClient();
  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: keys.endpoint(endpoint.id) });
    queryClient.invalidateQueries({ queryKey: keys.endpoints(endpoint.applicationId) });
  };
  const pause = useMutation({ mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/pause", { params: { path: { endpointId: endpoint.id } } })), onSuccess: invalidate });
  const resume = useMutation({ mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/resume", { params: { path: { endpointId: endpoint.id } } })), onSuccess: invalidate });
  const archive = useMutation({ mutationFn: () => unwrap(api.DELETE("/v1/endpoints/{endpointId}", { params: { path: { endpointId: endpoint.id } } })), onSuccess: invalidate });
  const [confirmArchive, setConfirmArchive] = useState(false);
  const err = [pause, resume, archive].find((m) => m.isError)?.error;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Pause, resume, archive</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {!endpoint.archivedAt && !endpoint.pausedAt && (
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="outline" onClick={() => pause.mutate()} disabled={pause.isPending} data-testid="pause">
              Pause
            </Button>
            <p className="text-muted-foreground">
              <span data-testid="waiting-notice">{endpoint.waiting ?? 0} deliveries are waiting</span> and will hold; attempts already claimed may still finish and record their outcome.
            </p>
          </div>
        )}
        {!endpoint.archivedAt && endpoint.pausedAt && (
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="outline" onClick={() => resume.mutate()} disabled={resume.isPending} data-testid="resume">
              Resume
            </Button>
            <p className="text-muted-foreground">
              Paused {when(endpoint.pausedAt)}; <span data-testid="waiting-notice">{endpoint.waiting ?? 0} deliveries are waiting</span> and drain on resume.
            </p>
          </div>
        )}
        {!endpoint.archivedAt && (
          <div className="flex flex-wrap items-center gap-3">
            {!confirmArchive ? (
              <Button variant="ghost" onClick={() => setConfirmArchive(true)}>
                Archive…
              </Button>
            ) : (
              <>
                <Button variant="outline" onClick={() => archive.mutate()} disabled={archive.isPending} data-testid="archive">
                  Archive and cancel {endpoint.waiting ?? 0} waiting
                </Button>
                <Button variant="ghost" onClick={() => setConfirmArchive(false)}>
                  Keep
                </Button>
              </>
            )}
            <p className="text-muted-foreground">Archiving stops new deliveries and cancels the waiting ones; history stays readable.</p>
          </div>
        )}
        {archive.data && <FormNotice message={`Archived; ${archive.data.cancelledDeliveries} waiting deliveries were cancelled.`} />}
        {endpoint.archivedAt && !archive.data && <p className="text-muted-foreground">Archived {when(endpoint.archivedAt)}.</p>}
        <FormError message={err ? asApiError(err).message : null} />
      </CardContent>
    </Card>
  );
}

function Edit({ endpoint }: { endpoint: Endpoint }) {
  const queryClient = useQueryClient();
  const [url, setUrl] = useState(endpoint.url);
  const [description, setDescription] = useState(endpoint.description);
  const [types, setTypes] = useState(endpoint.eventTypes.join(", "));
  useEffect(() => {
    setUrl(endpoint.url);
    setDescription(endpoint.description);
    setTypes(endpoint.eventTypes.join(", "));
  }, [endpoint.url, endpoint.description, endpoint.eventTypes, endpoint.revision]);
  const save = useMutation({
    mutationFn: () => {
      const eventTypes = types.split(",").map((t) => t.trim()).filter(Boolean);
      return unwrap(api.PATCH("/v1/endpoints/{endpointId}", { params: { path: { endpointId: endpoint.id } }, body: { url, description, eventTypes, expectedRevision: endpoint.revision } }));
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.endpoint(endpoint.id) });
      queryClient.invalidateQueries({ queryKey: keys.endpoints(endpoint.applicationId) });
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Configuration</CardTitle>
        <CardDescription>Saving bumps the revision; the save is conditional on revision {endpoint.revision} so two editors cannot overwrite each other. Deliveries already planned keep their revision.</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="flex max-w-2xl flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field id="edit-url" label="URL" type="url" required value={url} onChange={(e) => setUrl(e.target.value)} disabled={!!endpoint.archivedAt} />
          <Field id="edit-description" label="Description" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} disabled={!!endpoint.archivedAt} />
          <Field id="edit-types" label="Event types" value={types} onChange={(e) => setTypes(e.target.value)} hint="Comma-separated; empty means every type." disabled={!!endpoint.archivedAt} />
          <FormError message={save.isError ? asApiError(save.error).message : null} />
          <FormNotice message={save.isSuccess ? `Saved as revision ${save.data.revision}.` : null} />
          <div>
            <Button type="submit" disabled={save.isPending || !!endpoint.archivedAt} data-testid="save-endpoint">
              Save (expects revision {endpoint.revision})
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function Secrets({ endpoint }: { endpoint: Endpoint }) {
  const queryClient = useQueryClient();
  const [retireIn, setRetireIn] = useState("24h");
  const [rotated, setRotated] = useState<{ secret: string; version: number; retireAt: string } | null>(null);
  const rotate = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/endpoints/{endpointId}/rotate-secret", { params: { path: { endpointId: endpoint.id } }, body: { retireIn } })),
    onSuccess: (res) => {
      setRotated({ secret: res.signingSecret, version: res.secretVersion, retireAt: res.retireAt });
      queryClient.invalidateQueries({ queryKey: keys.endpoint(endpoint.id) });
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Signing secrets</CardTitle>
        <CardDescription>Rotation adds a version and keeps the old ones signing until they retire; every attempt carries one signature per active version.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            rotate.mutate();
          }}
        >
          <Field id="retire-in" label="Retire the current secret in" value={retireIn} onChange={(e) => setRetireIn(e.target.value)} hint="A duration such as 24h; 0s retires it now (emergency)." className="min-w-56" disabled={!!endpoint.archivedAt} />
          <Button type="submit" variant="outline" disabled={rotate.isPending || !!endpoint.archivedAt} data-testid="rotate">
            Rotate secret
          </Button>
          <FormError message={rotate.isError ? asApiError(rotate.error).message : null} />
        </form>
        {rotated && <SecretOnce label={`New signing secret, version ${rotated.version} (previous versions retire ${when(rotated.retireAt)})`} value={rotated.secret} />}
        <table className="w-full text-sm" data-testid="secret-versions">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-1 pr-4 font-medium">Version</th>
              <th className="py-1 pr-4 font-medium">Fingerprint</th>
              <th className="py-1 pr-4 font-medium">Created</th>
              <th className="py-1 pr-4 font-medium">Retires</th>
            </tr>
          </thead>
          <tbody>
            {(endpoint.secrets ?? []).map((s) => (
              <tr key={s.version} className="border-t">
                <td className="py-1 pr-4">{s.version}</td>
                <td className="py-1 pr-4 font-mono text-xs">{s.fingerprint}</td>
                <td className="py-1 pr-4">{when(s.createdAt)}</td>
                <td className="py-1 pr-4">{s.retireAt ? when(s.retireAt) : <span className="text-muted-foreground">current</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CardContent>
    </Card>
  );
}
