"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";

import type { components } from "@/api/schema";
import { useEnvironments } from "@/api/queries";
import { cn } from "@/lib/utils";

export type Environment = components["schemas"]["Environment"];

interface EnvironmentContextValue {
  environments: Environment[];
  current: Environment | null;
  select: (id: number) => void;
  loading: boolean;
}

const EnvironmentContext = createContext<EnvironmentContextValue | null>(null);

function storageKey(workspaceId: string) {
  return `hookbeam.environment.${workspaceId}`;
}

// EnvironmentProvider remembers which environment the person is looking at, per workspace, in this browser.
export function EnvironmentProvider({ workspaceId, children }: { workspaceId: string; children: React.ReactNode }) {
  const envs = useEnvironments(workspaceId !== "");
  const [selectedId, setSelectedId] = useState<number | null>(null);

  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(storageKey(workspaceId));
      setSelectedId(raw ? Number(raw) : null);
    } catch {
      setSelectedId(null);
    }
  }, [workspaceId]);

  const select = useCallback(
    (id: number) => {
      setSelectedId(id);
      try {
        window.localStorage.setItem(storageKey(workspaceId), String(id));
      } catch {
        // a private window or blocked storage only loses the convenience
      }
    },
    [workspaceId],
  );

  const value = useMemo<EnvironmentContextValue>(() => {
    const environments = envs.data ?? [];
    const current = environments.find((e) => e.id === selectedId) ?? environments.find((e) => e.kind === "development") ?? environments[0] ?? null;
    return { environments, current, select, loading: envs.isPending };
  }, [envs.data, envs.isPending, selectedId, select]);

  return <EnvironmentContext.Provider value={value}>{children}</EnvironmentContext.Provider>;
}

export function useEnvironment() {
  const ctx = useContext(EnvironmentContext);
  if (!ctx) throw new Error("useEnvironment needs an EnvironmentProvider");
  return ctx;
}

// EnvironmentBadge names the environment; production is loud and says so in words, not only in colour.
export function EnvironmentBadge({ environment, className }: { environment: Environment | null; className?: string }) {
  if (!environment) {
    return (
      <span className={cn("rounded px-2 py-0.5 text-xs font-medium bg-secondary text-secondary-foreground", className)} data-testid="environment-badge">
        no environment
      </span>
    );
  }
  const production = environment.kind === "production";
  return (
    <span
      className={cn(
        "rounded px-2 py-0.5 text-xs font-semibold uppercase tracking-wide",
        production ? "bg-destructive text-white ring-2 ring-destructive/40" : "bg-secondary text-secondary-foreground",
        className,
      )}
      data-testid="environment-badge"
      data-kind={environment.kind}
    >
      {production ? "Production" : "Development"}
      {environment.name.toLowerCase() !== environment.kind ? ` · ${environment.name}` : ""}
    </span>
  );
}
