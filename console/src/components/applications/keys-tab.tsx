"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Ban, KeyRound, Plus } from "lucide-react";
import { useState } from "react";

import { api } from "@/api/client";
import { asApiError } from "@/api/errors";
import type { components } from "@/api/schema";
import { keys, unwrap, useApiKeys } from "@/api/queries";
import { Field, FormError } from "@/components/form";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { CodeSnippet } from "@/components/ui/code-snippet";
import { Dialog, DialogActions } from "@/components/ui/dialog";
import { SecretOnce } from "@/components/ui/secret-once";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { ago, when } from "@/lib/format";

type ApiKey = components["schemas"]["ApiKey"];
type ApiKeyCreated = components["schemas"]["ApiKeyCreated"];
type Scope = ApiKey["scopes"][number];

const scopeHelp: Record<Scope, string> = {
  "messages:write": "submit events",
  "messages:read": "read events, deliveries and attempts",
  "payloads:read": "read retained payloads and response previews",
  "deliveries:replay": "replay failed or cancelled deliveries",
};

// KeysTab manages the keys bound to this application: they submit here and nowhere else. A key that leaked is
// revoked here, which stops it at once.
export function KeysTab({ applicationId, archived, mayManage, reason }: { applicationId: number; archived: boolean; mayManage: boolean; reason: string }) {
  const list = useApiKeys();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<ApiKeyCreated | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [showRevoked, setShowRevoked] = useState(false);
  const mine = (list.data ?? []).filter((k) => k.applicationId === applicationId);
  const revokedCount = mine.filter((k) => k.revokedAt).length;
  const shown = mine.filter((k) => showRevoked || !k.revokedAt);
  const blocked = archived ? "an archived application takes no new keys" : !mayManage ? reason : null;
  // The same action sits in the toolbar and in the empty state; only the toolbar one carries the test id.
  const create = (testId?: string) => (
    <Button onClick={() => setCreating(true)} disabled={!!blocked} data-testid={testId}>
      <Plus className="h-4 w-4" aria-hidden />
      Create key
    </Button>
  );
  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">Keys bound to this application. Hookbeam stores a prefix and a hash, never the key.</p>
        <div className="flex flex-col items-end gap-1">
          {create("create-key")}
          {blocked && <span className="text-xs text-muted-foreground">{blocked}</span>}
        </div>
      </div>
      {list.isPending && <LoadingState label="Loading keys…" />}
      {list.isError && <QueryState error={list.error} onRetry={() => list.refetch()} />}
      {list.data && mine.length === 0 && <EmptyState title="No key bound to this application" hint="A backend needs a key to submit events here." action={blocked ? undefined : create()} />}
      {mine.length > 0 && (
        <div className="overflow-hidden rounded-lg border bg-card">
          <div className="overflow-x-auto">
            <table data-testid="keys">
              <thead>
                <tr className="border-b">
                  <th className="px-4 py-2.5">Name</th>
                  <th className="px-4 py-2.5">Key</th>
                  <th className="px-4 py-2.5">Scopes</th>
                  <th className="px-4 py-2.5">Last used</th>
                  <th className="px-4 py-2.5">Expires</th>
                  <th className="px-4 py-2.5">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {shown.map((k) => (
                  <KeyRow key={k.id} apiKey={k} mayManage={mayManage} onRevoke={() => setRevoking(k)} />
                ))}
                {shown.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-6 text-center text-sm text-muted-foreground">
                      Every key of this application is revoked.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {revokedCount > 0 && (
            <div className="border-t px-4 py-2 text-xs text-muted-foreground">
              <button type="button" className="underline underline-offset-2 hover:text-foreground" onClick={() => setShowRevoked((s) => !s)} data-testid="toggle-revoked">
                {showRevoked ? "Hide" : "Show"} {revokedCount} revoked
              </button>
            </div>
          )}
        </div>
      )}
      <Quickstart applicationId={applicationId} apiKey={created?.key ?? null} />
      {creating && (
        <CreateKeyDialog
          applicationId={applicationId}
          onClose={() => setCreating(false)}
          onCreated={(k) => {
            setCreated(k);
            setShowRevoked(false);
          }}
        />
      )}
      {revoking && <RevokeKeyDialog apiKey={revoking} onClose={() => setRevoking(null)} />}
    </>
  );
}

function KeyRow({ apiKey: k, mayManage, onRevoke }: { apiKey: ApiKey; mayManage: boolean; onRevoke: () => void }) {
  const revoked = !!k.revokedAt;
  const expired = !!k.expiresAt && new Date(k.expiresAt).getTime() < Date.now();
  return (
    <tr className={`border-b last:border-b-0 ${revoked ? "text-muted-foreground" : ""}`} data-testid="key-row">
      <td className="px-4 py-3">
        <span className="flex items-center gap-2 font-medium">
          <KeyRound className="h-3.5 w-3.5 text-muted-foreground" aria-hidden />
          {k.name}
        </span>
      </td>
      <td className="px-4 py-3">
        <code className="rounded bg-secondary px-1.5 py-0.5 text-xs" data-testid="key-prefix">
          {k.prefix}…
        </code>
      </td>
      <td className="px-4 py-3">
        <span className="flex flex-wrap gap-1">
          {k.scopes.map((s) => (
            <Badge key={s}>{s}</Badge>
          ))}
        </span>
      </td>
      <td className="px-4 py-3 text-muted-foreground" title={when(k.lastUsedAt)}>
        {ago(k.lastUsedAt)}
      </td>
      <td className="px-4 py-3 text-muted-foreground">{k.expiresAt ? when(k.expiresAt) : "never"}</td>
      <td className="px-4 py-3 text-right">
        {revoked ? (
          <Badge title={when(k.revokedAt)}>revoked {ago(k.revokedAt)}</Badge>
        ) : expired ? (
          <Badge tone="warn">expired</Badge>
        ) : mayManage ? (
          <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={onRevoke} data-testid="revoke-key">
            <Ban className="h-3.5 w-3.5" aria-hidden />
            Revoke
          </Button>
        ) : (
          <Badge tone="good" dot>
            active
          </Badge>
        )}
      </td>
    </tr>
  );
}

