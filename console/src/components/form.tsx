import * as React from "react";

import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

// Field is a labelled input with an optional error announced to assistive technology.
export function Field({ id, label, error, hint, className, ...props }: React.ComponentProps<"input"> & { id: string; label: string; error?: string; hint?: string }) {
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;
  const describedBy = [error ? errorId : null, hint ? hintId : null].filter(Boolean).join(" ") || undefined;
  return (
    <div className={cn("flex flex-col gap-1", className)}>
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} aria-invalid={error ? true : undefined} aria-describedby={describedBy} {...props} />
      {hint && !error && (
        <p id={hintId} className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

// FormError is the form-level failure line, announced when it appears.
export function FormError({ message }: { message?: string | null }) {
  if (!message) return null;
  return (
    <p role="alert" className="text-sm text-destructive">
      {message}
    </p>
  );
}

// FormNotice is a form-level success line.
export function FormNotice({ message }: { message?: string | null }) {
  if (!message) return null;
  return (
    <p role="status" className="text-sm">
      {message}
    </p>
  );
}
