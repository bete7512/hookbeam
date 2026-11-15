"use client";

import { KeyRound, TriangleAlert } from "lucide-react";

import { CopyButton } from "@/components/ui/copy-button";

// SecretOnce shows a value the server will never show again: the secret in full, a copy button, and the warning.
export function SecretOnce({ label, value, hint = "Hookbeam keeps only a fingerprint of it." }: { label: string; value: string; hint?: string }) {
  return (
    <div role="status" className="overflow-hidden rounded-lg border border-beam-from/40 bg-beam-from/5" data-testid="secret-once">
      <div className="flex items-center gap-2 border-b border-beam-from/20 px-4 py-2.5">
        <KeyRound className="h-4 w-4 text-beam-from" aria-hidden />
        <p className="text-sm font-medium">{label}</p>
      </div>
      <div className="flex flex-col gap-3 p-4">
        <div className="flex items-start gap-2">
          <code className="min-w-0 flex-1 break-all rounded-md border bg-background px-3 py-2 font-mono text-xs leading-5" data-testid="secret-value">
            {value}
          </code>
          <CopyButton value={value} label="Copy" showLabel className="h-9" />
        </div>
        <p className="flex items-start gap-2 text-xs text-muted-foreground">
          <TriangleAlert className="mt-px h-3.5 w-3.5 shrink-0 text-amber-500" aria-hidden />
          <span>Copy it now: this is the only time it is shown. {hint}</span>
        </p>
      </div>
    </div>
  );
}
