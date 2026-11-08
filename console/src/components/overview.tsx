"use client";

import Link from "next/link";

import { useApplications, useOverview } from "@/api/queries";
import { GettingStarted } from "@/components/getting-started";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { LoadingState, QueryState } from "@/components/states";
import { useEnvironment } from "@/lib/environment";
import { ago } from "@/lib/format";

// Overview is the environment's dashboard: plain tiles with the age of what they show, and the getting-started
// flow until the environment has an application.
export function Overview() {
  const { current } = useEnvironment();
  const envId = current?.id ?? null;
  const overview = useOverview(envId);
  const apps = useApplications(envId);
  return (
    <div className="flex flex-col gap-6">
      <header className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Overview</h1>
          <p className="text-muted-foreground">{current ? `${current.name} · last 24 hours` : "Pick an environment"}</p>
        </div>
        <Link href="/applications" className="text-sm underline">
          Applications
        </Link>
      </header>
      {envId === null && <LoadingState label="Waiting for the environment…" />}
      {overview.isPending && envId !== null && <LoadingState label="Reading the tiles…" />}
      {overview.isError && <QueryState error={overview.error} onRetry={() => overview.refetch()} />}
      {overview.data && (
        <section aria-label="Tiles" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Tile title="Accepted" value={overview.data.accepted} hint="messages in the window" />
          <Tile title="Succeeded" value={overview.data.succeeded} hint="deliveries in the window" />
          <Tile title="Retry backlog" value={overview.data.backlog} hint="queued and scheduled right now" />
          <Tile title="Oldest pending" value={overview.data.oldestPendingDueAt ? ago(overview.data.oldestPendingDueAt) : "none"} hint="age of the oldest waiting delivery" />
          <Tile title="Exhausted" value={overview.data.exhausted} hint="gave up after the last retry" />
          <Tile
            title="Endpoint health"
            value={`${overview.data.endpoints.healthy} healthy · ${overview.data.endpoints.unhealthy} unhealthy · ${overview.data.endpoints.unknown} unknown`}
            hint={overview.data.endpoints.observedAt ? `last observed ${ago(overview.data.endpoints.observedAt)}` : "no attempt observed yet"}
            small
          />
          <p className="text-xs text-muted-foreground sm:col-span-2 lg:col-span-3" data-testid="computed-at">
            Read {ago(overview.data.computedAt)}; the numbers refresh every 15 seconds.
          </p>
        </section>
      )}
      {apps.data && apps.data.length === 0 && envId !== null && <GettingStarted environmentId={envId} />}
      {apps.data && apps.data.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Getting started</CardTitle>
            <CardDescription>Register an endpoint, create a key, send a test delivery and read its attempt.</CardDescription>
          </CardHeader>
          <CardContent>
            <GettingStarted environmentId={envId!} />
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function Tile({ title, value, hint, small = false }: { title: string; value: number | string; hint: string; small?: boolean }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription>{title}</CardDescription>
        <CardTitle className={small ? "text-base" : "text-3xl"}>{value}</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-xs text-muted-foreground">{hint}</p>
      </CardContent>
    </Card>
  );
}
