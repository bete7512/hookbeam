"use client";

import { Activity, ArrowRight, BookOpen, HeartPulse, KeyRound, Lock, RotateCcw, ScrollText, ShieldCheck, Terminal, Zap } from "lucide-react";
import Link from "next/link";

import { useMe } from "@/api/queries";
import { Brand } from "@/components/brand";
import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { repoURL } from "@/lib/repo";

// Landing is the public front page: what Hookbeam does, in the order a developer evaluates it, and the way in.
export function Landing() {
  const me = useMe();
  const signedIn = !!me.data;
  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-20 border-b border-border/60 bg-background/70 backdrop-blur-md">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4 sm:px-6">
          <Brand />
          <nav aria-label="Site" className="ml-auto flex items-center gap-1 text-sm">
            <ThemeToggle className="mr-1" />
            <a href="/docs" className="rounded-md px-3 py-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground">
              Docs
            </a>
            <a href={repoURL} className="hidden rounded-md px-3 py-1.5 text-muted-foreground hover:bg-secondary hover:text-foreground sm:inline-block" rel="noreferrer">
              GitHub
            </a>
            {signedIn ? (
              <Button size="sm" variant="beam" asChild>
                <Link href="/overview">Open console</Link>
              </Button>
            ) : (
              <>
                <Button variant="ghost" size="sm" asChild>
                  <Link href="/login">Sign in</Link>
                </Button>
                <Button size="sm" variant="beam" asChild>
                  <Link href="/signup">Get started</Link>
                </Button>
              </>
            )}
          </nav>
        </div>
      </header>

      <main>
        <section className="relative overflow-hidden">
          <div className="hero-backdrop pointer-events-none absolute inset-0" aria-hidden />
          <div className="relative mx-auto grid max-w-6xl gap-12 px-4 pb-20 pt-16 sm:px-6 lg:grid-cols-[1.1fr_1fr] lg:items-center lg:pt-28">
            <div className="flex flex-col gap-6">
              <p className="inline-flex w-fit items-center gap-2 rounded-full border border-border/70 bg-card/60 px-3 py-1 text-xs font-medium text-muted-foreground backdrop-blur">
                <span className="beam-bg h-1.5 w-1.5 rounded-full" aria-hidden />
                Open-source webhook delivery
              </p>
              <h1 className="text-4xl font-semibold tracking-tight sm:text-5xl lg:text-6xl">
                Webhooks your customers can <span className="beam-text">trust</span>.
              </h1>
              <p className="max-w-xl text-lg text-muted-foreground">
                Hookbeam accepts each event once, signs it, delivers it with retries that back off, and shows every attempt. Replay what failed, cancel what should not go out, rotate secrets without downtime, and keep an audit of all of it.
              </p>
              <div className="flex flex-wrap items-center gap-3">
                <Button size="lg" variant="beam" asChild>
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
              <ul className="flex flex-wrap gap-x-5 gap-y-2 text-sm text-muted-foreground">
                <li className="inline-flex items-center gap-1.5">
                  <Terminal className="h-4 w-4 text-beam-to" aria-hidden />
                  one <code className="text-foreground">docker compose up</code>
                </li>
                <li className="inline-flex items-center gap-1.5">
                  <ShieldCheck className="h-4 w-4 text-beam-to" aria-hidden />
                  Standard Webhooks signatures
                </li>
                <li className="inline-flex items-center gap-1.5">
                  <Lock className="h-4 w-4 text-beam-to" aria-hidden />
                  mTLS between planes
                </li>
              </ul>
            </div>
            <div className="flex flex-col gap-4">
              <div className="glow overflow-hidden rounded-xl border border-border/70 bg-card/90 backdrop-blur">
                <div className="flex items-center gap-2 border-b border-border/70 bg-secondary/50 px-4 py-2 text-xs text-muted-foreground">
                  <span className="h-2.5 w-2.5 rounded-full bg-[#ff5f57]" aria-hidden />
                  <span className="h-2.5 w-2.5 rounded-full bg-[#febc2e]" aria-hidden />
                  <span className="h-2.5 w-2.5 rounded-full bg-[#28c840]" aria-hidden />
                  <span className="ml-2 font-mono">submit an event</span>
                </div>
                <pre className="overflow-x-auto p-4 font-mono text-xs leading-relaxed">
                  <code>
                    <span className="text-beam-to">curl</span> -X POST https://hooks.example.com/v1/applications/42/messages \{"\n"}
                    {"  "}-H <span className="text-beam-from">&quot;Authorization: Bearer hb_...&quot;</span> \{"\n"}
                    {"  "}-H <span className="text-beam-from">&quot;Idempotency-Key: order-817-shipped-v1&quot;</span> \{"\n"}
                    {"  "}-d <span className="text-emerald-400">&apos;{"{"}&quot;type&quot;:&quot;order.shipped&quot;,&quot;external_id&quot;:&quot;order-817&quot;,{"\n"}
                    {"       "}&quot;data&quot;:{"{"}&quot;order_id&quot;:&quot;817&quot;{"}}"}&apos;</span>
                  </code>
                </pre>
              </div>
              <div className="rounded-xl border border-border/70 bg-card/90 p-4 backdrop-blur">
                <p className="mb-3 flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                  <Zap className="h-3.5 w-3.5 text-beam-from" aria-hidden />
                  Delivery 1024 · order.shipped
                </p>
                <ol className="flex flex-col gap-2 text-sm">
                  <Attempt n={1} outcome="HTTP 503" tone="bad" detail="retry in 15 s" />
                  <Attempt n={2} outcome="HTTP 204" tone="good" detail="succeeded · 42 ms · revision 3 · secret v2" />
                </ol>
              </div>
            </div>
          </div>
        </section>

        <section className="border-y border-border/60 bg-secondary/30">
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
          <div className="glow mt-10 flex flex-wrap items-center gap-3 rounded-xl border border-border/70 bg-card p-6">
            <div className="mr-auto">
              <p className="font-medium">Ready when you are</p>
              <p className="text-sm text-muted-foreground">A workspace is created with your account; invite your team from the console.</p>
            </div>
            <Button variant="beam" asChild>
              <Link href={signedIn ? "/overview" : "/signup"}>{signedIn ? "Open console" : "Create a workspace"}</Link>
            </Button>
            <Button variant="outline" asChild>
              <a href={repoURL} rel="noreferrer">
                View the source
              </a>
            </Button>
          </div>
        </section>
      </main>

      <footer className="border-t border-border/60">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-8 text-sm text-muted-foreground sm:px-6">
          <Brand className="text-foreground" />
          <a href="/docs" className="hover:text-foreground">
            API docs
          </a>
          <a href={repoURL} className="hover:text-foreground" rel="noreferrer">
            GitHub
          </a>
          <a href={`${repoURL}/blob/main/docs/quick-start.md`} className="hover:text-foreground" rel="noreferrer">
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
    <li className="flex items-center gap-3 rounded-md border border-border/70 px-3 py-2">
      <span className="font-mono text-xs text-muted-foreground">#{n}</span>
      <span className={tone === "good" ? "rounded px-2 py-0.5 text-xs font-medium bg-emerald-500/15 text-emerald-600 dark:text-emerald-300" : "rounded px-2 py-0.5 text-xs font-medium bg-destructive/15 text-destructive"}>{outcome}</span>
      <span className="text-muted-foreground">{detail}</span>
    </li>
  );
}

function Feature({ icon: Icon, title, text }: { icon: React.ComponentType<{ className?: string }>; title: string; text: string }) {
  return (
    <div className="group rounded-xl border border-border/70 bg-card p-5 transition-colors hover:border-beam-from/50">
      <span className="beam-bg inline-flex h-9 w-9 items-center justify-center rounded-md text-white shadow-[0_6px_20px_-8px_var(--beam-from)]">
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
      <span className="beam-bg inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-semibold text-white">{n}</span>
      <div>
        <h3 className="font-semibold">{title}</h3>
        <p className="mt-1 text-sm text-muted-foreground">{text}</p>
      </div>
    </li>
  );
}
