import Link from "next/link";
import type { ReactNode } from "react";

import { asApiError } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

// The shared screen states. Every page shows one of these instead of an ad-hoc message, so a delivery-service
// outage reads as "unavailable", never as an empty list, and the texts carry their meaning without colour.

export function LoadingState({ label = "Loading…" }: { label?: string }) {
  return (
    <div role="status" aria-live="polite" className="flex flex-col gap-2">
      <Skeleton className="h-4 w-2/3" />
      <Skeleton className="h-4 w-1/2" />
      <span className="text-xs text-muted-foreground">{label}</span>
    </div>
  );
}

export function EmptyState({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed p-8 text-center">
      <svg viewBox="0 0 64 24" className="mx-auto mb-3 h-6 w-16 text-beam-from" aria-hidden>
        <path d="M4 12h16m8 0h16m8 0h8" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeDasharray="2 4" />
        <circle cx="22" cy="12" r="3" fill="currentColor" />
        <circle cx="46" cy="12" r="3" fill="currentColor" opacity="0.5" />
      </svg>
      <p className="font-medium">{title}</p>
      {hint && <p className="mt-1 text-sm text-muted-foreground">{hint}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function UnavailableState({ onRetry }: { onRetry?: () => void }) {
  return (
    <div role="alert" className="rounded-lg border p-6">
      <p className="font-medium">Service unavailable</p>
      <p className="mt-1 text-sm text-muted-foreground">Hookbeam could not reach a part of itself. Nothing was lost; try again in a moment.</p>
      {onRetry && (
        <Button className="mt-4" variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  );
}

export function ForbiddenState({ message }: { message?: string }) {
  return (
    <div role="alert" className="rounded-lg border p-6">
      <p className="font-medium">You do not have permission</p>
      <p className="mt-1 text-sm text-muted-foreground">{message ?? "Your role in this workspace does not allow this."}</p>
    </div>
  );
}

export function ErrorState({ message, onRetry }: { message?: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="rounded-lg border p-6">
      <p className="font-medium">Something went wrong</p>
      <p className="mt-1 text-sm text-muted-foreground">{message ?? "The request failed."}</p>
      {onRetry && (
        <Button className="mt-4" variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  );
}

export function SignedOutState() {
  return (
    <div role="alert" className="rounded-lg border p-6">
      <p className="font-medium">Your session ended</p>
      <p className="mt-1 text-sm text-muted-foreground">Sign in again to continue.</p>
      <Button className="mt-4" size="sm" asChild>
        <Link href="/login">Sign in</Link>
      </Button>
    </div>
  );
}

// QueryState renders the right state for a query that failed; the caller renders its data otherwise.
export function QueryState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const err = asApiError(error);
  if (err.unavailable) return <UnavailableState onRetry={onRetry} />;
  if (err.unauthorized) return <SignedOutState />;
  if (err.forbidden) return <ForbiddenState message={err.message} />;
  return <ErrorState message={err.message} onRetry={onRetry} />;
}
