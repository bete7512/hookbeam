import { Webhook } from "lucide-react";
import Link from "next/link";

import { cn } from "@/lib/utils";

// Brand is the product mark: a webhook glyph on the accent, next to the wordmark.
export function Brand({ href = "/", className, wordmark = true }: { href?: string; className?: string; wordmark?: boolean }) {
  return (
    <Link href={href} className={cn("inline-flex items-center gap-2 font-semibold tracking-tight", className)} aria-label="Hookbeam">
      <span className="inline-flex h-7 w-7 items-center justify-center rounded-md bg-primary text-primary-foreground shadow-sm">
        <Webhook className="h-4 w-4" aria-hidden />
      </span>
      {wordmark && <span>Hookbeam</span>}
    </Link>
  );
}
