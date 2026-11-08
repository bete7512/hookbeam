"use client";

import Link from "next/link";

import { useLimits, useMe, useWorkspaces } from "@/api/queries";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";
import { duration } from "@/lib/format";

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
        </CardHeader>
        <CardContent>
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
        </CardContent>
      </Card>
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
