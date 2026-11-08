import * as React from "react";

import { cn } from "@/lib/utils";

const tones: Record<string, string> = {
  neutral: "bg-secondary text-secondary-foreground",
  good: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  warn: "bg-amber-500/15 text-amber-700 dark:text-amber-300",
  bad: "bg-destructive/15 text-destructive",
  info: "bg-beam-from/15 text-accent-foreground",
};

const dots: Record<string, string> = {
  neutral: "bg-muted-foreground",
  good: "bg-emerald-500",
  warn: "bg-amber-500",
  bad: "bg-destructive",
  info: "bg-beam-from",
};

// Badge names a state in words; the tone only reinforces it. `dot` adds a status dot, `pulse` makes it breathe
// for something still in motion.
export function Badge({ tone = "neutral", dot = false, pulse = false, className, children, ...props }: React.ComponentProps<"span"> & { tone?: keyof typeof tones; dot?: boolean; pulse?: boolean }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 rounded-md px-2 py-0.5 text-xs font-medium", tones[tone], className)} {...props}>
      {dot && (
        <span className="relative inline-flex h-1.5 w-1.5" aria-hidden>
          {pulse && <span className={cn("absolute inline-flex h-full w-full animate-ping rounded-full opacity-75", dots[tone])} />}
          <span className={cn("relative inline-flex h-1.5 w-1.5 rounded-full", dots[tone])} />
        </span>
      )}
      {children}
    </span>
  );
}
