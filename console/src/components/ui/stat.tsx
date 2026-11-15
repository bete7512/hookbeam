import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

const tones = {
  neutral: "text-foreground",
  good: "text-emerald-600 dark:text-emerald-400",
  warn: "text-amber-600 dark:text-amber-400",
  bad: "text-destructive",
};

// Stat is one number of a detail page's summary strip, with what it counts underneath.
export function Stat({
  label,
  value,
  hint,
  icon: Icon,
  tone = "neutral",
  ...props
}: {
  label: string;
  value: ReactNode;
  hint?: ReactNode;
  icon?: React.ComponentType<{ className?: string }>;
  tone?: keyof typeof tones;
  "data-testid"?: string;
}) {
  return (
    <div className="rounded-lg border bg-card p-4" {...props}>
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">{label}</p>
        {Icon && <Icon className="h-4 w-4 text-muted-foreground" aria-hidden />}
      </div>
      <p className={cn("mt-2 text-2xl font-semibold tabular-nums leading-none tracking-tight", tones[tone])}>{value}</p>
      {hint && <p className="mt-2 text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}
