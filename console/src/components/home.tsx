"use client";

import Link from "next/link";

import { useMe, useReadiness } from "@/api/queries";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";

// Home is the overview placeholder: the API's readiness through the shared origin and the caller's identity.
export function Home() {
  return (
    <div className="flex flex-col gap-6">
      <header>
        <h1 className="text-2xl font-semibold">Overview</h1>
        <p className="text-muted-foreground">Webhook delivery you can see.</p>
      </header>
      <ReadinessCard />
      <SessionCard />
    </div>
  );
}

function ReadinessCard() {
  const ready = useReadiness();
  return (
    <Card>
      <CardHeader>
        <CardTitle>API</CardTitle>
        <CardDescription>What the control plane reports through the shared origin.</CardDescription>
      </CardHeader>
      <CardContent>
        {ready.isPending && <LoadingState label="Checking…" />}
        {ready.isError && <QueryState error={ready.error} onRetry={() => ready.refetch()} />}
        {ready.data && (
          <dl className="grid grid-cols-2 gap-1 text-sm">
            <dt className="text-muted-foreground">status</dt>
            <dd>{ready.data.status}</dd>
            {Object.entries(ready.data.checks ?? {}).map(([name, state]) => (
              <Check key={name} name={name} state={String(state)} />
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  );
}

function Check({ name, state }: { name: string; state: string }) {
  return (
    <>
      <dt className="text-muted-foreground">{name}</dt>
      <dd>{state}</dd>
    </>
  );
}

function SessionCard() {
  const me = useMe();
  return (
    <Card>
      <CardHeader>
        <CardTitle>Session</CardTitle>
        <CardDescription>The cookie goauth set at login, read back through the same origin.</CardDescription>
      </CardHeader>
      <CardContent>
        {me.isPending && <LoadingState />}
        {me.isError && <QueryState error={me.error} onRetry={() => me.refetch()} />}
        {me.data === null && (
          <p className="text-sm">
            Not signed in.{" "}
            <Link href="/login" className="underline">
              Sign in
            </Link>
          </p>
        )}
        {me.data && (
          <dl className="grid grid-cols-2 gap-1 text-sm">
            <dt className="text-muted-foreground">user</dt>
            <dd data-testid="user-id">{me.data.userId ?? "api key"}</dd>
            <dt className="text-muted-foreground">workspace</dt>
            <dd>{me.data.workspaceId ?? "none selected"}</dd>
            <dt className="text-muted-foreground">methods</dt>
            <dd>{me.data.methods.join(", ")}</dd>
            <dt className="text-muted-foreground">expires</dt>
            <dd>{me.data.expiresAt ? new Date(me.data.expiresAt).toLocaleString() : "never"}</dd>
          </dl>
        )}
      </CardContent>
    </Card>
  );
}
