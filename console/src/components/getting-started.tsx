"use client";

import Link from "next/link";

import { useApiKeys, useApplications, useEndpoints } from "@/api/queries";
import { Button } from "@/components/ui/button";

// GettingStarted walks a new environment to its first delivery: each step reads the live state and links to where
// it happens, so nothing is duplicated from the real screens.
export function GettingStarted({ environmentId }: { environmentId: number }) {
  const apps = useApplications(environmentId);
  const first = apps.data?.[0] ?? null;
  const endpoints = useEndpoints(first?.id ?? 0);
  const keys = useApiKeys();
  const hasEndpoint = (endpoints.data?.length ?? 0) > 0;
  const hasKey = (keys.data ?? []).some((k) => k.applicationId === first?.id && !k.revokedAt);
  const steps = [
    { done: first !== null, title: "Create an application", hint: "One per recipient; it owns endpoints and keys.", href: "/applications" },
    { done: hasEndpoint, title: "Register an endpoint", hint: "The URL that receives signed POSTs; copy its secret once.", href: first ? `/applications/${first.id}` : "/applications" },
    { done: hasKey, title: "Create a key bound to the application", hint: "Your backend submits events with it; the curl example is ready to paste.", href: first ? `/applications/${first.id}` : "/applications" },
    { done: false, title: "Send a test delivery and read the first attempt", hint: "From the endpoint page: one click plans a hookbeam.test message.", href: first && endpoints.data?.[0] ? `/endpoints/${endpoints.data[0].id}` : "/applications" },
  ];
  return (
    <ol className="flex flex-col gap-3" data-testid="getting-started">
      {steps.map((step, i) => (
        <li key={step.title} className="flex items-start gap-3 rounded-md border p-3">
          <span aria-hidden className={`mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${step.done ? "bg-emerald-600 text-white" : "bg-secondary"}`}>
            {step.done ? "✓" : i + 1}
          </span>
          <div className="flex-1">
            <p className="font-medium">
              {step.title}
              <span className="sr-only">{step.done ? " (done)" : ""}</span>
            </p>
            <p className="text-sm text-muted-foreground">{step.hint}</p>
          </div>
          <Button variant={step.done ? "ghost" : "outline"} size="sm" asChild>
            <Link href={step.href}>{step.done ? "Open" : "Go"}</Link>
          </Button>
        </li>
      ))}
    </ol>
  );
}
