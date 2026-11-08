"use client";

import { Activity, ArrowRight, BookOpen, HeartPulse, KeyRound, RotateCcw, ScrollText, ShieldCheck } from "lucide-react";
import Link from "next/link";

import { useMe } from "@/api/queries";
import { Brand } from "@/components/brand";
import { Button } from "@/components/ui/button";

const repo = "https://github.com/bete7512/hookbeam";

// Landing is the public front page: what Hookbeam does, in the order a developer evaluates it, and the way in.
export function Landing() {
  const me = useMe();
  const signedIn = !!me.data;
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-20 border-b bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4 sm:px-6">
          <Brand />
          <nav aria-label="Site" className="ml-auto flex items-center gap-1 text-sm">
            <a href="/docs" className="rounded-md px-3 py-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground">
              Docs
            </a>
            <a href={repo} className="hidden rounded-md px-3 py-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground sm:inline-block" rel="noreferrer">
              GitHub
            </a>
            {signedIn ? (
              <Button size="sm" asChild>
                <Link href="/overview">Open console</Link>
              </Button>
            ) : (
              <>
                <Button variant="ghost" size="sm" asChild>
                  <Link href="/login">Sign in</Link>
                </Button>
                <Button size="sm" asChild>
                  <Link href="/signup">Get started</Link>
                </Button>
              </>
            )}
          </nav>
        </div>
      </header>

      <main>
        <section className="mx-auto grid max-w-6xl gap-12 px-4 pb-20 pt-16 sm:px-6 lg:grid-cols-[1.1fr_1fr] lg:items-center lg:pt-24">
          <div className="flex flex-col gap-6">
            <p className="inline-flex w-fit items-center gap-2 rounded-full border bg-card px-3 py-1 text-xs font-medium text-muted-foreground">
              <span className="h-1.5 w-1.5 rounded-full bg-primary" aria-hidden />
              Open-source webhook delivery
            </p>
            <h1 className="text-4xl font-semibold tracking-tight sm:text-5xl lg:text-6xl">Webhooks your customers can trust.</h1>
            <p className="max-w-xl text-lg text-muted-foreground">
              Hookbeam accepts each event once, signs it, delivers it with retries that back off, and shows every attempt. Replay what failed, cancel what should not go out, rotate secrets without downtime, and keep an audit of all of it.
            </p>
            <div className="flex flex-wrap items-center gap-3">
              <Button size="lg" asChild>
                <Link href={signedIn ? "/overview" : "/signup"}>
                  {signedIn ? "Open console" : "Create a free workspace"}
                  <ArrowRight className="h-4 w-4" aria-hidden />
                </Link>
              </Button>
              <Button size="lg" variant="outline" asChild>
                <a href="/docs">
                  <BookOpen className="h-4 w-4" aria-hidden />
                  Read the API docs
                </a>
              </Button>
            </div>
            <p className="text-sm text-muted-foreground">Runs on your machine with one docker compose up. Standard Webhooks signatures, so any receiver library verifies them.</p>
          </div>
          <div className="flex flex-col gap-4">
            <div className="overflow-hidden rounded-xl border bg-card shadow-sm">
              <div className="flex items-center gap-2 border-b bg-secondary/60 px-4 py-2 text-xs text-muted-foreground">
                <span className="h-2.5 w-2.5 rounded-full bg-border" aria-hidden />
                <span className="h-2.5 w-2.5 rounded-full bg-border" aria-hidden />
                <span className="h-2.5 w-2.5 rounded-full bg-border" aria-hidden />
                <span className="ml-2 font-mono">submit an event</span>
              </div>
              <pre className="overflow-x-auto p-4 font-mono text-xs leading-relaxed">
                <code>{`curl -X POST https://hooks.example.com/v1/applications/42/messages \\
  -H "Authorization: Bearer hb_..." \\
  -H "Idempotency-Key: order-817-shipped-v1" \\
  -d '{"type":"order.shipped","external_id":"order-817",
       "data":{"order_id":"817"}}'`}</code>
              </pre>
            </div>
            <div className="rounded-xl border bg-card p-4 shadow-sm">
              <p className="mb-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">Delivery 1024 · order.shipped</p>
              <ol className="flex flex-col gap-2 text-sm">
                <Attempt n={1} outcome="HTTP 503" tone="bad" detail="retry in 15 s" />
                <Attempt n={2} outcome="HTTP 204" tone="good" detail="succeeded · 42 ms · revision 3 · secret v2" />
              </ol>
            </div>
          </div>
        </section>

        <section className="border-y bg-secondary/40">
          <div className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
            <h2 className="text-2xl font-semibold tracking-tight">Everything between your event and their endpoint</h2>
            <p className="mt-2 max-w-2xl text-muted-foreground">The parts of webhook delivery that are tedious to get right, built once and visible in the console.</p>
            <div className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <Feature icon={ShieldCheck} title="Signed, verifiable deliveries" text="Every POST carries Standard Webhooks headers signed with the endpoint's secret. Rotate with an overlap; both signatures travel until the old one retires." />
              <Feature icon={RotateCcw} title="Retries that back off" text="Transient failures retry on a schedule from 15 seconds to 12 hours with jitter, honour Retry-After, and stop at the deadline. 4xx fails fast." />
              <Feature icon={Activity} title="Every attempt on record" text="Status or error class, latency, the endpoint revision and secret version used, response headers and a bounded preview. Replay or cancel from the same screen." />
              <Feature icon={HeartPulse} title="Endpoint health" text="Three consecutive failures open an episode and notify owners and admins in the console and by email; the next success closes it." />
              <Feature icon={KeyRound} title="Keys with scopes" text="Keys submit, read, or replay, and can be bound to one application. Roles from owner to viewer decide who sees payloads and who manages the team." />
              <Feature icon={ScrollText} title="Audit on both planes" text="Key, member, endpoint, delivery and payload-read actions from the control plane and the data plane, merged and filterable." />
            </div>
          </div>
        </section>

        <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
          <h2 className="text-2xl font-semibold tracking-tight">Three steps to the first delivery</h2>
          <ol className="mt-8 grid gap-6 md:grid-cols-3">
            <Step n={1} title="Register an endpoint" text="Create an application for a recipient and add the URL that receives signed POSTs. The signing secret is shown once." />
            <Step n={2} title="Submit events with a key" text="Your backend POSTs JSON with an Idempotency-Key. The same key with the same body is accepted once; a different body is a conflict." />
            <Step n={3} title="Watch it land" text="The explorer lists every event; a delivery shows its attempts as they happen, and a test delivery needs no key at all." />
          </ol>
          <div className="mt-10 flex flex-wrap items-center gap-3 rounded-xl border bg-card p-6">
            <div className="mr-auto">
              <p className="font-medium">Ready when you are</p>
              <p className="text-sm text-muted-foreground">A workspace is created with your account; invite your team from the console.</p>
            </div>
            <Button asChild>
              <Link href={signedIn ? "/overview" : "/signup"}>{signedIn ? "Open console" : "Create a workspace"}</Link>
            </Button>
            <Button variant="outline" asChild>
              <a href={repo} rel="noreferrer">
                View the source
              </a>
            </Button>
          </div>
        </section>
      </main>

      <footer className="border-t">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-8 text-sm text-muted-foreground sm:px-6">
          <Brand className="text-foreground" />
          <a href="/docs" className="hover:text-foreground">
            API docs
          </a>
          <a href={repo} className="hover:text-foreground" rel="noreferrer">
            GitHub
          </a>
          <a href={`${repo}/blob/main/docs/quick-start.md`} className="hover:text-foreground" rel="noreferrer">
            Quick start
          </a>
          <span className="ml-auto">Open source, Apache-style licence to follow.</span>
        </div>
      </footer>
    </div>
  );
}

