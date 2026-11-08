"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";

// SecretOnce shows a value the server will never show again and offers to copy it.
export function SecretOnce({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div role="status" className="rounded-md border border-amber-300 bg-amber-50 p-4 text-sm dark:border-amber-700 dark:bg-amber-950/40" data-testid="secret-once">
      <p className="font-medium">{label}</p>
      <p className="mt-1 text-muted-foreground">Copy it now; Hookbeam keeps only a fingerprint.</p>
      <div className="mt-2 flex items-center gap-2">
        <code className="break-all rounded bg-background px-2 py-1 font-mono text-xs" data-testid="secret-value">
          {value}
        </code>
        <Button
          variant="outline"
          size="sm"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(value);
              setCopied(true);
            } catch {
              setCopied(false);
            }
          }}
        >
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
    </div>
  );
}
