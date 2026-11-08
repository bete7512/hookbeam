"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

import { createWorkspace, renameWorkspace, switchWorkspace } from "@/api/auth";
import { asApiError } from "@/api/errors";
import { useLimits, useMe, useWorkspaces } from "@/api/queries";
import { Field, FormError, FormNotice } from "@/components/form";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";
import { duration } from "@/lib/format";
import { can, deny } from "@/lib/roles";

// Settings shows what the workspace lives under: its identity, the retention the data plane applies and the
// quotas with current usage. Nothing here is editable yet; limits and retention are operator configuration.
export function Settings() {
  const me = useMe();
  const workspaces = useWorkspaces();
  const limits = useLimits();
  const ws = workspaces.data?.find((w) => w.id === me.data?.workspaceId) ?? null;
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Settings</h1>
        <p className="text-muted-foreground">
          People are on the{" "}
          <Link href="/team" className="underline">
            team
          </Link>{" "}
          page and keys under{" "}
          <Link href="/developers" className="underline">
            developers
          </Link>
          .
        </p>
      </header>
      <Card>
        <CardHeader>
          <CardTitle>Workspace</CardTitle>
          <CardDescription>The workspace is a goauth organization: its name is what invitations and emails show.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {workspaces.isPending && <LoadingState />}
          {ws && (
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              <dt className="text-muted-foreground">name</dt>
              <dd data-testid="workspace-name">{ws.name}</dd>
              <dt className="text-muted-foreground">slug</dt>
              <dd>{ws.slug}</dd>
              <dt className="text-muted-foreground">id</dt>
              <dd className="font-mono text-xs">{ws.id}</dd>
              <dt className="text-muted-foreground">your role</dt>
              <dd data-testid="my-role">{me.data?.role ?? "—"}</dd>
            </dl>
          )}
          {ws && <RenameWorkspace id={ws.id} name={ws.name} role={me.data?.role ?? null} />}
        </CardContent>
      </Card>
      <CreateWorkspace />
      <Card>
        <CardHeader>
          <CardTitle>Retention</CardTitle>
          <CardDescription>What the data plane keeps and for how long. Metadata, deliveries and attempts stay after the payload is dropped.</CardDescription>
        </CardHeader>
        <CardContent>
          {limits.isPending && <LoadingState />}
          {limits.isError && <QueryState error={limits.error} onRetry={() => limits.refetch()} />}
          {limits.data && (
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm" data-testid="retention">
              <dt className="text-muted-foreground">payloads readable and replayable for</dt>
              <dd>{duration(limits.data.retention.payloadSeconds)}</dd>
              <dt className="text-muted-foreground">Idempotency-Key remembered for</dt>
              <dd>{duration(limits.data.retention.idempotencyWindowSeconds)}</dd>
              <dt className="text-muted-foreground">audit events kept for</dt>
              <dd>{duration(limits.data.retention.auditSeconds)}</dd>
            </dl>
          )}
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle>Limits</CardTitle>
          <CardDescription>A 21st endpoint on an application and a fan-out wider than the cap are refused with 422; a submission over the backlog is 429 with Retry-After.</CardDescription>
        </CardHeader>
        <CardContent>
          {limits.data && (
            <div className="flex flex-col gap-4 text-sm" data-testid="limits">
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                <dt className="text-muted-foreground">endpoints per application</dt>
                <dd>{limits.data.limits.endpointsPerApplication}</dd>
                <dt className="text-muted-foreground">endpoints per event (fan-out)</dt>
                <dd>{limits.data.limits.fanoutPerMessage}</dd>
                <dt className="text-muted-foreground">backlog</dt>
                <dd>
                  {limits.data.usage.backlog} of {limits.data.limits.backlogPerWorkspace} pending deliveries
                  <meter className="ml-2 align-middle" min={0} max={limits.data.limits.backlogPerWorkspace} value={limits.data.usage.backlog} aria-label="Backlog used" />
                </dd>
              </dl>
              {limits.data.usage.applications.length > 0 && (
                <table className="w-full">
                  <thead className="text-left text-muted-foreground">
                    <tr>
                      <th className="py-1 pr-4 font-medium">Application</th>
                      <th className="py-1 pr-4 font-medium">Active endpoints</th>
                    </tr>
                  </thead>
                  <tbody>
                    {limits.data.usage.applications.map((a) => (
                      <tr key={a.applicationId} className="border-t">
                        <td className="py-1 pr-4">
                          <Link href={`/applications/${a.applicationId}`} className="underline">
                            {a.name}
                          </Link>
                        </td>
                        <td className="py-1 pr-4">
                          {a.endpoints} of {limits.data.limits.endpointsPerApplication}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function RenameWorkspace({ id, name, role }: { id: string; name: string; role: string | null }) {
  const queryClient = useQueryClient();
  const [value, setValue] = useState(name);
  const [saved, setSaved] = useState<string | null>(null);
  const rename = useMutation({
    mutationFn: () => renameWorkspace(id, value.trim()),
    onSuccess: (w) => {
      setSaved(`Renamed to “${w.name}”.`);
      queryClient.invalidateQueries({ queryKey: ["workspaces"] });
    },
  });
  const allowed = can(role, "manageMembers");
  return (
    <form
      className="flex flex-wrap items-end gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        setSaved(null);
        rename.mutate();
      }}
    >
      <Field id="ws-name" label="Rename" required maxLength={100} value={value} onChange={(e) => setValue(e.target.value)} disabled={!allowed} className="min-w-64" />
      <Button type="submit" variant="outline" disabled={!allowed || rename.isPending || value.trim() === name} data-testid="rename-workspace">
        Save name
      </Button>
      {!allowed && <span className="text-sm text-muted-foreground">{deny(role, "manageMembers")}</span>}
      <FormError message={rename.isError ? asApiError(rename.error).message : null} />
      <FormNotice message={saved} />
    </form>
  );
}

// CreateWorkspace starts another workspace for the same account and switches into it; the person becomes its owner.
function CreateWorkspace() {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: async () => {
      const w = await createWorkspace(name.trim());
      await switchWorkspace(w.id);
      return w;
    },
    onSuccess: () => {
      queryClient.clear();
      window.location.assign("/overview");
    },
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>New workspace</CardTitle>
        <CardDescription>A separate team, keys and applications under the same account. You become its owner and are switched into it.</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <Field id="new-ws-name" label="Name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} className="min-w-64" placeholder="Acme production" />
          <Button type="submit" disabled={create.isPending || name.trim() === ""} data-testid="create-workspace">
            Create and switch
          </Button>
          <FormError message={create.isError ? asApiError(create.error).message : null} />
        </form>
      </CardContent>
    </Card>
  );
}
