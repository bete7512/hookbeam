import Link from "next/link";
import type { ReactNode } from "react";

import { asApiError } from "@/api/errors";
import { Button } from "@/components/ui/button";

// The shared screen states. Every page shows one of these instead of an ad-hoc message, so a delivery-service
// outage reads as "unavailable", never as an empty list, and the texts carry their meaning without colour.

export function LoadingState({ label = "Loading…" }: { label?: string }) {
  return (
    <p role="status" aria-live="polite" className="text-sm text-muted-foreground">
      {label}
    </p>
  );
}

export function EmptyState({ title, hint, action }: { title: string; hint?: string; action?: ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed p-8 text-center">
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
