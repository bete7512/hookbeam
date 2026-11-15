"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useRef, type KeyboardEvent, type ReactNode } from "react";

import { cn } from "@/lib/utils";

export interface TabItem<T extends string> {
  id: T;
  label: string;
  count?: number;
  icon?: React.ComponentType<{ className?: string }>;
}

// Tabs is the tab strip of a detail page: arrow keys move between tabs, and the selected one owns the panel below.
export function Tabs<T extends string>({ items, value, onChange, label }: { items: TabItem<T>[]; value: T; onChange: (next: T) => void; label: string }) {
  const refs = useRef<Record<string, HTMLButtonElement | null>>({});
  const move = (e: KeyboardEvent<HTMLButtonElement>) => {
    const index = items.findIndex((i) => i.id === value);
    const next =
      e.key === "ArrowRight" ? items[(index + 1) % items.length] : e.key === "ArrowLeft" ? items[(index - 1 + items.length) % items.length] : e.key === "Home" ? items[0] : e.key === "End" ? items[items.length - 1] : null;
    if (!next) return;
    e.preventDefault();
    onChange(next.id);
    refs.current[next.id]?.focus();
  };
  return (
    <div role="tablist" aria-label={label} className="flex gap-1 overflow-x-auto border-b">
      {items.map((item) => {
        const selected = item.id === value;
        const Icon = item.icon;
        return (
          <button
            key={item.id}
            ref={(el) => {
              refs.current[item.id] = el;
            }}
            type="button"
            role="tab"
            id={`tab-${item.id}`}
            aria-selected={selected}
            aria-controls={`panel-${item.id}`}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(item.id)}
            onKeyDown={move}
            data-testid={`tab-${item.id}`}
            className={cn(
              "relative -mb-px inline-flex items-center gap-2 whitespace-nowrap border-b-2 px-3 py-2.5 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              selected ? "border-beam-from font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
            )}
          >
            {Icon && <Icon className="h-4 w-4" aria-hidden />}
            {item.label}
            {item.count !== undefined && <span className={cn("rounded-full px-1.5 py-0.5 text-[11px] tabular-nums leading-none", selected ? "bg-accent text-accent-foreground" : "bg-secondary text-muted-foreground")}>{item.count}</span>}
          </button>
        );
      })}
    </div>
  );
}

// TabPanel is the region a tab controls; only the selected one is rendered.
export function TabPanel<T extends string>({ id, value, children, className }: { id: T; value: T; children: ReactNode; className?: string }) {
  if (id !== value) return null;
  return (
    <div role="tabpanel" id={`panel-${id}`} aria-labelledby={`tab-${id}`} className={cn("flex flex-col gap-6", className)}>
      {children}
    </div>
  );
}

// useTabParam keeps the selected tab in `?tab=`, so a reload or a copied link opens the same section.
export function useTabParam<T extends string>(allowed: readonly T[], fallback: T): [T, (next: T) => void] {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const raw = params.get("tab");
  const value = allowed.includes(raw as T) ? (raw as T) : fallback;
  const set = (next: T) => {
    const query = new URLSearchParams(params.toString());
    if (next === fallback) query.delete("tab");
    else query.set("tab", next);
    const q = query.toString();
    router.replace(q ? `${pathname}?${q}` : pathname, { scroll: false });
  };
  return [value, set];
}
