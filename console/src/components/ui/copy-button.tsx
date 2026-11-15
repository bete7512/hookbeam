"use client";

import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";

// CopyButton copies a value and says so for a moment; `label` names what is copied for assistive technology.
export function CopyButton({ value, label = "Copy", showLabel = false, className }: { value: string; label?: string; showLabel?: boolean; className?: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);
  return (
    <button
      type="button"
      aria-label={showLabel ? undefined : copied ? "Copied" : label}
      onClick={() =>
        navigator.clipboard
          .writeText(value)
          .then(() => setCopied(true))
          .catch(() => setCopied(false))
      }
      className={cn(
        "inline-flex h-8 shrink-0 items-center justify-center gap-1.5 rounded-md border border-input bg-background px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        className,
      )}
    >
      {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" aria-hidden /> : <Copy className="h-3.5 w-3.5" aria-hidden />}
      {showLabel && <span aria-live="polite">{copied ? "Copied" : label}</span>}
    </button>
  );
}
