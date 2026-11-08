"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import { keys, unwrap, useApiKeys, useApplication, useEndpoints, useMe } from "@/api/queries";
import { Field, FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { SecretOnce } from "@/components/ui/secret-once";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { HealthBadge } from "@/components/endpoints/health-badge";
import { when } from "@/lib/format";
import { can, deny } from "@/lib/roles";

export function ApplicationDetail({ id }: { id: number }) {
  const app = useApplication(id);
  if (app.isPending) return <LoadingState />;
  if (app.isError) return <QueryState error={app.error} onRetry={() => app.refetch()} />;
  const a = app.data;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <p className="text-sm text-muted-foreground">
          <Link href="/applications" className="underline">
            Applications
          </Link>{" "}
          / {a.environmentKind}
        </p>
        <h1 className="text-2xl font-semibold" data-testid="application-name">
          {a.name}
        </h1>
        <p className="text-sm text-muted-foreground">
          Created {when(a.createdAt)} {a.archivedAt ? "· archived" : ""}
        </p>
      </header>
      <Endpoints applicationId={id} />
      <Keys applicationId={id} />
      <Settings applicationId={id} enabled={a.responsePreviewsEnabled} />
      <Archive application={a} />
    </div>
  );
}

function Endpoints({ applicationId }: { applicationId: number }) {
  const list = useEndpoints(applicationId);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Endpoints</CardTitle>
        <CardDescription>Destination URLs; each has its own signing secret and subscriptions.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <CreateEndpoint applicationId={applicationId} />
        {list.isPending && <LoadingState />}
        {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
        {list.data && list.data.length === 0 && <EmptyState title="No endpoints yet" hint="Register the URL that should receive this application's events." />}
        {list.data && list.data.length > 0 && (
          <table className="w-full text-sm">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-2 pr-4 font-medium">URL</th>
                <th className="py-2 pr-4 font-medium">Events</th>
                <th className="py-2 pr-4 font-medium">Health</th>
                <th className="py-2 pr-4 font-medium">State</th>
              </tr>
            </thead>
            <tbody>
              {list.data.map((e) => (
                <tr key={e.id} className="border-t align-top">
                  <td className="py-2 pr-4">
                    <Link href={`/endpoints/${e.id}`} className="break-all underline" data-testid="endpoint-link">
                      {e.url}
                    </Link>
                    <span className="block text-xs text-muted-foreground">revision {e.revision}</span>
                  </td>
                  <td className="py-2 pr-4">{e.eventTypes.length === 0 ? <span className="text-muted-foreground">every type</span> : e.eventTypes.join(", ")}</td>
                  <td className="py-2 pr-4">
                    <HealthBadge health={e.health} />
                  </td>
                  <td className="py-2 pr-4">{e.archivedAt ? <Badge>archived</Badge> : e.pausedAt ? <Badge tone="warn">paused</Badge> : <Badge tone="good">active</Badge>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardContent>
    </Card>
  );
}

function CreateEndpoint({ applicationId }: { applicationId: number }) {
  const queryClient = useQueryClient();
  const [url, setUrl] = useState("");
  const [types, setTypes] = useState("");
  const [created, setCreated] = useState<{ secret: string; version: number } | null>(null);
  const create = useMutation({
    mutationFn: () => {
      const eventTypes = types.split(",").map((t) => t.trim()).filter(Boolean);
      return unwrap(api.POST("/v1/applications/{applicationId}/endpoints", { params: { path: { applicationId } }, body: { url, eventTypes } }));
    },
    onSuccess: (res) => {
      setCreated({ secret: res.signingSecret, version: res.secretVersion });
      setUrl("");
      setTypes("");
      queryClient.invalidateQueries({ queryKey: keys.endpoints(applicationId) });
    },
  });
  return (
    <div className="flex flex-col gap-3 rounded-md border p-4">
      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          setCreated(null);
          create.mutate();
        }}
      >
        <Field id="endpoint-url" label="URL" type="url" required value={url} onChange={(e) => setUrl(e.target.value)} className="min-w-72 flex-1" hint="https only, unless the data plane allows private destinations locally." />
        <Field id="endpoint-types" label="Event types" value={types} onChange={(e) => setTypes(e.target.value)} className="min-w-56" hint="Comma-separated; empty means every type." />
        <Button type="submit" disabled={create.isPending}>
          Register endpoint
        </Button>
        <FormError message={create.isError ? asApiError(create.error).message : null} />
      </form>
      {created && <SecretOnce label={`Signing secret, version ${created.version}`} value={created.secret} />}
    </div>
  );
}

function Keys({ applicationId }: { applicationId: number }) {
  const queryClient = useQueryClient();
  const list = useApiKeys();
  const [name, setName] = useState("");
  const [created, setCreated] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/applications/{applicationId}/api-keys", { params: { path: { applicationId } }, body: { name, scopes: ["messages:write", "messages:read"] } })),
    onSuccess: (res) => {
      setCreated(res.key);
      setName("");
      queryClient.invalidateQueries({ queryKey: keys.apiKeys });
    },
  });
  const mine = (list.data ?? []).filter((k) => k.applicationId === applicationId);
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const curl = `curl -X POST ${origin}/v1/applications/${applicationId}/messages \\
  -H "Authorization: Bearer ${created ?? "hb_<your key>"}" \\
  -H "Idempotency-Key: order-817-shipped-v1" \\
  -H "Content-Type: application/json" \\
  -d '{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}'`;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Keys</CardTitle>
        <CardDescription>Bound to this application: they submit here and nowhere else. The key is shown once.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            setCreated(null);
            create.mutate();
          }}
        >
          <Field id="key-name" label="Key name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} className="min-w-64" />
          <Button type="submit" disabled={create.isPending}>
            Create key
          </Button>
          <FormError message={create.isError ? asApiError(create.error).message : null} />
        </form>
        {created && <SecretOnce label="API key" value={created} />}
        <div>
          <p className="mb-1 text-sm font-medium">Submit an event</p>
          <pre className="overflow-x-auto rounded-md bg-secondary p-3 text-xs" data-testid="curl-example">
            {curl}
          </pre>
        </div>
        {list.isPending && <LoadingState />}
        {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
        {list.data && mine.length === 0 && <p className="text-sm text-muted-foreground">No key bound to this application yet.</p>}
        {mine.length > 0 && (
          <ul className="text-sm">
            {mine.map((k) => (
              <li key={k.id} className="flex items-center gap-3 border-t py-2">
                <span className="font-medium">{k.name}</span>
                <code className="text-xs text-muted-foreground">{k.prefix}…</code>
                <span className="text-xs text-muted-foreground">{k.scopes.join(", ")}</span>
                {k.revokedAt && <Badge>revoked</Badge>}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function Settings({ applicationId, enabled }: { applicationId: number; enabled: boolean }) {
  const queryClient = useQueryClient();
  const toggle = useMutation({
    mutationFn: (next: boolean) => unwrap(api.PATCH("/v1/applications/{applicationId}", { params: { path: { applicationId } }, body: { responsePreviewsEnabled: next } })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: keys.application(applicationId) }),
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Settings</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={enabled} onChange={(e) => toggle.mutate(e.target.checked)} disabled={toggle.isPending} />
          Show response previews to people allowed to read payloads
        </label>
        <FormError message={toggle.isError ? asApiError(toggle.error).message : null} />
      </CardContent>
    </Card>
  );
}

// Archive retires the application: its endpoints are archived and their unsent deliveries cancelled. Owners and
// admins only; the confirmation says what stops.
function Archive({ application }: { application: { id: number; name: string; archivedAt?: string | null } }) {
  const me = useMe();
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const [result, setResult] = useState<{ archivedEndpoints: number; cancelledDeliveries: number } | null>(null);
  const archive = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/applications/{applicationId}", { params: { path: { applicationId: application.id } } })),
    onSuccess: (res) => {
      setConfirm(false);
      setResult({ archivedEndpoints: res.archivedEndpoints, cancelledDeliveries: res.cancelledDeliveries });
      queryClient.invalidateQueries({ queryKey: keys.application(application.id) });
      queryClient.invalidateQueries({ queryKey: keys.endpoints(application.id) });
      queryClient.invalidateQueries({ queryKey: ["applications"] });
    },
  });
  const role = me.data?.role ?? null;
  const allowed = can(role, "manageEndpoints");
  return (
    <Card className="border-destructive/30">
      <CardHeader>
        <CardTitle>Archive</CardTitle>
        <CardDescription>
          {application.archivedAt
            ? `Archived ${when(application.archivedAt)}. It no longer accepts events; its history stays readable.`
            : "Stops this application for good: new events are refused, every endpoint is archived and waiting deliveries are cancelled. Attempts already in flight finish and record normally."}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {result && (
          <p role="status" data-testid="archive-result">
            Archived {result.archivedEndpoints} endpoint{result.archivedEndpoints === 1 ? "" : "s"} and cancelled {result.cancelledDeliveries} waiting deliver{result.cancelledDeliveries === 1 ? "y" : "ies"}.
          </p>
        )}
        {!application.archivedAt && !confirm && (
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="destructive" disabled={!allowed} onClick={() => setConfirm(true)} data-testid="archive-application">
              Archive application
            </Button>
            {!allowed && <span className="text-muted-foreground">{deny(role, "manageEndpoints")}</span>}
          </div>
        )}
        {confirm && (
          <div role="dialog" aria-modal="true" aria-labelledby="archive-title" className="rounded-md border bg-card p-4" data-testid="archive-dialog">
            <h3 id="archive-title" className="font-medium">
              Archive “{application.name}”?
            </h3>
            <p className="mt-1 text-muted-foreground">Keys bound to it stop submitting, its endpoints stop receiving, and this cannot be undone.</p>
            <FormError message={archive.isError ? asApiError(archive.error).message : null} />
            <div className="mt-3 flex gap-2">
              <Button variant="destructive" onClick={() => archive.mutate()} disabled={archive.isPending} data-testid="confirm-archive">
                Archive
              </Button>
              <Button variant="ghost" onClick={() => setConfirm(false)}>
                Keep
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
