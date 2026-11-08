import * as React from "react";

import { cn } from "@/lib/utils";

const tones: Record<string, string> = {
  neutral: "bg-secondary text-secondary-foreground",
  good: "bg-emerald-100 text-emerald-900 dark:bg-emerald-900/40 dark:text-emerald-100",
  warn: "bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-100",
  bad: "bg-destructive text-white",
};

// Badge names a state in words; the tone only reinforces it.
export function Badge({ tone = "neutral", className, ...props }: React.ComponentProps<"span"> & { tone?: keyof typeof tones }) {
  return <span className={cn("inline-flex items-center rounded px-2 py-0.5 text-xs font-medium", tones[tone], className)} {...props} />;
}
