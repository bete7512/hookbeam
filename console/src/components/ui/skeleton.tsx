import { cn } from "@/lib/utils";

// Skeleton is a shimmering placeholder the size of what is loading.
export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden className={cn("animate-pulse rounded-md bg-muted", className)} />;
}
