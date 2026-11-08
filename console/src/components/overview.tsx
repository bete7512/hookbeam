"use client";

import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, ArrowRight, CheckCircle2, Clock, HeartPulse, Inbox, RotateCcw } from "lucide-react";
import Link from "next/link";

import { api } from "@/api/client";
import { unwrap, useApplications, useOverview } from "@/api/queries";
import type { components } from "@/api/schema";
import { GettingStarted } from "@/components/getting-started";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, LoadingState, QueryState } from "@/components/states";
import { useEnvironment } from "@/lib/environment";
import { ago, when } from "@/lib/format";

type Bucket = components["schemas"]["OverviewBucket"];

// Overview is the environment's dashboard: the last 24 hours as numbers and as a chart, what needs attention, the
// newest events, and the getting-started flow until the environment has an application.
export function Overview() {
  const { current } = useEnvironment();
  const envId = current?.id ?? null;
  const overview = useOverview(envId);
  const apps = useApplications(envId);
  const recent = useQuery({
    queryKey: ["messages", envId, "recent"],
    queryFn: () => unwrap(api.GET("/v1/environments/{environmentId}/messages", { params: { path: { environmentId: envId ?? 0 }, query: { limit: 6, sort_by: "acceptedAt", sort_dir: "desc" } } })),
    enabled: envId !== null,
    refetchInterval: 15_000,
  });
  const o = overview.data;
  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{current ? current.name : "Environment"}</p>
          <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
          <p className="text-muted-foreground">Last 24 hours. {o ? `Read ${ago(o.computedAt)}; refreshes every 15 seconds.` : ""}</p>
        </div>
        <Link href="/events" className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          All events <ArrowRight className="h-3.5 w-3.5" aria-hidden />
        </Link>
      </header>
      {envId === null && <LoadingState label="Waiting for the environment…" />}
      {overview.isPending && envId !== null && <TilesSkeleton />}
      {overview.isError && <QueryState error={overview.error} onRetry={() => overview.refetch()} />}
      {o && (
        <>
          <section aria-label="Tiles" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Tile icon={Inbox} title="Accepted" value={o.accepted} hint="messages in the window" series={o.series.map((b) => b.accepted)} />
            <Tile icon={CheckCircle2} title="Succeeded" value={o.succeeded} hint="deliveries in the window" series={o.series.map((b) => b.succeeded)} tone="good" />
            <Tile icon={RotateCcw} title="Retry backlog" value={o.backlog} hint="queued and scheduled right now" />
            <Tile icon={AlertTriangle} title="Exhausted" value={o.exhausted} hint="gave up after the last retry" series={o.series.map((b) => b.failed)} tone="bad" />
          </section>
          <section className="grid gap-4 lg:grid-cols-[2fr_1fr]">
            <Card>
              <CardHeader className="flex flex-row items-start justify-between space-y-0">
                <div>
                  <CardTitle>Deliveries per hour</CardTitle>
                  <CardDescription>Succeeded and failed deliveries, and the events accepted, hour by hour.</CardDescription>
                </div>
                <ul className="flex gap-3 text-xs text-muted-foreground">
                  <li className="inline-flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-sm bg-emerald-500" aria-hidden /> succeeded
                  </li>
                  <li className="inline-flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-sm bg-destructive" aria-hidden /> failed
                  </li>
                  <li className="inline-flex items-center gap-1.5">
                    <span className="h-0.5 w-3 rounded bg-beam-from" aria-hidden /> accepted
                  </li>
                </ul>
              </CardHeader>
              <CardContent>
                <Chart series={o.series} />
              </CardContent>
            </Card>
            <div className="flex flex-col gap-4">
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <HeartPulse className="h-4 w-4 text-beam-from" aria-hidden />
                    Needs attention
                  </CardTitle>
                </CardHeader>
                <CardContent className="flex flex-col gap-3 text-sm" data-testid="tile-endpoint-health">
                  <AttentionRow label="Unhealthy endpoints" value={o.endpoints.unhealthy} tone={o.endpoints.unhealthy > 0 ? "bad" : "good"} href="/applications" />
                  <AttentionRow label="Healthy endpoints" value={o.endpoints.healthy} tone="good" href="/applications" suffix="healthy" />
                  <AttentionRow label="Unobserved endpoints" value={o.endpoints.unknown} tone="neutral" href="/applications" suffix="unknown" />
                  <AttentionRow label="Oldest pending" value={o.oldestPendingDueAt ? ago(o.oldestPendingDueAt) : "none"} tone={o.oldestPendingDueAt ? "warn" : "neutral"} href="/events?outcome=retry_scheduled" icon={Clock} />
                  <p className="text-xs text-muted-foreground">{o.endpoints.observedAt ? `Health observed ${ago(o.endpoints.observedAt)}.` : "No attempt observed yet."}</p>
                </CardContent>
              </Card>
            </div>
          </section>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0">
              <div>
                <CardTitle>Recent events</CardTitle>
                <CardDescription>The newest accepted messages and where their deliveries stand.</CardDescription>
              </div>
              <Link href="/events" className="text-sm text-muted-foreground hover:text-foreground">
                Explorer
              </Link>
            </CardHeader>
            <CardContent>
              {recent.isPending && <LoadingState />}
              {recent.isError && <QueryState error={recent.error} onRetry={() => recent.refetch()} />}
              {recent.data && recent.data.items.length === 0 && <EmptyState title="Nothing accepted yet" hint="Send a test delivery from an endpoint, or submit an event with a key." />}
              {recent.data && recent.data.items.length > 0 && (
                <table>
                  <thead>
                    <tr>
                      <th className="py-2 pr-4">Type</th>
                      <th className="py-2 pr-4">Webhook id</th>
                      <th className="py-2 pr-4">Accepted</th>
                      <th className="py-2 pr-4">Deliveries</th>
                    </tr>
                  </thead>
                  <tbody>
                    {recent.data.items.map((m) => (
                      <tr key={m.message.id} className="border-t">
                        <td className="py-2 pr-4">
                          <Link href={`/events/${m.message.id}`} className="font-medium hover:underline">
                            {m.message.type}
                          </Link>
                        </td>
                        <td className="py-2 pr-4 font-mono text-xs text-muted-foreground">{m.message.webhookId}</td>
                        <td className="py-2 pr-4 text-muted-foreground" title={when(m.message.acceptedAt)}>
                          {ago(m.message.acceptedAt)}
                        </td>
                        <td className="py-2 pr-4">
                          <span className="flex flex-wrap gap-1">
                            {m.deliveryCounts.succeeded > 0 && (
                              <Badge tone="good" dot>
                                {m.deliveryCounts.succeeded} succeeded
                              </Badge>
                            )}
                            {m.deliveryCounts.failed > 0 && (
                              <Badge tone="bad" dot>
                                {m.deliveryCounts.failed} failed
                              </Badge>
                            )}
                            {m.deliveryCounts.retryScheduled + m.deliveryCounts.queued + m.deliveryCounts.inFlight > 0 && (
                              <Badge tone="warn" dot pulse>
                                {m.deliveryCounts.retryScheduled + m.deliveryCounts.queued + m.deliveryCounts.inFlight} pending
                              </Badge>
                            )}
                            {m.deliveryCounts.cancelled > 0 && <Badge dot>{m.deliveryCounts.cancelled} cancelled</Badge>}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </CardContent>
          </Card>
          <p className="sr-only" data-testid="computed-at">
            Read {ago(o.computedAt)}
          </p>
        </>
      )}
      {apps.data && apps.data.length === 0 && envId !== null && (
        <Card className="glow">
          <CardHeader>
            <CardTitle>Getting started</CardTitle>
            <CardDescription>Four steps to the first delivery; each links to where it happens.</CardDescription>
          </CardHeader>
          <CardContent>
            <GettingStarted environmentId={envId} />
          </CardContent>
        </Card>
      )}
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

function TilesSkeleton() {
  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4" role="status" aria-label="Reading the tiles">
      {[0, 1, 2, 3].map((i) => (
        <Skeleton key={i} className="h-32" />
      ))}
    </div>
  );
}

function Tile({ icon: Icon, title, value, hint, series, tone = "neutral" }: { icon: React.ComponentType<{ className?: string }>; title: string; value: number | string; hint: string; series?: number[]; tone?: "neutral" | "good" | "bad" }) {
  const color = tone === "good" ? "bg-emerald-500" : tone === "bad" ? "bg-destructive" : "beam-bg";
  const max = Math.max(1, ...(series ?? [0]));
  return (
    <Card data-testid={`tile-${title.toLowerCase().replace(/\s+/g, "-")}`} className="relative overflow-hidden">
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardDescription>{title}</CardDescription>
          <Icon className="h-4 w-4 text-muted-foreground" aria-hidden />
        </div>
        <CardTitle className="text-3xl tabular-nums">{value}</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-xs text-muted-foreground">{hint}</p>
        {series && series.length > 1 && (
          <div className="mt-3 flex h-6 items-end gap-px" aria-hidden>
            {series.map((v, i) => (
              <span key={i} className={`flex-1 rounded-sm ${color} opacity-70`} style={{ height: `${Math.max(8, (v / max) * 100)}%` }} />
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function AttentionRow({ label, value, tone, href, suffix, icon: Icon }: { label: string; value: number | string; tone: "good" | "bad" | "warn" | "neutral"; href: string; suffix?: string; icon?: React.ComponentType<{ className?: string }> }) {
  return (
    <Link href={href} className="flex items-center justify-between rounded-md border border-border/70 px-3 py-2 transition-colors hover:border-beam-from/50">
      <span className="inline-flex items-center gap-2 text-muted-foreground">
        {Icon && <Icon className="h-3.5 w-3.5" aria-hidden />}
        {label}
      </span>
      <Badge tone={tone} dot>
        {value}
        {suffix ? ` ${suffix}` : ""}
      </Badge>
    </Link>
  );
}

// Chart draws the 24 hourly buckets as stacked bars (succeeded over failed) with the accepted count as a line.
function Chart({ series }: { series: Bucket[] }) {
  const w = 720;
  const h = 180;
  const pad = { top: 10, right: 8, bottom: 22, left: 28 };
  const innerW = w - pad.left - pad.right;
  const innerH = h - pad.top - pad.bottom;
  const n = Math.max(series.length, 1);
  const max = Math.max(1, ...series.map((b) => Math.max(b.succeeded + b.failed, b.accepted)));
  const slot = innerW / n;
  const bar = Math.max(2, slot * 0.6);
  const y = (v: number) => pad.top + innerH - (v / max) * innerH;
  const line = series.map((b, i) => `${i === 0 ? "M" : "L"}${(pad.left + slot * i + slot / 2).toFixed(1)},${y(b.accepted).toFixed(1)}`).join(" ");
  const empty = series.every((b) => b.accepted + b.succeeded + b.failed === 0);
  return (
    <div className="relative">
      <svg viewBox={`0 0 ${w} ${h}`} className="h-auto w-full" role="img" aria-label="Deliveries per hour over the last 24 hours">
        {[0, 0.5, 1].map((f) => (
          <g key={f}>
            <line x1={pad.left} x2={w - pad.right} y1={y(max * f)} y2={y(max * f)} stroke="currentColor" strokeOpacity="0.08" />
            <text x={pad.left - 6} y={y(max * f) + 3} textAnchor="end" fontSize="9" fill="currentColor" fillOpacity="0.5">
              {Math.round(max * f)}
            </text>
          </g>
        ))}
        {series.map((b, i) => {
          const x = pad.left + slot * i + (slot - bar) / 2;
          const okH = (b.succeeded / max) * innerH;
          const badH = (b.failed / max) * innerH;
          return (
            <g key={b.hour}>
              <title>{`${new Date(b.hour).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}: ${b.accepted} accepted, ${b.succeeded} succeeded, ${b.failed} failed`}</title>
              <rect x={x} y={pad.top + innerH - okH - badH} width={bar} height={badH} fill="var(--destructive)" rx="1" />
              <rect x={x} y={pad.top + innerH - okH} width={bar} height={okH} fill="#10b981" rx="1" />
              {(i % 4 === 0 || i === series.length - 1) && (
                <text x={x + bar / 2} y={h - 6} textAnchor="middle" fontSize="9" fill="currentColor" fillOpacity="0.5">
                  {new Date(b.hour).toLocaleTimeString([], { hour: "2-digit" })}
                </text>
              )}
            </g>
          );
        })}
        {series.length > 1 && <path d={line} fill="none" stroke="var(--beam-from)" strokeWidth="1.5" strokeLinejoin="round" />}
      </svg>
      {empty && <p className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-muted-foreground">No deliveries in the last 24 hours yet.</p>}
    </div>
  );
}