// CreateKeyDialog mints a bound key and then stays open on the key itself, which is shown this once.
function CreateKeyDialog({ applicationId, onClose, onCreated }: { applicationId: number; onClose: () => void; onCreated: (k: ApiKeyCreated) => void }) {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<Scope[]>(["messages:write", "messages:read"]);
  const [expiresAt, setExpiresAt] = useState("");
  const [created, setCreated] = useState<ApiKeyCreated | null>(null);
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/v1/applications/{applicationId}/api-keys", {
          params: { path: { applicationId } },
          body: { name: name.trim(), scopes, expiresAt: expiresAt ? new Date(expiresAt).toISOString() : undefined },
        }),
      ),
    onSuccess: (k) => {
      setCreated(k);
      onCreated(k);
      queryClient.invalidateQueries({ queryKey: keys.apiKeys });
    },
  });
  if (created) {
    return (
      <Dialog open onClose={onClose} title="Key created" description="Store it in your backend's secret manager. The example on this page now uses it until you leave.">
        <SecretOnce label={`API key “${created.name}”`} value={created.key} hint="Hookbeam keeps only its prefix and a hash." />
        <DialogActions>
          <Button onClick={onClose} data-testid="key-done">
            Done
          </Button>
        </DialogActions>
      </Dialog>
    );
  }
  return (
    <Dialog open onClose={onClose} title="Create key" description="The key is bound to this application: it cannot submit anywhere else.">
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Field id="key-name" label="Key name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} placeholder="production backend" autoFocus autoComplete="off" />
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 text-sm font-medium">Scopes</legend>
          {(Object.keys(scopeHelp) as Scope[]).map((s) => (
            <label key={s} className="flex cursor-pointer items-start gap-3 rounded-md border bg-background px-3 py-2 text-sm transition-colors has-[:checked]:border-beam-from/50 has-[:checked]:bg-beam-from/5">
              <input type="checkbox" className="mt-0.5 accent-[var(--primary)]" checked={scopes.includes(s)} onChange={(e) => setScopes((prev) => (e.target.checked ? [...prev, s] : prev.filter((x) => x !== s)))} data-testid={`scope-${s}`} />
              <span className="flex flex-col">
                <code className="text-xs">{s}</code>
                <span className="text-xs text-muted-foreground">{scopeHelp[s]}</span>
              </span>
            </label>
          ))}
        </fieldset>
        <Field id="key-expires" label="Expires" type="datetime-local" value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} hint="Leave empty for a key that lives until it is revoked." />
        <FormError message={create.isError ? asApiError(create.error).message : null} />
        <DialogActions>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={create.isPending || scopes.length === 0 || name.trim() === ""} data-testid="submit-key">
            {create.isPending ? "Creating…" : "Create key"}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  );
}

// RevokeKeyDialog confirms a revocation, which is immediate and final. Mount it only while it is open.
function RevokeKeyDialog({ apiKey, onClose }: { apiKey: ApiKey; onClose: () => void }) {
  const queryClient = useQueryClient();
  const revoke = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/api-keys/{id}", { params: { path: { id: apiKey.id } } })),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.apiKeys });
      onClose();
    },
  });
  return (
    <Dialog open onClose={onClose} title={`Revoke “${apiKey.name}”?`} description="Use this when a key leaked or is no longer needed." data-testid="revoke-dialog">
      <p className="text-sm text-muted-foreground">
        Every system using <code className="rounded bg-secondary px-1.5 py-0.5 text-xs text-foreground">{apiKey.prefix}…</code> is refused from the next request on. A revoked key cannot be restored; create a new one and deploy it first if the system must keep submitting.
      </p>
      <FormError message={revoke.isError ? asApiError(revoke.error).message : null} />
      <DialogActions>
        <Button variant="ghost" onClick={onClose}>
          Keep it
        </Button>
        <Button variant="destructive" onClick={() => revoke.mutate()} disabled={revoke.isPending} data-testid="confirm-revoke">
          {revoke.isPending ? "Revoking…" : "Revoke key"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

// Quickstart is the request a backend sends; it carries the key that was just created, or a placeholder.
function Quickstart({ applicationId, apiKey }: { applicationId: number; apiKey: string | null }) {
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const key = apiKey ?? "hb_<your key>";
  const curl = `curl -X POST ${origin}/v1/applications/${applicationId}/messages \\
  -H "Authorization: Bearer ${key}" \\
  -H "Idempotency-Key: order-817-shipped-v1" \\
  -H "Content-Type: application/json" \\
  -d '{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}'`;
  const node = `const res = await fetch("${origin}/v1/applications/${applicationId}/messages", {
  method: "POST",
  headers: {
    authorization: "Bearer ${key}",
    "idempotency-key": "order-817-shipped-v1",
    "content-type": "application/json",
  },
  body: JSON.stringify({ type: "order.shipped", external_id: "order-817", data: { order_id: "817" } }),
});
const { messageId } = await res.json();`;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Submit an event</CardTitle>
        <CardDescription>The same Idempotency-Key with the same body is accepted once; a different body under the same key is a conflict.</CardDescription>
      </CardHeader>
      <CardContent>
        <CodeSnippet
          snippets={[
            { label: "curl", code: curl },
            { label: "Node", code: node },
          ]}
          data-testid="curl-example"
        />
      </CardContent>
    </Card>
  );
}