function Attempt({ n, outcome, tone, detail }: { n: number; outcome: string; tone: "good" | "bad"; detail: string }) {
  return (
    <li className="flex items-center gap-3 rounded-md border px-3 py-2">
      <span className="font-mono text-xs text-muted-foreground">#{n}</span>
      <span className={tone === "good" ? "rounded px-2 py-0.5 text-xs font-medium bg-emerald-100 text-emerald-900" : "rounded px-2 py-0.5 text-xs font-medium bg-destructive text-white"}>{outcome}</span>
      <span className="text-muted-foreground">{detail}</span>
    </li>
  );
}

function Feature({ icon: Icon, title, text }: { icon: React.ComponentType<{ className?: string }>; title: string; text: string }) {
  return (
    <div className="rounded-xl border bg-card p-5 shadow-sm">
      <span className="inline-flex h-9 w-9 items-center justify-center rounded-md bg-accent text-accent-foreground">
        <Icon className="h-4.5 w-4.5" aria-hidden />
      </span>
      <h3 className="mt-4 font-semibold">{title}</h3>
      <p className="mt-1.5 text-sm text-muted-foreground">{text}</p>
    </div>
  );
}

function Step({ n, title, text }: { n: number; title: string; text: string }) {
  return (
    <li className="flex gap-4">
      <span className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary text-sm font-semibold text-primary-foreground">{n}</span>
      <div>
        <h3 className="font-semibold">{title}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{text}</p>
      </div>
    </li>
  );
}
