import Link from "next/link";

import { cn } from "@/lib/utils";

// Logo is the Hookbeam mark: a hook catching a beam of light, on the accent gradient.
export function Logo({ className, size = 28 }: { className?: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" className={cn("shrink-0", className)} aria-hidden focusable="false">
      <defs>
        <linearGradient id="hb-beam" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#7c6cff" />
          <stop offset="1" stopColor="#22d3ee" />
        </linearGradient>
      </defs>
      <rect width="32" height="32" rx="8" fill="url(#hb-beam)" />
      <path d="M11 8v10a5.5 5.5 0 0 0 11 0v-2" fill="none" stroke="#fff" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M18 7l6 6" stroke="#fff" strokeWidth="3" strokeLinecap="round" />
      <path d="M24 7l-6 6" stroke="#fff" strokeOpacity="0.55" strokeWidth="2" strokeLinecap="round" />
      <circle cx="24" cy="13" r="2.2" fill="#fff" />
    </svg>
  );
}

// Brand is the mark next to the wordmark, linking home.
export function Brand({ href = "/", className, wordmark = true, size = 28 }: { href?: string; className?: string; wordmark?: boolean; size?: number }) {
  return (
    <Link href={href} className={cn("inline-flex items-center gap-2.5 font-semibold tracking-tight", className)} aria-label="Hookbeam">
      <Logo size={size} className="drop-shadow-[0_0_12px_rgba(124,108,255,0.45)]" />
      {wordmark && <span className="text-[15px]">Hookbeam</span>}
    </Link>
  );
}
