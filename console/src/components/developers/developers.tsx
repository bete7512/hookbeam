"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useApiKeys, useApplications, useMe } from "@/api/queries";
import { Field, FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { SecretOnce } from "@/components/ui/secret-once";
import { LoadingState, QueryState } from "@/components/states";
import { useEnvironment } from "@/lib/environment";
import { ago, when } from "@/lib/format";
import { can, deny } from "@/lib/roles";

type Scope = components["schemas"]["CreateApiKeyRequest"]["scopes"] extends (infer S)[] | undefined ? S : never;
type ApiKey = components["schemas"]["ApiKey"];

const scopeHelp: Record<Scope, string> = {
  "messages:write": "submit events",
  "messages:read": "read messages, deliveries and attempts",
  "payloads:read": "read retained payloads and response previews",
  "deliveries:replay": "replay failed or cancelled deliveries",
};

// Developers is where keys are minted and revoked, with the examples a backend needs to start submitting.
export function Developers() {
  const me = useMe();
  const role = me.data?.role ?? null;
  const list = useApiKeys();
  const [created, setCreated] = useState<components["schemas"]["ApiKeyCreated"] | null>(null);
  if (me.isPending) return <LoadingState />;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Developers</h1>
        <p className="text-muted-foreground">
          API keys for the systems that submit and read events. A key is shown once; Hookbeam keeps only its prefix and a hash. The full reference is at{" "}
          <a href="/docs" className="underline" target="_blank" rel="noreferrer">
            /docs
          </a>
          .
        </p>
      </header>
      <CreateKey role={role} onCreated={setCreated} />
      {created && (
        <>
          <SecretOnce label={`API key “${created.name}”`} value={created.key} />
          <Examples apiKey={created} />
        </>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Keys</CardTitle>
          <CardDescription>Every key in the workspace, including revoked ones. Revocation is immediate and cannot be undone.</CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending && <LoadingState />}
          {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
          {list.data && list.data.length === 0 && <p className="text-sm text-muted-foreground">No key yet.</p>}
          {list.data && list.data.length > 0 && (
            <table className="w-full text-sm" data-testid="keys">
              <thead className="text-left text-muted-foreground">
                <tr>
                  <th className="py-2 pr-4 font-medium">Name</th>
                  <th className="py-2 pr-4 font-medium">Prefix</th>
                  <th className="py-2 pr-4 font-medium">Scopes</th>
                  <th className="py-2 pr-4 font-medium">Binding</th>
                  <th className="py-2 pr-4 font-medium">Last used</th>
                  <th className="py-2 pr-4 font-medium">Expires</th>
                  <th className="py-2 pr-4 font-medium"></th>
                </tr>
              </thead>
              <tbody>
                {list.data.map((k) => (
                  <KeyRow key={k.id} apiKey={k} role={role} />
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function CreateKey({ role, onCreated }: { role: string | null; onCreated: (k: components["schemas"]["ApiKeyCreated"]) => void }) {
  const queryClient = useQueryClient();
  const { current } = useEnvironment();
  const apps = useApplications(current?.id ?? null);
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<Scope[]>(["messages:write"]);
  const [binding, setBinding] = useState<string>("");
  const [expiresAt, setExpiresAt] = useState("");
  const create = useMutation({
    mutationFn: () => {
      const body = { name, scopes, expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined };
      return binding
        ? unwrap(api.POST("/v1/applications/{applicationId}/api-keys", { params: { path: { applicationId: Number(binding) } }, body }))
        : unwrap(api.POST("/v1/api-keys", { body }));
    },
    onSuccess: (k) => {
      onCreated(k);
      setName("");
      queryClient.invalidateQueries({ queryKey: keys.apiKeys });
    },
  });
  const allowed = can(role, "manageKeys");
  return (
    <Card>
      <CardHeader>
        <CardTitle>Create a key</CardTitle>
        <CardDescription>{allowed ? "Give it the fewest scopes that do the job; bind it to one application when it submits for one system." : deny(role, "manageKeys") + "."}</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="grid gap-4 sm:grid-cols-2"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <Field id="key-name" label="Name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} disabled={!allowed} placeholder="production backend" />
          <div className="flex flex-col gap-1">
            <label htmlFor="key-binding" className="text-sm font-medium">
              Binding
            </label>
            <select id="key-binding" className="h-9 rounded-md border border-input bg-background px-2 text-sm" value={binding} onChange={(e) => setBinding(e.target.value)} disabled={!allowed} data-testid="key-binding">
              <option value="">whole workspace</option>
              {(apps.data ?? []).map((a) => (
                <option key={a.id} value={a.id}>
                  only {a.name}
                </option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground">Applications of the {current?.name ?? "current"} environment.</p>
          </div>
          <fieldset className="flex flex-col gap-1">
            <legend className="text-sm font-medium">Scopes</legend>
            {(Object.keys(scopeHelp) as Scope[]).map((s) => (
              <label key={s} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={scopes.includes(s)}
                  disabled={!allowed}
                  onChange={(e) => setScopes((prev) => (e.target.checked ? [...prev, s] : prev.filter((x) => x !== s)))}
                  data-testid={`scope-${s}`}
                />
                <code className="text-xs">{s}</code>
                <span className="text-muted-foreground">{scopeHelp[s]}</span>
              </label>
            ))}
          </fieldset>
          <Field id="key-expires" label="Expires" type="datetime-local" value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} disabled={!allowed} hint="Leave empty for a key that lives until revoked." />
          <div className="flex items-center gap-3 sm:col-span-2">
            <Button type="submit" disabled={!allowed || create.isPending || scopes.length === 0} data-testid="create-key">
              Create key
            </Button>
            {!allowed && (
              <span className="text-sm text-muted-foreground" data-testid="create-key-reason">
                {deny(role, "manageKeys")}
              </span>
            )}
            <FormError message={create.isError ? asApiError(create.error).message : null} />
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function KeyRow({ apiKey, role }: { apiKey: ApiKey; role: string | null }) {
  const queryClient = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const revoke = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/api-keys/{id}", { params: { path: { id: apiKey.id } } })),
    onSuccess: () => {
      setConfirm(false);
      queryClient.invalidateQueries({ queryKey: keys.apiKeys });
    },
  });
  const allowed = can(role, "manageKeys");
  const revoked = !!apiKey.revokedAt;
  return (
    <tr className={`border-t align-top ${revoked ? "text-muted-foreground" : ""}`} data-testid="key-row">
      <td className="py-2 pr-4">
        <span className="font-medium">{apiKey.name}</span>
        {revoked && (
          <Badge className="ml-2" tone="neutral">
            revoked {ago(apiKey.revokedAt)}
          </Badge>
        )}
      </td>
      <td className="py-2 pr-4 font-mono text-xs" data-testid="key-prefix">
        {apiKey.prefix}…
      </td>
      <td className="py-2 pr-4">
        <span className="flex flex-wrap gap-1">
          {apiKey.scopes.map((s) => (
            <Badge key={s}>{s}</Badge>
          ))}
        </span>
      </td>
      <td className="py-2 pr-4">
        {apiKey.applicationId ? (
          <Link href={`/applications/${apiKey.applicationId}`} className="underline">
            application {apiKey.applicationId}
          </Link>
        ) : (
          "workspace"
        )}
      </td>
      <td className="py-2 pr-4" title={when(apiKey.lastUsedAt)}>
        {ago(apiKey.lastUsedAt)}
      </td>
      <td className="py-2 pr-4">{apiKey.expiresAt ? when(apiKey.expiresAt) : "never"}</td>
      <td className="py-2 pr-4 text-right">
        {!revoked && allowed && !confirm && (
          <Button variant="ghost" size="sm" onClick={() => setConfirm(true)} data-testid="revoke-key">
            Revoke
          </Button>
        )}
        {confirm && (
          <span className="inline-flex items-center gap-2 text-xs">
            Systems using {apiKey.prefix}… stop working now.
            <Button variant="destructive" size="sm" onClick={() => revoke.mutate()} disabled={revoke.isPending} data-testid="confirm-revoke">
              Revoke
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setConfirm(false)}>
              Keep
            </Button>
          </span>
        )}
        <FormError message={revoke.isError ? asApiError(revoke.error).message : null} />
      </td>
    </tr>
  );
}

function Examples({ apiKey }: { apiKey: components["schemas"]["ApiKeyCreated"] }) {
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const appId = apiKey.applicationId ?? "<application id>";
  const curl = `curl -X POST ${origin}/v1/applications/${appId}/messages \\
  -H "Authorization: Bearer ${apiKey.key}" \\
  -H "Idempotency-Key: order-817-shipped-v1" \\
  -H "Content-Type: application/json" \\
  -d '{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}'`;
  const node = `const res = await fetch("${origin}/v1/applications/${appId}/messages", {
  method: "POST",
  headers: { authorization: "Bearer ${apiKey.key}", "idempotency-key": "order-817-shipped-v1", "content-type": "application/json" },
  body: JSON.stringify({ type: "order.shipped", external_id: "order-817", data: { order_id: "817" } }),
});
const { message } = await res.json(); // then GET ${origin}/v1/messages/<id> with messages:read`;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Submit an event with this key</CardTitle>
        <CardDescription>The same Idempotency-Key with the same body is accepted once; a different body under the same key is a conflict.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <Snippet label="curl" text={curl} />
        <Snippet label="Node" text={node} />
      </CardContent>
    </Card>
  );
}

function Snippet({ label, text }: { label: string; text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <p className="text-sm font-medium">{label}</p>
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            navigator.clipboard
              .writeText(text)
              .then(() => setCopied(true))
              .catch(() => setCopied(false))
          }
        >
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <pre className="overflow-x-auto rounded-md bg-secondary p-3 text-xs" data-testid={`snippet-${label}`}>
        {text}
      </pre>
    </div>
  );
}
