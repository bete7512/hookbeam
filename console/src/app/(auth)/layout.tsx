import { Activity, RotateCcw, ShieldCheck } from "lucide-react";

import { Brand } from "@/components/brand";
import { ThemeToggle } from "@/components/theme-toggle";

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-screen lg:grid-cols-[1.1fr_1fr]">
      <aside className="relative hidden flex-col justify-between overflow-hidden bg-sidebar p-10 text-sidebar-foreground lg:flex" aria-label="About Hookbeam">
        <div className="hero-backdrop pointer-events-none absolute inset-0" aria-hidden />
        <Brand className="relative" />
        <div className="relative max-w-md">
          <h2 className="text-3xl font-semibold tracking-tight">
            Webhook delivery you can <span className="beam-text">see</span>.
          </h2>
          <ul className="mt-8 flex flex-col gap-5 text-sm">
            <li className="flex gap-3">
              <ShieldCheck className="mt-0.5 h-5 w-5 shrink-0 text-beam-to" aria-hidden />
              <span>Every delivery is signed with Standard Webhooks headers; secrets rotate with an overlap.</span>
            </li>
            <li className="flex gap-3">
              <RotateCcw className="mt-0.5 h-5 w-5 shrink-0 text-beam-to" aria-hidden />
              <span>Transient failures retry with backoff and Retry-After; what fails for good can be replayed.</span>
            </li>
            <li className="flex gap-3">
              <Activity className="mt-0.5 h-5 w-5 shrink-0 text-beam-to" aria-hidden />
              <span>Every attempt is on record, with the revision and secret it used, for the whole team.</span>
            </li>
          </ul>
        </div>
        <p className="relative text-xs text-muted-foreground">Open source. One docker compose up runs the whole product.</p>
      </aside>
      <main className="relative flex flex-col justify-center px-6 py-12">
        <ThemeToggle className="absolute right-4 top-4" />
        <div className="mx-auto flex w-full max-w-sm flex-col gap-8">
          <Brand className="lg:hidden" />
          {children}
        </div>
      </main>
    </div>
  );
}
