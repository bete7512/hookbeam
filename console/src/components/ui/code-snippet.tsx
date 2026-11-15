"use client";

import { useState } from "react";

import { CopyButton } from "@/components/ui/copy-button";
import { cn } from "@/lib/utils";

export interface Snippet {
  label: string;
  code: string;
}

// CodeSnippet shows one or more versions of an example (curl, Node) with a copy button for the one in view.
export function CodeSnippet({ snippets, ...props }: { snippets: Snippet[]; "data-testid"?: string }) {
  const [index, setIndex] = useState(0);
  const current = snippets[Math.min(index, snippets.length - 1)];
  return (
    <div className="overflow-hidden rounded-lg border bg-background">
      <div className="flex items-center justify-between gap-2 border-b bg-secondary/40 px-2 py-1.5">
        <div className="flex gap-1">
          {snippets.map((s, i) => (
            <button
              key={s.label}
              type="button"
              aria-pressed={i === index}
              onClick={() => setIndex(i)}
              className={cn(
                "rounded-md px-2 py-1 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                i === index ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {s.label}
            </button>
          ))}
        </div>
        <CopyButton value={current.code} label={`Copy the ${current.label} example`} className="h-7 border-transparent bg-transparent" />
      </div>
      <pre className="overflow-x-auto p-4 font-mono text-xs leading-5" tabIndex={0} {...props}>
        {current.code}
      </pre>
    </div>
  );
}
