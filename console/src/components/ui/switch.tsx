"use client";

import { cn } from "@/lib/utils";

// Switch is an on/off setting that applies as soon as it is flipped.
export function Switch({ checked, onChange, disabled, label, ...props }: { checked: boolean; onChange: (next: boolean) => void; disabled?: boolean; label: string; "data-testid"?: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50",
        checked ? "bg-primary" : "bg-input",
      )}
      {...props}
    >
      <span className={cn("inline-block h-5 w-5 rounded-full bg-white shadow-sm transition-transform", checked ? "translate-x-5" : "translate-x-0.5")} aria-hidden />
    </button>
  );
}
